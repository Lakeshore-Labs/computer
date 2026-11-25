// Package computer is a sandboxed computer for an agent: a shell (gbash) over mounted directories, with the tools of
// an MCP server as commands and as a Python package, jq and sqlite3, and python3 running as WebAssembly. Nothing in it
// reaches the network or the host, except through those tools and mounts.
package computer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Lakeshore-Labs/computer/files"
	"github.com/Lakeshore-Labs/computer/tools"
	"github.com/Lakeshore-Labs/computer/wasm"
	"github.com/Lakeshore-Labs/gbash"
	"github.com/Lakeshore-Labs/gbash/commands"
	"github.com/Lakeshore-Labs/gbash/contrib/awk"
	"github.com/Lakeshore-Labs/gbash/contrib/htmltomarkdown"
	"github.com/Lakeshore-Labs/gbash/contrib/jq"
	"github.com/Lakeshore-Labs/gbash/contrib/sqlite3"
	"github.com/Lakeshore-Labs/gbash/contrib/yq"
	gbfs "github.com/Lakeshore-Labs/gbash/fs"
	"github.com/Lakeshore-Labs/gbash/policy"
	"github.com/Lakeshore-Labs/gbash/shell/syntax"
	"github.com/tetratelabs/wazero/experimental"
)

// A Mount is a directory the shell and every program see at the path Guest: the filesystem FS, which can be
// anything that implements afero.Fs, or, when FS is nil, the host directory Host (see files.Dir).
type Mount = wasm.Dir

// Config is what one computer is made of. Only Mounts is needed for a working shell.
type Config struct {
	Mounts   []Mount
	WorkDir  string             // where a command starts; a mount, or a directory in one
	Commands []commands.Command // commands beyond the shell's own
	Tools    []tools.Source     // the MCP servers whose tools become commands and the Python package `tools`
	Python   *Python            // python3; nil for none
	// Options are further gbash options, applied last: gbash.WithHTTPAccess or WithNetwork for curl, which the
	// computer otherwise lacks, WithLimitOverrides, WithTracing, WithPolicy.
	Options []gbash.Option
}

// Python is a wasi-python, compiled once and shared by every computer of the process.
type Python struct {
	dir string // the unpacked release: python.wasm, lib/python315.zip and mpl-data/
	// ready is closed when module and err are set. Compiling the 110 MB binary takes about 30 seconds without a
	// cache, so it runs while the computer is already in use, and only a python3 waits for it.
	ready  chan struct{}
	module *wasm.Module
	err    error
}

const (
	pythonExe  = "/usr/local/bin/python.wasm" // the program's name, as wasi-python's runner gives it
	pythonLib  = "/usr/local/lib"             // where wasi-python looks for python315.zip
	pythonMpl  = pythonLib + "/python315.zip/matplotlib/mpl-data"
	pythonSite = "/usr/lib/python-tools" // where the generated package is mounted
	// pythonStartCode runs before every program. wasi-python runs in isolated mode, without site and without a
	// working directory, so it moves to the shell's working directory, puts the script's directory (or the working
	// directory) and the tools package on the path, and runs the program's arguments as wasi-python's runner does.
	// wasi-python's time.sleep waits for an absolute time, which wazero does not support; select waits for a
	// relative one.
	pythonStartCode = `import os, sys, time, select, linecache, _reactor
linecache._interactive_cache.clear()
time.sleep = lambda secs: select.select([], [], [], secs)
try:
    os.chdir(os.environ.get("PYCWD", "/"))
except OSError:
    pass
args, i = sys.argv[1:], 0
while i < len(args) and args[i].startswith("-") and args[i] not in ("-", "-c", "-m"):
    i += 2 if args[i] == "-X" else 1
if i < len(args) and not args[i].startswith("-"):
    args[i] = os.path.abspath(args[i])
    sys.path.insert(0, os.path.dirname(args[i]))
else:
    sys.path.insert(0, os.getcwd())
sys.path.append("` + pythonSite + `")
sys.exit(_reactor.run(["` + pythonExe + `", *args], dict(os.environ)))
`
)

// LoadPython reads the wasi-python release unpacked in dir, laid out as EmbeddedPython unpacks it:
// python.wasm, lib/python315.zip (alone in lib) and mpl-data/, matplotlib's data, and starts compiling it; the
// first python3 waits for the compile, and fails with its error. p's Binary is read from the file, its host call
// is answered by the computer's tools, and a zero MemoryPages is 8192 (512 MiB); its other fields are the caller's.
func LoadPython(ctx context.Context, dir string, p wasm.Program) (*Python, error) {
	var err error
	if p.Binary, err = os.ReadFile(filepath.Join(dir, "python.wasm")); err != nil {
		return nil, err
	}
	p.Host = tools.Host
	// wasi-python's C++ (pybind11, matplotlib, scipy) throws WebAssembly exceptions.
	p.Features |= experimental.CoreFeaturesExceptionHandling
	if p.MemoryPages == 0 {
		p.MemoryPages = 8192
	}
	py := &Python{dir: dir, ready: make(chan struct{})}
	go func() {
		defer close(py.ready)
		py.module, py.err = wasm.Compile(context.WithoutCancel(ctx), p)
	}()
	return py, nil
}

// pythonStart is pythonStartCode as a program for -c. It runs in a namespace of its own, so the program's
// __main__ is untouched, and under a file name of its own, so a traceback through it does not show the program's
// -c source. A JSON string is a Python string literal.
var pythonStart = func() string {
	b, _ := json.Marshal(pythonStartCode)
	return "exec(compile(" + string(b) + `, "<computer start>", "exec"), {})`
}()

// A Computer is one agent's computer. Its methods are safe for concurrent use.
type Computer struct {
	cfg  Config
	site string // a host directory: the generated Python package, rewritten when the tools change
	tmp  string // a host directory mounted at /tmp when the caller mounts none, removed by Close
	// opened are the host directories among the mounts, opened by New and closed by Close.
	opened []io.Closer

	mu      sync.Mutex
	listed  [sha256.Size]byte // the tools the session was built from
	written [sha256.Size]byte // the tools the Python package was last written from
	session *gbash.Session
	runtime *gbash.Runtime    // the session's, from which an Environment's command is given a session of its own
	policy  policy.Policy     // the session's, by which files decides as the shell does
	calling []string          // the commands that call tools, as the session names them
	modules []string          // the same tools' modules in the Python package, in the same order
	env     map[string]string // the shell's exported variables and working directory after the last Exec
}

// New makes a computer. Close it to free what it holds on the host.
func New(cfg Config) (_ *Computer, err error) {
	c := &Computer{cfg: cfg}
	defer func() { // what was made before an error is removed
		if err != nil {
			err = errors.Join(err, c.Close())
		}
	}()
	if c.site, err = os.MkdirTemp("", "computer-python"); err != nil {
		return nil, err
	}
	c.cfg.Mounts = slices.Clone(cfg.Mounts)
	if !slices.ContainsFunc(cfg.Mounts, func(m Mount) bool { return m.Guest == "/tmp" }) {
		if c.tmp, err = os.MkdirTemp("", "computer-tmp"); err != nil {
			return nil, err
		}
		c.cfg.Mounts = append(c.cfg.Mounts, Mount{Guest: "/tmp", Host: c.tmp})
	}
	if cfg.Python != nil {
		c.cfg.Mounts = append(c.cfg.Mounts, Mount{Guest: pythonLib, Host: filepath.Join(cfg.Python.dir, "lib"), ReadOnly: true},
			Mount{Guest: pythonSite, Host: c.site, ReadOnly: true})
	}
	// Each host directory is opened once, so that the shell and every program are given the one filesystem.
	for i, m := range c.cfg.Mounts {
		if m.FS != nil {
			continue
		}
		dir, err := files.Dir(m.Host)
		if err != nil {
			return nil, err
		}
		c.cfg.Mounts[i].FS, c.opened = dir, append(c.opened, dir)
	}
	return c, nil
}

// Mounts is every directory the computer mounts, the caller's and its own.
func (c *Computer) Mounts() []Mount { return c.cfg.Mounts }

// Close removes what the computer keeps on the host. The caller's mounts are the caller's and stay.
func (c *Computer) Close() error {
	var err error
	for _, dir := range c.opened {
		err = errors.Join(err, dir.Close())
	}
	err = errors.Join(err, os.RemoveAll(c.site))
	if c.tmp != "" {
		err = errors.Join(err, os.RemoveAll(c.tmp))
	}
	return err
}

// Exec runs a script or a command in the computer's shell, with the tools as they are now. A request with no Env
// starts where the last one ended: gbash keeps files between requests, and Exec carries the working directory and
// the exported variables.
func (c *Computer) Exec(ctx context.Context, req *gbash.ExecutionRequest) (*gbash.ExecutionResult, error) {
	s, err := c.Session(ctx)
	if err != nil {
		return nil, err
	}
	carry := req.Env == nil
	if carry {
		r := *req
		c.mu.Lock()
		r.Env = c.env
		if r.WorkDir == "" {
			r.WorkDir = c.env["PWD"] // gbash starts a request in WorkDir, not in the PWD of its environment
		}
		c.mu.Unlock()
		req = &r
	}
	res, err := s.Exec(ctx, req)
	if carry && res != nil {
		c.mu.Lock()
		c.env = res.FinalEnv
		c.mu.Unlock()
	}
	return res, err
}

// listTimeout is how long the MCP servers have to list their tools before a command runs with the tools it had.
const listTimeout = 5 * time.Second

// Session is the computer's shell session, for its filesystem and for interactive use. The tools are listed on
// every call: when they have changed, the commands and the Python package are made again and the session is a new
// one over the same files.
func (c *Computer) Session(ctx context.Context) (*gbash.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// ponytail: the tools are listed before every command, so a slow server costs each command up to listTimeout;
	// list once and refresh on the MCP client's tools-changed notification if that cost shows.
	listing, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	listed, err := tools.List(listing, c.cfg.Tools)
	if err != nil {
		if c.session != nil { // a server that stops answering takes its own commands down, not the shell
			return c.session, nil
		}
		return nil, err
	}
	if sum := digest(listed); c.session == nil || sum != c.listed {
		opts, err := c.options(listed)
		if err != nil {
			return nil, err
		}
		rt, err := gbash.New(opts...)
		if err != nil {
			return nil, err
		}
		s, err := rt.NewSession(ctx)
		if err != nil {
			return nil, err
		}
		c.session, c.runtime, c.listed = s, rt, sum
	}
	return c.session, nil
}

// shellBuiltins are the shell's builtins. gbash keeps its list to itself, so its shell is asked, once.
var shellBuiltins = sync.OnceValue(func() map[string]bool {
	builtins := map[string]bool{}
	rt, err := gbash.New()
	if err != nil {
		return builtins
	}
	res, err := rt.Run(context.Background(), &gbash.ExecutionRequest{Script: "enable -a"})
	if err != nil {
		return builtins
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if name, ok := strings.CutPrefix(line, "enable "); ok {
			builtins[strings.TrimPrefix(name, "-n ")] = true
		}
	}
	return builtins
})

// digest identifies a listing of the tools, to tell whether it has changed.
func digest(listed []tools.Tool) [sha256.Size]byte {
	b, _ := json.Marshal(listed)
	return sha256.Sum256(b)
}

// ToolCommands are the commands that call tools, as the shell last built names them.
func (c *Computer) ToolCommands() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calling
}

// ToolModules are the modules of the Python package tools, one per command of ToolCommands, in its order.
func (c *Computer) ToolModules() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.modules
}

// Options is the computer as gbash options, with the tools as they are now, for gbash's own front ends: its
// command-line program, its server and its tool for a model.
func (c *Computer) Options(ctx context.Context) ([]gbash.Option, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	listed, err := tools.List(ctx, c.cfg.Tools)
	if err != nil {
		return nil, err
	}
	return c.options(listed)
}

func (c *Computer) options(listed []tools.Tool) ([]gbash.Option, error) {
	registry := gbash.DefaultRegistry()
	// gbash's optional commands, except its python (a subset, and native code) and its nodejs (not yet safe).
	if err := errors.Join(awk.Register(registry), htmltomarkdown.Register(registry), jq.Register(registry), sqlite3.Register(registry), yq.Register(registry)); err != nil {
		return nil, err
	}
	mounts := c.cfg.Mounts
	custom := c.cfg.Commands
	if c.cfg.Python != nil {
		if sum := digest(listed); sum != c.written {
			if err := tools.WritePython(filepath.Join(c.site, "tools"), listed); err != nil {
				return nil, err
			}
			c.written = sum
		}
		custom = append(custom[:len(custom):len(custom)], c.python("python3", mounts), c.python("python", mounts))
	}
	for _, cmd := range custom {
		if err := registry.Register(cmd); err != nil {
			return nil, err
		}
	}
	groups := tools.Groups(listed)
	sorted := slices.Sorted(maps.Keys(groups))
	var names []string // the commands that call tools
	for _, group := range sorted {
		name := tools.Kebab(group)
		if name == "" { // a group whose name has no letter or digit a command can be spelled with
			name = "tools"
		}
		// The shell's command, builtin or keyword, or a group whose name spells the same, keeps the name: the shell
		// would run its own, and the group's command could not be reached.
		for _, taken := registry.Lookup(name); taken || syntax.IsKeyword(name) || shellBuiltins()[name]; _, taken = registry.Lookup(name) {
			name += "-tools"
		}
		if err := registry.Register(tools.Command(name, c.cfg.Tools, groups[group])); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	c.calling, c.modules = names, tools.Modules(sorted)
	var table []gbfs.MountConfig
	writable := []string{"/dev/null"} // gbash's own device, which a redirect to nowhere writes
	var readOnly []string
	for _, m := range mounts {
		factory := gbfs.FactoryFunc(func(context.Context) (gbfs.FileSystem, error) {
			return &shellFS{fs: m.FS, readOnly: m.ReadOnly, cwd: "/"}, nil
		})
		if m.ReadOnly {
			readOnly = append(readOnly, m.Guest)
		} else {
			writable = append(writable, m.Guest)
		}
		table = append(table, gbfs.MountConfig{MountPoint: m.Guest, Factory: factory})
	}
	// Outside the mounts gbash keeps files in memory, where a program run as WebAssembly cannot see them. The
	// shell may write only where every program sees the result.
	writes := readOnlyMounts{policy.NewStatic(&policy.Config{ReadRoots: []string{"/"}, WriteRoots: writable, SymlinkMode: policy.SymlinkDeny, Limits: limits}), readOnly}
	opts := []gbash.Option{gbash.WithPolicy(writes), gbash.WithRegistry(registry), gbash.WithBaseEnv(map[string]string{"HOME": c.cfg.WorkDir}), gbash.WithFileSystem(gbash.MountableFileSystem(gbash.MountableFileSystemOptions{Mounts: table, WorkingDir: c.cfg.WorkDir}))}
	opts = append(opts, c.cfg.Options...)
	var built gbash.Config // what the embedder's options leave as the policy
	for _, option := range opts {
		_ = option(&built)
	}
	c.policy = built.Policy
	return opts, nil
}

// readOnlyMounts is the shell's policy, which refuses a change to a read-only mount as it refuses one outside every
// mount, but says why: the path is in a read-only mount. Only the reason differs; what is refused is the same.
type readOnlyMounts struct {
	policy.Policy
	guests []string
}

func (p readOnlyMounts) AllowPath(ctx context.Context, action policy.FileAction, target string) error {
	err := p.Policy.AllowPath(ctx, action, target)
	var denied *policy.DeniedError
	if !errors.As(err, &denied) {
		return err
	}
	target = path.Clean("/" + target)
	for _, guest := range p.guests {
		if guest == "/" || target == guest || strings.HasPrefix(target, guest+"/") {
			return &policy.DeniedError{Subject: denied.Subject, Reason: "read-only file system"}
		}
	}
	return err
}

// limits are the shell's, chosen for data work: gbash's defaults stop a command at a file of 8 MiB and a script at
// 10,000 commands or loop iterations, which a CSV export or a loop over its lines passes. A command reads a whole
// file into the host's memory, so the file limit is also what one command may hold. Limits left at zero keep gbash's
// defaults, among them 1 MiB of captured output, which is what a model can be shown. An embedder changes any of
// them with gbash.WithLimitOverrides in Config.Options.
var limits = policy.Limits{MaxFileBytes: 256 << 20, MaxCommandCount: 1_000_000, MaxLoopIterations: 1_000_000}

// About is what an agent is told about this computer, given the commands that call tools and their modules in
// Python, as ToolCommands and ToolModules name them.
func (c *Computer) About(toolCommands, toolModules []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You work on a computer through a shell. Your files are under %s.", c.cfg.WorkDir)
	var opts gbash.Config // what the embedder's options make of the shell, to see whether curl has a network
	for _, option := range c.cfg.Options {
		_ = option(&opts)
	}
	switch {
	case opts.Network != nil && len(opts.Network.AllowedURLPrefixes) > 0:
		fmt.Fprintf(&b, " curl can fetch only URLs that start with %s.", strings.Join(opts.Network.AllowedURLPrefixes, ", "))
	case opts.Network == nil && opts.NetworkClient == nil:
		b.WriteString(" There is no network.")
	}
	b.WriteString("\n")
	b.WriteString("The shell has the usual commands, and also: jq, yq, awk, sqlite3")
	for _, command := range c.cfg.Commands {
		b.WriteString(", " + command.Name())
	}
	b.WriteString(".")
	if c.cfg.Python != nil {
		b.WriteString(" python3 has numpy, pandas, scipy, scikit-learn, statsmodels, matplotlib, seaborn, Pillow, lxml, openpyxl, xlsxwriter, python-docx, python-pptx, pypdf, pdfplumber and reportlab; it has no network, threads or subprocesses.")
	}
	if len(toolCommands) > 0 {
		fmt.Fprintf(&b, "\nThese commands call tools: %s. Run one with --help to see its tools, and `<command> <tool> --help` for a tool's arguments. A tool prints its result as text or JSON.", strings.Join(toolCommands, ", "))
		if c.cfg.Python != nil {
			fmt.Fprintf(&b, " In Python the same tools are the package tools, with a module per command, in the same order: %s. Import one as `from tools import %s`, then help(%s).", strings.Join(toolModules, ", "), toolModules[0], toolModules[0])
		}
	}
	b.WriteString("\nWhen data is to be filtered, counted, combined or stored, pipe it or process it in a script; do not copy data into a command by hand, and do not print large results.")
	return b.String()
}

// python is CPython as the command name.
func (c *Computer) python(name string, mounts []Mount) commands.Command {
	return commands.DefineCommand(name, func(ctx context.Context, inv *commands.Invocation) error {
		env := map[string]string{}
		for k, v := range inv.Env {
			env[k] = v
		}
		// matplotlib keeps its font list in a writable directory; /tmp outlives the run.
		env["PYCWD"], env["MPLCONFIGDIR"] = inv.Cwd, "/tmp/.matplotlib"
		stdin := inv.Stdin
		if stdin == nil {
			stdin = strings.NewReader("")
		}
		py := c.cfg.Python
		select {
		case <-py.ready:
		case <-ctx.Done():
			return commands.Exitf(inv, 124, "%s: %v", name, ctx.Err())
		}
		if py.err != nil {
			return commands.Exitf(inv, 70, "%s: %v", name, py.err)
		}
		code, err := py.module.Run(tools.With(ctx, c.cfg.Tools, inv.FS), wasm.Run{
			Args: append([]string{pythonExe, "-c", pythonStart}, inv.Args...), Env: env,
			Stdin: stdin, Stdout: inv.Stdout, Stderr: inv.Stderr,
			// matplotlib's data is mounted inside the zip at pythonLib, which wazero allows and the shell's own
			// mounts do not, so only Python sees it.
			Dirs: append(mounts[:len(mounts):len(mounts)], Mount{Guest: pythonMpl, Host: filepath.Join(c.cfg.Python.dir, "mpl-data"), ReadOnly: true}),
		})
		if err != nil {
			return commands.Exitf(inv, code, "%s: %v", name, err)
		}
		if code != 0 {
			return &commands.ExitError{Code: code}
		}
		return nil
	})
}

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
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/Lakeshore-Labs/computer/agent"
	"github.com/Lakeshore-Labs/computer/files"
	"github.com/Lakeshore-Labs/computer/tools"
	"github.com/Lakeshore-Labs/computer/wasm"
	"github.com/ewhauser/gbash"
	"github.com/ewhauser/gbash/commands"
	"github.com/ewhauser/gbash/contrib/awk"
	"github.com/ewhauser/gbash/contrib/htmltomarkdown"
	"github.com/ewhauser/gbash/contrib/jq"
	"github.com/ewhauser/gbash/contrib/sqlite3"
	"github.com/ewhauser/gbash/contrib/yq"
	gbfs "github.com/ewhauser/gbash/fs"
	"github.com/ewhauser/gbash/policy"
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
	Agent    *agent.Config      // the command agent, an agent that works in this computer; nil for none
	// Options are further gbash options, applied last: gbash.WithHTTPAccess or WithNetwork for curl, which the
	// computer otherwise lacks, WithLimitOverrides, WithTracing, WithPolicy.
	Options []gbash.Option
}

// Python is a compiled CPython, shared by every computer of the process.
type Python struct {
	module *wasm.Module
	lib    string
}

const (
	pythonLib  = "/lib"                  // CPython looks for its standard library under PYTHONHOME/lib
	pythonSite = "/usr/lib/python-tools" // where the generated package and the start-up module are mounted
	// sitecustomize is imported by Python at start. WASI has no working directory, so the command passes its own.
	sitecustomize = "import os\ntry:\n    os.chdir(os.environ.get('PYCWD', '/'))\nexcept OSError:\n    pass\n"
)

// LoadPython compiles the CPython binary at binary, whose standard library is the directory lib. p's Binary is
// read from the file; its other fields are the caller's, and its host call is answered by the computer's tools.
func LoadPython(ctx context.Context, binary, lib string, p wasm.Program) (*Python, error) {
	var err error
	if p.Binary, err = os.ReadFile(binary); err != nil {
		return nil, err
	}
	p.Host = tools.Host
	module, err := wasm.Compile(ctx, p)
	if err != nil {
		return nil, err
	}
	return &Python{module: module, lib: lib}, nil
}

// A Computer is one agent's computer. Its methods are safe for concurrent use.
type Computer struct {
	cfg  Config
	site string // a host directory: the generated Python package, rewritten when the tools change
	tmp  string // a host directory mounted at /tmp when the caller mounts none, removed by Close
	// opened are the host directories among the mounts, opened by New and closed by Close.
	opened []io.Closer
	// state is where the agent keeps its conversation: the caller's directory, or one made here and removed by
	// Close, with which the conversation ends.
	state, ownState string

	mu      sync.Mutex
	listed  [sha256.Size]byte // the tools the session was built from
	session *gbash.Session
	calling []string          // the commands that call tools, as the session names them
	env     map[string]string // the shell's exported variables and working directory after the last Exec
}

// New makes a computer. Close it to free what it holds on the host.
func New(cfg Config) (*Computer, error) {
	site, err := os.MkdirTemp("", "computer-python")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(site, "sitecustomize.py"), []byte(sitecustomize), 0o600); err != nil {
		return nil, err
	}
	c := &Computer{cfg: cfg, site: site}
	c.cfg.Mounts = slices.Clone(cfg.Mounts)
	if !slices.ContainsFunc(cfg.Mounts, func(m Mount) bool { return m.Guest == "/tmp" }) {
		if c.tmp, err = os.MkdirTemp("", "computer-tmp"); err != nil {
			return nil, err
		}
		c.cfg.Mounts = append(c.cfg.Mounts, Mount{Guest: "/tmp", Host: c.tmp})
	}
	if cfg.Python != nil {
		c.cfg.Mounts = append(c.cfg.Mounts, Mount{Guest: pythonLib, Host: cfg.Python.lib, ReadOnly: true}, Mount{Guest: pythonSite, Host: site, ReadOnly: true})
	}
	// Each host directory is opened once, so that the shell and every program are given the one filesystem.
	for i, m := range c.cfg.Mounts {
		if m.FS != nil {
			continue
		}
		dir, err := files.Dir(m.Host)
		if err != nil {
			return nil, errors.Join(err, c.Close())
		}
		c.cfg.Mounts[i].FS, c.opened = dir, append(c.opened, dir)
	}
	if cfg.Agent != nil {
		if c.state = cfg.Agent.State; c.state == "" {
			if c.ownState, err = os.MkdirTemp("", "computer-agent"); err != nil {
				return nil, err
			}
			c.state = c.ownState
		}
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
	if c.ownState != "" {
		err = errors.Join(err, os.RemoveAll(c.ownState))
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

// Session is the computer's shell session, for its filesystem and for interactive use. The tools are listed on
// every call: when they have changed, the commands and the Python package are made again and the session is a new
// one over the same files.
func (c *Computer) Session(ctx context.Context) (*gbash.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	listed, err := tools.List(ctx, c.cfg.Tools)
	if err != nil {
		if c.session != nil { // a server that stops answering takes its own commands down, not the shell
			return c.session, nil
		}
		return nil, err
	}
	b, _ := json.Marshal(listed)
	if sum := sha256.Sum256(b); c.session == nil || sum != c.listed {
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
		c.session, c.listed = s, sum
	}
	return c.session, nil
}

// ToolCommands are the commands that call tools, as the shell last built names them.
func (c *Computer) ToolCommands() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calling
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
		if err := tools.WritePython(filepath.Join(c.site, "tools"), listed); err != nil {
			return nil, err
		}
		custom = append(custom[:len(custom):len(custom)], c.python("python3", mounts), c.python("python", mounts))
	}
	for _, cmd := range custom {
		if err := registry.Register(cmd); err != nil {
			return nil, err
		}
	}
	groups := tools.Groups(listed)
	var names []string // the commands that call tools
	for _, group := range slices.Sorted(maps.Keys(groups)) {
		name := tools.Kebab(group)
		if name == "" { // a group whose name has no letter or digit a command can be spelled with
			name = "tools"
		}
		// The shell's command, the agent, or a group whose name spells the same, keeps the name.
		for _, taken := registry.Lookup(name); taken || c.cfg.Agent != nil && name == "agent"; _, taken = registry.Lookup(name) {
			name += "-tools"
		}
		if err := registry.Register(tools.Command(name, c.cfg.Tools, groups[group])); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	c.calling = names
	if c.cfg.Agent != nil {
		a := *c.cfg.Agent
		a.Mounts, a.Home, a.State = mounts, c.cfg.WorkDir, c.state
		if a.Instructions == "" {
			a.Instructions = c.About(names)
		}
		if err := registry.Register(agent.Command("agent", a)); err != nil {
			return nil, err
		}
	}
	var table []gbfs.MountConfig
	writable := []string{"/dev/null"} // gbash's own device, which a redirect to nowhere writes
	for _, m := range mounts {
		factory := gbfs.FactoryFunc(func(context.Context) (gbfs.FileSystem, error) {
			return &shellFS{fs: m.FS, readOnly: m.ReadOnly, cwd: "/"}, nil
		})
		if !m.ReadOnly {
			writable = append(writable, m.Guest)
		}
		table = append(table, gbfs.MountConfig{MountPoint: m.Guest, Factory: factory})
	}
	// Outside the mounts gbash keeps files in memory, where a program run as WebAssembly cannot see them. The
	// shell may write only where every program sees the result.
	writes := policy.NewStatic(&policy.Config{ReadRoots: []string{"/"}, WriteRoots: writable, SymlinkMode: policy.SymlinkDeny})
	opts := []gbash.Option{gbash.WithPolicy(writes), gbash.WithRegistry(registry), gbash.WithBaseEnv(map[string]string{"HOME": c.cfg.WorkDir}), gbash.WithFileSystem(gbash.MountableFileSystem(gbash.MountableFileSystemOptions{Mounts: table, WorkingDir: c.cfg.WorkDir}))}
	return append(opts, c.cfg.Options...), nil
}

// About is what an agent is told about this computer, given the commands that call tools.
func (c *Computer) About(toolCommands []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You work on a computer through a shell. Your files are under %s. There is no network.\n", c.cfg.WorkDir)
	b.WriteString("The shell has the usual commands, and jq, yq, awk and sqlite3.")
	if c.cfg.Python != nil {
		b.WriteString(" python3 has the standard library.")
	}
	if len(toolCommands) > 0 {
		fmt.Fprintf(&b, "\nThese commands call tools: %s. Run one with --help to see its tools, and `<command> <tool> --help` for a tool's arguments. A tool prints its result as text or JSON.", strings.Join(toolCommands, ", "))
		if c.cfg.Python != nil {
			b.WriteString(" In Python the same tools are the package tools: `from tools import <command>`, then help(<command>).")
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
		env["PYTHONHOME"], env["PYTHONPATH"], env["PYCWD"], env["PYTHONDONTWRITEBYTECODE"] = "/", pythonSite, inv.Cwd, "1"
		stdin := inv.Stdin
		if stdin == nil {
			stdin = strings.NewReader("")
		}
		args := append([]string{name}, inv.Args...)
		// CPython makes its script's path absolute before sitecustomize has moved to the working directory, so
		// a relative script is made absolute here. Options that end the options or take a value are stepped over.
	script:
		for i := 1; i < len(args); i++ {
			switch a := args[i]; {
			case a == "-c" || a == "-m" || a == "-":
				break script
			case a == "-W" || a == "-X":
				i++
			case !strings.HasPrefix(a, "-"):
				args[i] = gbfs.Resolve(inv.Cwd, a)
				break script
			}
		}
		code, err := c.cfg.Python.module.Run(tools.With(ctx, c.cfg.Tools), wasm.Run{
			Args: args, Env: env,
			Stdin: stdin, Stdout: inv.Stdout, Stderr: inv.Stderr, Dirs: mounts,
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

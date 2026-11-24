// Command computer runs a sandboxed computer from the command line.
//
//	computer -mount ./work:/home/user                      a shell with history and Tab completion
//	computer -mount ./work:/home/user -c 'ls | wc -l'      one command
//	computer -mount ./work:/home/user -mcp URL             the same, with the MCP server's tools as commands
//	computer -mount ./work:/home/user -mcp URL mcp         an MCP server on stdin and stdout, for an agent: its
//	                                                       tools are bash, read, edit and write
//
// Arguments after the flags go to gbash's own command line, so --json and --server work as they do there.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	nethttp "net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Lakeshore-Labs/gbash"
	"github.com/Lakeshore-Labs/gbash/cli"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/term"

	"github.com/Lakeshore-Labs/computer"
	"github.com/Lakeshore-Labs/computer/extra"
	"github.com/Lakeshore-Labs/computer/tools"
	"github.com/Lakeshore-Labs/computer/wasm"
)

type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(v string) error { *l = append(*l, v); return nil }

// server is one MCP server: a URL, or a program started and spoken to on its standard streams. The fields are
// those of the mcpServers entries that Claude Code and Claude Desktop read.
type server struct {
	name    string
	Type    string            `json:"type"` // read so that such a file is accepted; the url or the command says which it is
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// file is the -config file: what the flags -mount and -mcp say, for a computer with more of them than a command
// line reads well with.
type file struct {
	Mounts     []string          `json:"mounts"` // each as -mount takes it: host:guest or host:guest:ro
	MCPServers map[string]server `json:"mcpServers"`
}

// readConfig is the mounts and the servers, by name, of the -config file at path.
func readConfig(path string) ([]string, []server, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var f file
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields() // a misspelt key is an error, not a server silently left out
	if err := decoder.Decode(&f); err != nil {
		return nil, nil, err
	}
	var servers []server
	for _, name := range slices.Sorted(maps.Keys(f.MCPServers)) {
		s := f.MCPServers[name]
		if !isName(name) {
			return nil, nil, fmt.Errorf("the server name %q must be letters, digits, hyphens and underscores", name)
		}
		for _, values := range []map[string]string{s.Headers, s.Env} {
			for k, v := range values {
				values[k] = os.ExpandEnv(v)
			}
		}
		s.name = name
		servers = append(servers, s)
	}
	return f.Mounts, servers, nil
}

// headers adds an MCP server's headers, such as its token, to the requests sent to that server's own host, and to
// no host a redirect leads to.
type headers struct {
	scheme, host string
	set          map[string]string
}

func (h headers) RoundTrip(r *nethttp.Request) (*nethttp.Response, error) {
	if r.URL.Scheme == h.scheme && r.URL.Host == h.host {
		r = r.Clone(r.Context())
		for name, value := range h.set {
			r.Header.Set(name, value)
		}
	}
	return nethttp.DefaultTransport.RoundTrip(r)
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "computer:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	var mounts, allow, servers list
	flag.Var(&mounts, "mount", "a host directory, seen in the computer at the same path, or host:guest to name another; add :ro for read-only; repeatable")
	flag.Var(&allow, "allow-url", "a URL prefix curl may fetch; repeatable; none gives the computer no network")
	workDir := flag.String("workdir", "", "where commands start; the first mount when empty")
	flag.Var(&servers, "mcp", "an MCP server whose tools become commands: name=URL, or name='command arguments' for a server started as a program and spoken to on its standard streams, each one command of that name; or a bare URL, to group its tools by the prefix of their names; repeatable; a bearer token for a URL is read from COMPUTER_MCP_TOKEN_<NAME>, with a hyphen as an underscore, or COMPUTER_MCP_TOKEN for a bare URL")
	python := flag.String("python", "", "a CPython built for WASI, for python3, in place of the one built in")
	pythonLib := flag.String("python-lib", "", "the directory holding that build's lib/python3.x")
	pythonHost := flag.String("python-host", "computer", "the module name the -python build imports its host call from")
	pythonRuns := flag.Int("python-runs", 8, "how many python3 may run at once, each with up to 512 MiB; a further one waits; 0 is no limit")
	pythonFast := flag.Bool("python-unstoppable", false, "run Python three to four times faster, and give up stopping a script that does not end")
	timeout := flag.Duration("timeout", 2*time.Minute, "in the mcp mode, how long a bash call may run when the agent sets no limit")
	script := flag.String("c", "", "run this command and exit")
	config := flag.String("config", "", `a JSON file of mounts and MCP servers, added to those the flags give: {"mounts": ["host:guest"], "mcpServers": {"name": {"url": "..."} or {"command": "...", "args": [...]}}}; mcpServers is as in Claude Code's .mcp.json, and ${NAME} in a header or an env value is read from the environment`)
	flag.Parse()

	var listed []server // in the order given: the file's by name, then the flags'
	if *config != "" {
		f, named, err := readConfig(*config)
		if err != nil {
			return fmt.Errorf("-config %s: %w", *config, err)
		}
		mounts, listed = append(mounts, f...), named
	}

	cfg := computer.Config{WorkDir: *workDir, Commands: extra.Commands()}
	if len(allow) > 0 {
		cfg.Options = append(cfg.Options, gbash.WithNetwork(&gbash.NetworkConfig{AllowedURLPrefixes: allow, DenyPrivateRanges: true}))
	}
	for _, m := range mounts {
		mount, err := parseMount(m)
		if err != nil {
			return err
		}
		cfg.Mounts = append(cfg.Mounts, mount)
	}
	if cfg.WorkDir == "" && len(cfg.Mounts) > 0 {
		cfg.WorkDir = cfg.Mounts[0].Guest
	}
	for _, flagged := range servers {
		name, url, named := strings.Cut(flagged, "=")
		// The variable is spelt with an underscore for a hyphen, so that a shell can export it.
		token := "COMPUTER_MCP_TOKEN_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
		if !named || !isName(name) { // a bare URL or program, which may itself hold an equals sign
			name, url, token = "", flagged, "COMPUTER_MCP_TOKEN"
		}
		if name != "" && slices.ContainsFunc(listed, func(s server) bool { return s.name == name }) {
			return fmt.Errorf("the MCP server %q is given twice", name)
		}
		s := server{URL: url}
		if program := strings.Fields(url); !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") && len(program) > 0 {
			s = server{Command: program[0], Args: program[1:]}
		} else if t := os.Getenv(token); t != "" {
			s.Headers = map[string]string{"Authorization": "Bearer " + t}
		}
		s.name = name
		listed = append(listed, s)
	}
	for _, s := range listed {
		name := s.name
		var transport mcp.Transport
		switch {
		case s.Command != "" && s.URL == "":
			command := exec.CommandContext(ctx, s.Command, s.Args...)
			// The program is not given any server's token, unless its own env names one.
			command.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
				return strings.HasPrefix(kv, "COMPUTER_MCP_TOKEN")
			})
			for k, v := range s.Env {
				command.Env = append(command.Env, k+"="+v)
			}
			transport = &mcp.CommandTransport{Command: command}
		case s.URL != "" && s.Command == "":
			endpoint, err := neturl.Parse(s.URL)
			if err != nil {
				return fmt.Errorf("the MCP server %q: %w", name, err)
			}
			http := &mcp.StreamableClientTransport{Endpoint: s.URL}
			if len(s.Headers) > 0 {
				http.HTTPClient = &nethttp.Client{Transport: headers{scheme: endpoint.Scheme, host: endpoint.Host, set: s.Headers}}
			}
			transport = http
		default:
			return fmt.Errorf("the MCP server %q needs a url or a command, and not both", name)
		}
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "computer"}, nil).Connect(ctx, transport, nil)
		if err != nil {
			return fmt.Errorf("the MCP server %q: %w", name, err)
		}
		defer cs.Close()
		cfg.Tools = append(cfg.Tools, tools.Source{Name: name, Session: cs})
	}
	cache, err := cacheDir(cfg.Mounts)
	if err != nil {
		// A mount such as the home directory holds the cache; Python is then compiled on every start instead.
		fmt.Fprintf(os.Stderr, "computer: compiling Python without a cache: %v\n", err)
		cache = ""
	}
	program := wasm.Program{MaxRuns: *pythonRuns, CacheDir: cache, Unstoppable: *pythonFast}
	if *python != "" {
		program.MemoryPages, program.HostModule = 8192, *pythonHost
		cfg.Python, err = computer.LoadPython(ctx, *python, *pythonLib, program)
	} else {
		cfg.Python, err = computer.EmbeddedPython(ctx, program)
	}
	if err != nil {
		return fmt.Errorf("python: %w", err)
	}
	c, err := computer.New(cfg)
	if err != nil {
		return err
	}
	defer c.Close()

	if *script == "" && flag.NArg() == 0 && term.IsTerminal(syscall.Stdin) {
		return prompt(ctx, c, cfg.WorkDir) // which gives an interrupt to the command that is running, and goes on
	}
	// An interrupt ends what is running as a deadline would, and the computer removes its temporary directories.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if flag.Arg(0) == "mcp" {
		server, err := c.MCPServer(ctx, *timeout)
		if err != nil {
			return err
		}
		return server.Run(ctx, &mcp.StdioTransport{})
	}
	opts, err := c.Options(ctx)
	if err != nil {
		return err
	}
	args := flag.Args()
	if *script != "" {
		args = append([]string{"-c", *script}, args...)
	}
	code, err := cli.Run(ctx, cli.Config{Name: "computer", BaseOptions: opts}, "computer", args, os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		return err
	}
	if code != 0 {
		_ = c.Close()
		os.Exit(code)
	}
	return nil
}

// parseMount reads a -mount value: host:guest, with :ro for read-only. A host directory alone is seen in the
// computer at the path it has on the host, which is the path an agent started in that directory will use.
func parseMount(m string) (computer.Mount, error) {
	parts := strings.Split(m, ":")
	readOnly := len(parts) > 1 && parts[len(parts)-1] == "ro"
	if readOnly {
		parts = parts[:len(parts)-1]
	}
	switch {
	case len(parts) == 1 && parts[0] != "":
		guest, err := filepath.Abs(parts[0])
		if err != nil {
			return computer.Mount{}, err
		}
		return computer.Mount{Host: parts[0], Guest: filepath.ToSlash(guest), ReadOnly: readOnly}, nil
	case len(parts) == 2:
		return computer.Mount{Host: parts[0], Guest: parts[1], ReadOnly: readOnly}, nil
	}
	return computer.Mount{}, fmt.Errorf("-mount %q is not host, host:guest, or either with :ro", m)
}

// cacheDir is where compiled programs are kept. What is compiled there is native code that this process runs, so a
// script must not be able to write it.
func cacheDir(mounts []computer.Mount) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "computer")
	return dir, outside(dir, mounts)
}

// outside makes dir and reports, as an error, that it is within a mounted host directory or holds one, where
// what runs in the computer could read and write it.
func outside(dir string, mounts []computer.Mount) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	for _, m := range mounts {
		if m.Host == "" {
			continue
		}
		host, err := filepath.Abs(m.Host)
		if err == nil {
			host, err = filepath.EvalSymlinks(host)
		}
		if err != nil {
			return err
		}
		if within(dir, host) || within(host, dir) {
			return fmt.Errorf("%s must not be inside the mounted folder %s, nor hold it: what runs in the computer could change it", dir, host)
		}
	}
	return nil
}

// within reports whether p is dir or below it.
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// isName reports whether s can name an MCP server: letters, digits, hyphens and underscores.
func isName(s string) bool {
	return s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") == ""
}

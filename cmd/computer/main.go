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
	"crypto/sha256"
	"encoding/base64"
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

	"github.com/ewhauser/gbash"
	"github.com/ewhauser/gbash/cli"
	"github.com/ewhauser/gbash/contrib/bashtool"
	"github.com/ewhauser/gbash/contrib/codingtools"
	gbfs "github.com/ewhauser/gbash/fs"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/term"

	"github.com/Lakeshore-Labs/computer"
	"github.com/Lakeshore-Labs/computer/agent"
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
	flag.Var(&mounts, "mount", "a host directory and where the computer sees it, as host:guest; add :ro for read-only; repeatable")
	flag.Var(&allow, "allow-url", "a URL prefix curl may fetch; repeatable; none gives the computer no network")
	workDir := flag.String("workdir", "", "where commands start; the first mount when empty")
	flag.Var(&servers, "mcp", "an MCP server whose tools become commands: name=URL, or name='command arguments' for a server started as a program and spoken to on its standard streams, each one command of that name; or a bare URL, to group its tools by the prefix of their names; repeatable; a bearer token for a URL is read from COMPUTER_MCP_TOKEN_<NAME>, or COMPUTER_MCP_TOKEN for a bare URL")
	python := flag.String("python", "", "a CPython built for WASI, for python3")
	pythonLib := flag.String("python-lib", "", "the directory holding that build's lib/python3.x")
	pythonHost := flag.String("python-host", "computer", "the module name the Python build imports its host call from")
	pythonFast := flag.Bool("python-unstoppable", false, "run Python three to four times faster, and give up stopping a script that does not end")
	timeout := flag.Duration("timeout", 2*time.Minute, "in the mcp mode, how long a bash call may run when the agent sets no limit")
	agentDir := flag.String("agent", "", "a directory holding the agent's bundle, agent.js, for the command agent")
	agentEngine := flag.String("agent-engine", "", "QuickJS built as a WASI command, which runs the agent")
	agentState := flag.String("agent-state", "", "a directory in which the agent keeps its conversation, outside every mount; by default one under the user's cache directory, named for the first mount")
	modelURL := flag.String("model-url", "", "the agent's model: an OpenAI-compatible address, without /chat/completions; its key is read from COMPUTER_MODEL_KEY")
	modelName := flag.String("model", "", "the agent's model's name")
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

	cfg := computer.Config{WorkDir: *workDir}
	if len(allow) > 0 {
		cfg.Options = append(cfg.Options, gbash.WithNetwork(&gbash.NetworkConfig{AllowedURLPrefixes: allow, DenyPrivateRanges: true}))
	}
	for _, m := range mounts {
		parts := strings.Split(m, ":")
		if len(parts) < 2 || len(parts) > 3 || len(parts) == 3 && parts[2] != "ro" {
			return fmt.Errorf("-mount %q is not host:guest or host:guest:ro", m)
		}
		cfg.Mounts = append(cfg.Mounts, computer.Mount{Host: parts[0], Guest: parts[1], ReadOnly: len(parts) == 3})
	}
	if cfg.WorkDir == "" && len(cfg.Mounts) > 0 {
		cfg.WorkDir = cfg.Mounts[0].Guest
	}
	for _, flagged := range servers {
		name, url, named := strings.Cut(flagged, "=")
		token := "COMPUTER_MCP_TOKEN_" + strings.ToUpper(name)
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
			command.Env = os.Environ()
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
	if *python != "" {
		cache, err := cacheDir(cfg.Mounts)
		if err != nil {
			return err
		}
		p, err := computer.LoadPython(ctx, *python, *pythonLib, wasm.Program{MemoryPages: 8192, CacheDir: cache, HostModule: *pythonHost, Unstoppable: *pythonFast})
		if err != nil {
			return fmt.Errorf("-python: %w", err)
		}
		cfg.Python = p
	}
	if *agentDir != "" {
		engine, err := os.ReadFile(*agentEngine)
		if err != nil {
			return fmt.Errorf("-agent-engine: %w", err)
		}
		cache, err := cacheDir(cfg.Mounts)
		if err != nil {
			return err
		}
		// The agent can be stopped where it stands: what the model asks of it, such as many edits of a large
		// file, can keep it computing long past the time a caller allows.
		module, err := wasm.Compile(ctx, wasm.Program{Binary: engine, MemoryPages: 8192, CacheDir: cache})
		if err != nil {
			return fmt.Errorf("-agent-engine: %w", err)
		}
		// The conversation is kept outside every mount, by default in a directory named for the first one, so
		// that it goes on from one invocation to the next over the same folder.
		stateDir := *agentState
		if stateDir == "" && len(cfg.Mounts) > 0 {
			folder, _ := filepath.Abs(cfg.Mounts[0].Host)
			stateDir = fmt.Sprintf("%s/agent/%x", cache, sha256.Sum256([]byte(folder)))
		}
		if stateDir != "" {
			if err := outside(stateDir, cfg.Mounts); err != nil {
				return fmt.Errorf("-agent-state: %w", err)
			}
		}
		cfg.Agent = &agent.Config{Engine: module, Script: *agentDir, Cache: cache + "/agent-bytecode", State: stateDir, ModelURL: *modelURL, ModelKey: os.Getenv("COMPUTER_MODEL_KEY"), Model: *modelName}
	}
	c, err := computer.New(cfg)
	if err != nil {
		return err
	}
	defer c.Close()

	if *script == "" && flag.NArg() == 0 && term.IsTerminal(int(os.Stdin.Fd())) {
		return prompt(ctx, c, cfg.WorkDir) // which gives an interrupt to the command that is running, and goes on
	}
	// An interrupt ends what is running as a deadline would: the agent records its run as aborted, and the
	// computer removes its temporary directories.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if flag.Arg(0) == "mcp" {
		return serve(ctx, c, cfg, *timeout)
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

// cacheDir is where compiled programs and, by default, the agent's conversation are kept. What is compiled there
// is native code that this process runs, so a script must not be able to write it.
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

// writable reports, as an error, a path the file tools may not write: one outside every writable mount. The shell
// is held to the same rule by the computer; the file tools reach the filesystem directly.
func writable(c *computer.Computer, workDir, p string) error {
	if p == "~" || strings.HasPrefix(p, "~/") { // the file tools read a leading ~ as the working directory
		p = workDir + p[1:]
	}
	p = gbfs.Resolve(workDir, p)
	for _, m := range c.Mounts() {
		if !m.ReadOnly && (p == m.Guest || strings.HasPrefix(p, strings.TrimSuffix(m.Guest, "/")+"/")) {
			return nil
		}
	}
	return fmt.Errorf("%s is outside the writable folders", p)
}

// serve offers the computer to an agent as an MCP server on stdin and stdout. The tools and their wording are
// gbash's: bashtool's bash, and codingtools' read, edit and write.
func serve(ctx context.Context, c *computer.Computer, cfg computer.Config, limit time.Duration) error {
	server := mcp.NewServer(&mcp.Implementation{Name: "computer"}, nil)
	text := func(s string, failed bool) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}, IsError: failed}, nil
	}

	description := "Run bash commands in a sandboxed computer. It has the usual commands, jq, yq, awk and sqlite3"
	if cfg.Python != nil {
		description += ", and python3"
	}
	if len(cfg.Tools) > 0 {
		if _, err := c.Session(ctx); err != nil {
			return err
		}
		description += ". These commands call tools; run one with --help to see what it does: " + strings.Join(c.ToolCommands(), ", ")
		if cfg.Python != nil {
			modules := c.ToolModules()
			description += ". In Python the same tools are the package `tools`, with the modules " + strings.Join(modules, ", ") + ": from tools import " + modules[0]
		}
	}
	bash := bashtool.New(bashtool.Config{})
	server.AddTool(&mcp.Tool{Name: bash.Name(), Description: description + ".", InputSchema: bash.InputSchema()},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var input map[string]any
			_ = json.Unmarshal(req.Params.Arguments, &input)
			in, err := bashtool.ParseRequest(input)
			if err != nil {
				return text(err.Error(), true)
			}
			timeout := limit
			if t := in.Timeout(); t > 0 && t < limit {
				timeout = t
			}
			res, err := c.Exec(ctx, &gbash.ExecutionRequest{Script: in.ResolvedCommands(), Timeout: timeout})
			if err != nil {
				return text(err.Error(), true)
			}
			return text(bashtool.FormatToolResult(bashtool.Response{Stdout: res.Stdout, Stderr: res.Stderr, ExitCode: res.ExitCode}), false)
		})

	for _, def := range codingtools.New(codingtools.Config{}).ToolDefinitions() {
		server.AddTool(&mcp.Tool{Name: def.Name, Description: def.Description, InputSchema: def.InputSchema},
			func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				s, err := c.Session(ctx)
				if err != nil {
					return text(err.Error(), true)
				}
				files := codingtools.New(codingtools.Config{FS: s.FileSystem(), WorkingDir: cfg.WorkDir, HomeDir: cfg.WorkDir})
				var input map[string]any
				_ = json.Unmarshal(req.Params.Arguments, &input)
				var blocks []codingtools.ContentBlock
				switch def.Name {
				case files.ReadToolDefinition().Name:
					var in codingtools.ReadRequest
					var out codingtools.ReadResponse
					if in, err = codingtools.ParseReadRequest(input); err == nil {
						out, err = files.Read(ctx, in)
					}
					blocks = out.Content
				case files.EditToolDefinition().Name:
					var in codingtools.EditRequest
					var out codingtools.EditResponse
					if in, err = codingtools.ParseEditRequest(input); err == nil {
						if err = writable(c, cfg.WorkDir, in.Path); err == nil {
							out, err = files.Edit(ctx, in)
						}
					}
					blocks = out.Content
				default:
					var in codingtools.WriteRequest
					var out codingtools.WriteResponse
					if in, err = codingtools.ParseWriteRequest(input); err == nil {
						if err = writable(c, cfg.WorkDir, in.Path); err == nil {
							out, err = files.Write(ctx, in)
						}
					}
					blocks = out.Content
				}
				if err != nil {
					return text(err.Error(), true)
				}
				result := &mcp.CallToolResult{}
				for _, b := range blocks {
					if data, err := base64.StdEncoding.DecodeString(b.Data); b.Data != "" && err == nil {
						result.Content = append(result.Content, &mcp.ImageContent{Data: data, MIMEType: b.MIMEType})
					} else {
						result.Content = append(result.Content, &mcp.TextContent{Text: b.Text})
					}
				}
				return result, nil
			})
	}
	return server.Run(ctx, &mcp.StdioTransport{})
}

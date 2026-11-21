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
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	nethttp "net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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
	"github.com/Lakeshore-Labs/computer/extra"
	"github.com/Lakeshore-Labs/computer/tools"
	"github.com/Lakeshore-Labs/computer/wasm"
)

type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(v string) error { *l = append(*l, v); return nil }

// bearer adds an MCP server's token to the requests sent to that server's own host, and to no host a redirect
// leads to.
type bearer struct{ scheme, host, token string }

func (b bearer) RoundTrip(r *nethttp.Request) (*nethttp.Response, error) {
	if r.URL.Scheme == b.scheme && r.URL.Host == b.host {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+b.token)
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
	flag.Parse()

	cfg := computer.Config{WorkDir: *workDir, Commands: extra.Commands()}
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
	for _, server := range servers {
		name, url, named := strings.Cut(server, "=")
		token := "COMPUTER_MCP_TOKEN_" + strings.ToUpper(name)
		if !named || !isName(name) { // a bare URL or program, which may itself hold an equals sign
			name, url, token = "", server, "COMPUTER_MCP_TOKEN"
		}
		var transport mcp.Transport
		if program := strings.Fields(url); !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") && len(program) > 0 {
			transport = &mcp.CommandTransport{Command: exec.CommandContext(ctx, program[0], program[1:]...)}
		} else {
			endpoint, err := neturl.Parse(url)
			if err != nil {
				return fmt.Errorf("-mcp %s: %w", server, err)
			}
			http := &mcp.StreamableClientTransport{Endpoint: url}
			if t := os.Getenv(token); t != "" {
				http.HTTPClient = &nethttp.Client{Transport: bearer{scheme: endpoint.Scheme, host: endpoint.Host, token: t}}
			}
			transport = http
		}
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "computer"}, nil).Connect(ctx, transport, nil)
		if err != nil {
			return fmt.Errorf("-mcp %s: %w", server, err)
		}
		defer cs.Close()
		cfg.Tools = append(cfg.Tools, tools.Source{Name: name, Session: cs})
	}
	if *python != "" {
		cache, _ := os.UserCacheDir()
		p, err := computer.LoadPython(ctx, *python, *pythonLib, wasm.Program{MemoryPages: 8192, CacheDir: cache + "/computer", HostModule: *pythonHost, Unstoppable: *pythonFast})
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
		cache, _ := os.UserCacheDir()
		// The agent can be stopped where it stands: what the model asks of it, such as many edits of a large
		// file, can keep it computing long past the time a caller allows.
		module, err := wasm.Compile(ctx, wasm.Program{Binary: engine, MemoryPages: 8192, CacheDir: cache + "/computer"})
		if err != nil {
			return fmt.Errorf("-agent-engine: %w", err)
		}
		// The conversation is kept outside every mount, by default in a directory named for the first one, so
		// that it goes on from one invocation to the next over the same folder.
		stateDir := *agentState
		if stateDir == "" && len(cfg.Mounts) > 0 {
			folder, _ := filepath.Abs(cfg.Mounts[0].Host)
			stateDir = fmt.Sprintf("%s/computer/agent/%x", cache, sha256.Sum256([]byte(folder)))
		}
		if stateDir != "" {
			if err := os.MkdirAll(stateDir, 0o700); err != nil {
				return fmt.Errorf("-agent-state: %w", err)
			}
		}
		cfg.Agent = &agent.Config{Engine: module, Script: *agentDir, Cache: cache + "/computer/agent-bytecode", State: stateDir, ModelURL: *modelURL, ModelKey: os.Getenv("COMPUTER_MODEL_KEY"), Model: *modelName}
	}
	c, err := computer.New(cfg)
	if err != nil {
		return err
	}
	defer c.Close()

	if flag.Arg(0) == "mcp" {
		return serve(ctx, c, cfg, *timeout)
	}
	opts, err := c.Options(ctx)
	if err != nil {
		return err
	}
	args := flag.Args()
	if *script == "" && len(args) == 0 && term.IsTerminal(int(os.Stdin.Fd())) {
		return prompt(ctx, c, cfg.WorkDir)
	}
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
		listed, err := tools.List(ctx, cfg.Tools)
		if err != nil {
			return err
		}
		var groups []string
		for group := range tools.Groups(listed) {
			groups = append(groups, tools.Kebab(group))
		}
		sort.Strings(groups)
		description += ". These commands call tools; run one with --help to see what it does: " + strings.Join(groups, ", ")
		if cfg.Python != nil {
			description += ". In Python the same tools are the package `tools`: from tools import <command>"
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

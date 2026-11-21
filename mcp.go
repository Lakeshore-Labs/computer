package computer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ewhauser/gbash"
	"github.com/ewhauser/gbash/contrib/codingtools"
	gbfs "github.com/ewhauser/gbash/fs"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// writable reports, as an error, a path the file tools may not write: one outside every writable mount. The shell
// is held to the same rule by the computer; the file tools reach the filesystem directly.
func (c *Computer) writable(p string) error {
	workDir := c.cfg.WorkDir
	if p == "~" || strings.HasPrefix(p, "~/") { // the file tools read a leading ~ as the working directory
		p = workDir + p[1:]
	}
	p = gbfs.Resolve(workDir, p)
	for _, m := range c.cfg.Mounts {
		if !m.ReadOnly && (p == m.Guest || strings.HasPrefix(p, strings.TrimSuffix(m.Guest, "/")+"/")) {
			return nil
		}
	}
	return fmt.Errorf("%s is outside the writable folders", p)
}

// bashInput is the input of the bash tool, the same as gbash's bash tool takes.
type bashInput struct {
	Commands  string `json:"commands,omitempty" jsonschema:"Bash commands to execute."`
	Script    string `json:"script,omitempty" jsonschema:"The same as commands, for callers that send script."`
	TimeoutMS int64  `json:"timeout_ms,omitempty" jsonschema:"Per-call timeout in milliseconds."`
}

// MCPServer is the computer as an MCP server for an agent, with the tools bash, read, edit and write. The caller
// connects it: server.Run(ctx, &mcp.StdioTransport{}) for an agent that starts the computer as a program, or
// server.Connect with one end of mcp.NewInMemoryTransports for an agent in this process. limit is how long a bash
// call may run when the agent sets no shorter time. The tools and their wording are gbash's: the input of its bash
// tool, and codingtools' read, edit and write.
//
// A computer is one agent's: every server made from it shares the one shell, with its working directory and
// variables. The file tools take a relative path from the computer's WorkDir, wherever the shell has since moved.
// The bash tool describes the commands that call tools as they are when MCPServer is called.
func (c *Computer) MCPServer(ctx context.Context, limit time.Duration) (*mcp.Server, error) {
	workDir := c.cfg.WorkDir
	server := mcp.NewServer(&mcp.Implementation{Name: "computer"}, nil)
	text := func(s string, failed bool) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}, IsError: failed}, nil
	}

	// The agent is told what the agent in the computer is told, so the two cannot differ.
	if _, err := c.Session(ctx); err != nil {
		return nil, err
	}
	description := "Run bash commands in a sandboxed computer. " + c.About(c.ToolCommands(), c.ToolModules())
	mcp.AddTool(server, &mcp.Tool{Name: "bash", Description: description},
		func(ctx context.Context, _ *mcp.CallToolRequest, in bashInput) (*mcp.CallToolResult, any, error) {
			script := in.Commands
			if script == "" {
				script = in.Script
			}
			if script == "" {
				result, err := text("`commands` or `script` is required", true)
				return result, nil, err
			}
			timeout := limit
			if t := time.Duration(in.TimeoutMS) * time.Millisecond; t > 0 && t < limit {
				timeout = t
			}
			res, err := c.Exec(ctx, &gbash.ExecutionRequest{Script: script, Timeout: timeout})
			if err != nil {
				result, err := text(err.Error(), true)
				return result, nil, err
			}
			// The result is worded as gbash's bash tool words it.
			var out []string
			if res.Stdout != "" {
				out = append(out, res.Stdout)
			}
			if res.Stderr != "" {
				out = append(out, "STDERR: "+res.Stderr)
			}
			if res.ExitCode != 0 {
				out = append(out, fmt.Sprintf("Exit code: %d", res.ExitCode))
			}
			if len(out) == 0 {
				out = append(out, "(no output)")
			}
			result, err := text(strings.Join(out, "\n"), false)
			return result, nil, err
		})

	for _, def := range codingtools.New(codingtools.Config{}).ToolDefinitions() {
		server.AddTool(&mcp.Tool{Name: def.Name, Description: def.Description, InputSchema: def.InputSchema},
			func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				s, err := c.Session(ctx)
				if err != nil {
					return text(err.Error(), true)
				}
				files := codingtools.New(codingtools.Config{FS: s.FileSystem(), WorkingDir: workDir, HomeDir: workDir})
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
						if err = c.writable(in.Path); err == nil {
							out, err = files.Edit(ctx, in)
						}
					}
					blocks = out.Content
				default:
					var in codingtools.WriteRequest
					var out codingtools.WriteResponse
					if in, err = codingtools.ParseWriteRequest(input); err == nil {
						if err = c.writable(in.Path); err == nil {
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
	return server, nil
}

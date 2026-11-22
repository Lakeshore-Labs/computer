// Compare a library desk's published hour through the Go, host-call and shell interfaces.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Lakeshore-Labs/computer"
	"github.com/Lakeshore-Labs/computer/tools"
	"github.com/ewhauser/gbash"
	"github.com/ewhauser/gbash/commands"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/afero"
)

type hours struct {
	Day    string `json:"day"`
	Closes string `json:"closes"`
}

func exercise() (map[string]any, error) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "reading-room", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "library_getHours", Description: "Return the Sunday closing time for a synthetic library."},
		func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, hours, error) {
			return nil, hours{"Sunday", "16:00"}, nil
		})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		return nil, err
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "catalog", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		return nil, err
	}
	defer cs.Close()
	sources := []tools.Source{{Session: cs}}
	list, err := tools.List(ctx, sources)
	if err != nil {
		return nil, err
	}
	groups := tools.Groups(list)
	if len(list) != 1 || len(groups["library"]) != 1 {
		return nil, errors.New("expected a library tool")
	}
	cliName := tools.Kebab(groups["library"][0].Short)
	if cliName != "get-hours" {
		return nil, errors.New("unexpected command name")
	}
	modules := tools.Modules([]string{"library"})
	if !slices.Equal(modules, []string{"library"}) {
		return nil, errors.New("unexpected Python module")
	}
	dir, err := os.MkdirTemp("", "computer-hours-modules-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := tools.WritePython(dir, list); err != nil {
		return nil, err
	}
	py, err := os.ReadFile(filepath.Join(dir, "library.py"))
	if err != nil {
		return nil, err
	}
	if !strings.Contains(string(py), "def get_hours(") {
		return nil, errors.New("missing Python binding")
	}
	direct, err := tools.Call(ctx, cs, nil, "library_getHours", nil)
	if err != nil {
		return nil, err
	}
	want := hours{"Sunday", "16:00"}
	var viaGo hours
	if err := json.Unmarshal(direct, &viaGo); err != nil || viaGo != want {
		return nil, errors.New("Go result differs")
	}
	var viaHost struct {
		OK     bool
		Result hours
	}
	callContext := tools.With(ctx, sources, nil)
	reply := tools.Host(callContext, []byte(`{"source":0,"tool":"library_getHours","arguments":{}}`))
	if err := json.Unmarshal(reply, &viaHost); err != nil || !viaHost.OK || viaHost.Result != want {
		return nil, errors.New("host call differs")
	}
	command := tools.Command("library", sources, groups["library"])
	c, err := computer.New(computer.Config{WorkDir: "/desk", Mounts: []computer.Mount{{Guest: "/desk", FS: afero.NewMemMapFs()}}, Commands: []commands.Command{command}})
	if err != nil {
		return nil, err
	}
	defer c.Close()
	res, err := c.Exec(ctx, &gbash.ExecutionRequest{Script: "library " + cliName + " --json '{}'"})
	if err != nil {
		return nil, err
	}
	var shell hours
	if res.ExitCode != 0 || json.Unmarshal([]byte(res.Stdout), &shell) != nil || shell != want {
		return nil, fmt.Errorf("shell result differs: %q %q", res.Stdout, res.Stderr)
	}
	return map[string]any{"tool": list[0].Name, "group": list[0].Group, "shell_command": "library " + cliName + " --json '{}'", "python_module": modules[0], "python_function": "get_hours", "go_result": viaGo, "host_result": viaHost.Result, "shell_result": shell}, nil
}
func main() {
	v, err := exercise()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	if err := e.Encode(v); err != nil {
		panic(err)
	}
}

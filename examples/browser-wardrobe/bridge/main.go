// This stdio MCP server forwards two fixed operations to a browser-owned wardrobe.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type plan struct {
	Revision  string `json:"revision"`
	Required  int    `json:"required"`
	Available int    `json:"available"`
	Allocated int    `json:"allocated"`
	Short     int    `json:"short"`
	Spare     int    `json:"spare"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "expected loopback desk URL")
		os.Exit(1)
	}
	u, err := url.Parse(os.Args[1])
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		fmt.Fprintln(os.Stderr, "expected loopback origin")
		os.Exit(1)
	}
	client := http.Client{Timeout: 12 * time.Second}
	forward := func(ctx context.Context, operation string, input any) (*mcp.CallToolResult, any, error) {
		b, err := json.Marshal(map[string]any{"op": operation, "input": input})
		if err != nil {
			return nil, nil, err
		}
		req, err := http.NewRequestWithContext(ctx, "POST", u.String()+"/operation", bytes.NewReader(b))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Wardrobe-Link", os.Getenv("WARDROBE_LINK"))
		res, err := client.Do(req)
		if err != nil {
			return nil, nil, err
		}
		defer res.Body.Close()
		data, err := io.ReadAll(io.LimitReader(res.Body, 8192))
		if err != nil {
			return nil, nil, err
		}
		if res.StatusCode != 200 {
			return nil, nil, fmt.Errorf("browser store: %s", data)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "browser-wardrobe", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "read_inventory", Description: "Read the jacket inventory held by this browser tab."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return forward(ctx, "read", nil)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "save_plan", Description: "Save the allocation if the browser's inventory revision still matches."}, func(ctx context.Context, _ *mcp.CallToolRequest, p plan) (*mcp.CallToolResult, any, error) {
		return forward(ctx, "save", p)
	})
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// Command field-guide serves the recorded exhibits over MCP stdio.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed computer-field-guide.zip
var bundle []byte

type exhibit struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Group   string `json:"group"`
	Summary string `json:"summary"`
}

type searchInput struct {
	Offset int    `json:"offset,omitempty" jsonschema:"Number of matching entries to skip, starting at zero."`
	Query  string `json:"query" jsonschema:"Words to find in an exhibit's title, group, or summary."`
}
type searchOutput struct {
	Total int       `json:"total"`
	Items []exhibit `json:"items"`
}
type showInput struct {
	ID string `json:"id" jsonschema:"An exhibit id returned by search."`
}

func newGuide() (*mcp.Server, error) {
	archive, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		return nil, err
	}
	cases, err := fs.ReadDir(archive, "cases")
	if err != nil {
		return nil, err
	}
	exhibits := make(map[string]exhibit, len(cases))
	for _, file := range cases {
		id := strings.TrimSuffix(file.Name(), ".html")
		data, err := fs.ReadFile(archive, "evidence/"+id+".json")
		if err != nil {
			return nil, err
		}
		var item exhibit
		if err := json.Unmarshal(data, &item); err != nil {
			return nil, err
		}
		item.ID = id
		exhibits[id] = item
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "computer-field-guide", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "search", Description: "Find recorded Computer exhibits. Returns up to twelve matching entries and the full match count."},
		func(_ context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, searchOutput, error) {
			if in.Offset < 0 {
				return nil, searchOutput{}, errors.New("offset must be at least zero")
			}
			words := strings.Fields(strings.ToLower(in.Query))
			found := []exhibit{}
			for _, item := range exhibits {
				text := strings.ToLower(item.Title + " " + item.Group + " " + item.Summary)
				if slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(text, w) }) {
					continue
				}
				found = append(found, item)
			}
			slices.SortFunc(found, func(a, b exhibit) int { return strings.Compare(a.ID, b.ID) })
			start := min(in.Offset, len(found))
			page := found[start:]
			return nil, searchOutput{Total: len(found), Items: page[:min(12, len(page))]}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "show", Description: "Read an exhibit's evidence and its first recorded still image when available."},
		func(_ context.Context, _ *mcp.CallToolRequest, in showInput) (*mcp.CallToolResult, any, error) {
			if _, ok := exhibits[in.ID]; !ok {
				return nil, nil, fmt.Errorf("unknown exhibit %q", in.ID)
			}
			data, err := fs.ReadFile(archive, "evidence/"+in.ID+".json")
			if err != nil {
				return nil, nil, err
			}
			result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}
			for _, ext := range []string{".png", ".jpg"} {
				image, err := fs.ReadFile(archive, "assets/"+in.ID+ext)
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				if err != nil {
					return nil, nil, err
				}
				mime := "image/png"
				if ext == ".jpg" {
					mime = "image/jpeg"
				}
				result.Content = append(result.Content, &mcp.ImageContent{MIMEType: mime, Data: image})
				break
			}
			return result, nil, nil
		})
	for _, asset := range []struct{ uri, name, kind, path string }{
		{"guide://dispatch/sheet", "Printable dispatch sheet", "application/pdf", "assets/dispatch.pdf"},
		{"guide://dispatch/workbook", "Dispatch workbook", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "assets/dispatch.xlsx"},
		{"guide://observations/great-lakes", "Offline satellite observation desk", "text/html", "assets/great-lakes.html"},
	} {
		server.AddResource(&mcp.Resource{URI: asset.uri, Name: asset.name, MIMEType: asset.kind},
			func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				data, err := fs.ReadFile(archive, asset.path)
				if err != nil {
					return nil, err
				}
				content := &mcp.ResourceContents{URI: asset.uri, MIMEType: asset.kind}
				if asset.kind == "text/html" {
					content.Text = string(data)
				} else {
					content.Blob = data
				}
				return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{content}}, nil
			})
	}
	return server, nil
}

func main() {
	server, err := newGuide()
	if err != nil {
		log.Fatal(err)
	}
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

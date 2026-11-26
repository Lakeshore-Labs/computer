package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type outlineRequest struct {
	Repository string `json:"repoName" jsonschema:"GitHub repository in owner/repository format"`
}

var repositoryName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func readOutline(ctx context.Context, _ *mcp.CallToolRequest, input outlineRequest) (*mcp.CallToolResult, any, error) {
	if !repositoryName.MatchString(input.Repository) {
		return nil, nil, fmt.Errorf("expected a GitHub owner/repository")
	}
	endpoint := "https://raw.githubusercontent.com/" + input.Repository + "/HEAD/README.md"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("GitHub README returned %s", res.Status)
	}

	scanner := bufio.NewScanner(io.LimitReader(res.Body, 256<<10))
	var headings []string
	inCode := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
		}
		if !inCode && strings.HasPrefix(line, "#") && strings.HasPrefix(strings.TrimLeft(line, "#"), " ") {
			headings = append(headings, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	if len(headings) == 0 {
		return nil, nil, fmt.Errorf("no Markdown headings found in README.md")
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: strings.Join(headings, "\n")}},
	}, nil, nil
}

package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func connectGuide(t *testing.T) *mcp.ClientSession {
	t.Helper()
	server, err := newGuide()
	if err != nil {
		t.Fatal(err)
	}
	left, right := mcp.NewInMemoryTransports()
	s, err := server.Connect(t.Context(), left, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "guide-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), right, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestGuideSearchPages(t *testing.T) {
	session := connectGuide(t)
	search := func(offset int) searchOutput {
		t.Helper()
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "search", Arguments: map[string]any{"query": "", "offset": offset}})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatal("search returned a tool error")
		}
		var page searchOutput
		if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	first := search(0)
	second := search(len(first.Items))
	if first.Total != second.Total || second.Total < 24 || len(first.Items) != 12 || len(second.Items) != 12 || first.Items[11].ID >= second.Items[0].ID {
		t.Fatal("search pages overlap or are incomplete")
	}
	if last := search(first.Total); len(last.Items) != 0 || last.Total != first.Total {
		t.Fatal("expected an empty final page")
	}
}

func TestFindAndOpenExhibit(t *testing.T) {
	session := connectGuide(t)
	found, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "search", Arguments: map[string]any{"query": "bakery chrome"}})
	if err != nil {
		t.Fatal(err)
	}
	var matches searchOutput
	if err := json.Unmarshal([]byte(found.Content[0].(*mcp.TextContent).Text), &matches); err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(matches.Items, func(e exhibit) bool { return e.ID == "repair" })
	if i < 0 {
		t.Fatalf("unexpected matches: %v", matches.Items)
	}
	got, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "show", Arguments: map[string]any{"id": matches.Items[i].ID}})
	if err != nil {
		t.Fatal(err)
	}
	if got.IsError || len(got.Content) != 2 {
		t.Fatal("expected a receipt and a preview")
	}
	preview, ok := got.Content[1].(*mcp.ImageContent)
	if !ok || !bytes.HasPrefix(preview.Data, []byte("\x89PNG")) {
		t.Fatal("recorded preview was missing")
	}
}

func TestDispatchResourceReturnsPDF(t *testing.T) {
	packet, err := connectGuide(t).ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "guide://dispatch/sheet"})
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.Contents) != 1 || !bytes.HasPrefix(packet.Contents[0].Blob, []byte("%PDF-")) {
		t.Fatal("dispatch resource did not return a PDF")
	}
}

func TestGuidePublishesAmendedFestivalCalendar(t *testing.T) {
	session := connectGuide(t)
	uri := "guide://festival/amended"
	listed, err := session.ListResources(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(listed.Resources, func(r *mcp.Resource) bool { return r.URI == uri && r.MIMEType == "text/calendar" }) {
		t.Fatalf("the amended calendar is missing from listed resources")
	}
	result, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("got %d contents", len(result.Contents))
	}
	cal := result.Contents[0]
	if cal.URI != uri || cal.MIMEType != "text/calendar" || !strings.Contains(cal.Text, "DTSTART:20261123T184500Z\r\n") {
		t.Fatalf("calendar time or encoding differs from the published program")
	}
}

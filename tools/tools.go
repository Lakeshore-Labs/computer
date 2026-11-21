// Package tools turns the tools of an MCP server into what a shell and a script can use: a command per group of
// tools, with a subcommand per tool, and a Python package with a module per group and a function per tool.
//
// A tool named notes_listNotes is the command `notes list-notes` and the function notes.list_notes().
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/ewhauser/gbash/commands"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A Source is one MCP server. With a Name, the server is one command and one Python module of that name, and
// each of its tools a subcommand and a function. Without one, its tools are grouped by the part of their name before
// the first underscore, as a server that serves several apps names them.
type Source struct {
	Name    string
	Session *mcp.ClientSession
}

// A Tool is a listed tool: the group it is in, its name within the group, and which source serves it.
type Tool struct {
	*mcp.Tool
	Group, Short string
	Source       int // the tool's source, as an index into the sources it was listed from
}

// List is every tool the sources offer now.
func List(ctx context.Context, sources []Source) ([]Tool, error) {
	var tools []Tool
	for i, src := range sources {
		for t, err := range src.Session.Tools(ctx, nil) {
			if err != nil {
				return nil, fmt.Errorf("listing the tools of MCP server %d %s: %w", i, src.Name, err)
			}
			group, short := src.Name, t.Name
			if group == "" {
				var ok bool
				if group, short, ok = strings.Cut(t.Name, "_"); !ok {
					group, short = "tool", t.Name
				}
			}
			tools = append(tools, Tool{Tool: t, Group: group, Short: short, Source: i})
		}
	}
	return tools, nil
}

// Call calls one tool and returns its result as JSON: the structured result when the tool gives one, else its text.
func Call(ctx context.Context, cs *mcp.ClientSession, name string, arguments json.RawMessage) (json.RawMessage, error) {
	if len(arguments) == 0 {
		arguments = json.RawMessage("{}")
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return nil, err
	}
	var text strings.Builder
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			text.WriteString(t.Text)
		}
	}
	if res.IsError {
		return nil, errors.New(text.String())
	}
	if res.StructuredContent == nil {
		return json.Marshal(text.String())
	}
	structured, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return nil, err
	}
	// MCP requires a structured result to be an object, so servers wrap a list or a scalar as {"result": ...}.
	var wrapped map[string]json.RawMessage
	if json.Unmarshal(structured, &wrapped) == nil && len(wrapped) == 1 && wrapped["result"] != nil {
		return wrapped["result"], nil
	}
	return structured, nil
}

type sessionKey struct{}

// With returns a context whose programs' host calls reach the tools of sources.
func With(ctx context.Context, sources []Source) context.Context {
	return context.WithValue(ctx, sessionKey{}, sources)
}

// Host answers a program's host call (see wasm.Program.Host). The request is {"source": index, "tool": name,
// "arguments": {...}} and the reply {"ok": true, "result": ...} or {"ok": false, "error": "..."}. The generated
// Python package speaks it.
func Host(ctx context.Context, request []byte) []byte {
	reply := func(v map[string]any) []byte { b, _ := json.Marshal(v); return b }
	sources, _ := ctx.Value(sessionKey{}).([]Source)
	var in struct {
		Source    int
		Tool      string
		Arguments json.RawMessage
	}
	if err := json.Unmarshal(request, &in); err != nil || in.Tool == "" || in.Source < 0 || in.Source >= len(sources) {
		return reply(map[string]any{"ok": false, "error": "no tool to call"})
	}
	result, err := Call(ctx, sources[in.Source].Session, in.Tool, in.Arguments)
	if err != nil {
		return reply(map[string]any{"ok": false, "error": err.Error()})
	}
	return reply(map[string]any{"ok": true, "result": result})
}

// schema is the part of a tool's input schema a command line and a function signature are made from.
type schema struct {
	Properties map[string]struct {
		Type        any
		Description string
		Enum        []any
	}
	Required []string
}

func schemaOf(t Tool) (s schema) {
	b, _ := json.Marshal(t.InputSchema)
	_ = json.Unmarshal(b, &s)
	return s
}

func (s schema) required(p string) bool {
	for _, r := range s.Required {
		if r == p {
			return true
		}
	}
	return false
}

// params is the schema's parameters, the required ones first, each group by name.
func (s schema) params() []string {
	ps := make([]string, 0, len(s.Properties))
	for p := range s.Properties {
		ps = append(ps, p)
	}
	sort.Slice(ps, func(i, j int) bool {
		if s.required(ps[i]) != s.required(ps[j]) {
			return s.required(ps[i])
		}
		return ps[i] < ps[j]
	})
	return ps
}

// Groups sorts tools by their group.
func Groups(tools []Tool) map[string][]Tool {
	groups := map[string][]Tool{}
	for _, t := range tools {
		groups[t.Group] = append(groups[t.Group], t)
	}
	return groups
}

// words splits a name at its capitals and at anything that is not an ASCII letter or digit: listNotes is list,
// notes. Only ASCII is kept: Python and a shell disagree with Unicode about which other characters make a name.
func words(s string) []string {
	var out []string
	var w strings.Builder
	flush := func() {
		if w.Len() > 0 {
			out = append(out, w.String())
			w.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			flush()
			w.WriteRune(r + 'a' - 'A')
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			w.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return out
}

// Kebab is a name as a command line spells it: listNotes is list-notes.
func Kebab(s string) string { return strings.Join(words(s), "-") }

// unique is name, or name with the first of 2, 3, ... after sep that makes it one not yet in taken; it is then
// taken. Two tools whose names spell the same both stay reachable.
func unique(name, sep string, taken map[string]bool) string {
	out := name
	for n := 2; out == "" || taken[out]; n++ {
		out = name + sep + strconv.Itoa(n)
	}
	taken[out] = true
	return out
}

// byName sorts tools by their own names, so that names made unique are the same on every run.
func byName(group []Tool) []Tool {
	group = append([]Tool(nil), group...)
	sort.Slice(group, func(i, j int) bool { return group[i].Name < group[j].Name })
	return group
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// Command is one group of tools as a command called name: `name --help` lists its tools, `name tool --help` a
// tool's arguments, and `name tool --argument value` calls the tool and prints its result: text as it is, anything
// else as JSON.
func Command(name string, sources []Source, group []Tool) commands.Command {
	subs, taken := map[string]Tool{}, map[string]bool{}
	for _, t := range byName(group) {
		subs[unique(Kebab(t.Short), "-", taken)] = t
	}
	return commands.DefineCommand(name, func(ctx context.Context, inv *commands.Invocation) error {
		args := inv.Args
		if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
			fmt.Fprintf(inv.Stdout, "usage: %s <tool> [--argument value ...]   (%s <tool> --help for its arguments)\n\ntools:\n", name, name)
			for _, sub := range sortedKeys(subs) {
				first, _, _ := strings.Cut(strings.TrimSpace(subs[sub].Description), "\n")
				fmt.Fprintf(inv.Stdout, "  %-28s %s\n", sub, first)
			}
			return nil
		}
		tool, ok := subs[args[0]]
		if !ok {
			return commands.Exitf(inv, 2, "%s: no tool %q; run `%s --help`", name, args[0], name)
		}
		s := schemaOf(tool)
		flags := map[string]string{} // --note-id and --noteId both name noteId
		for p := range s.Properties {
			flags[Kebab(p)], flags[p] = p, p
		}
		values := map[string]any{}
		for i := 1; i < len(args); i++ {
			if args[i] == "--help" || args[i] == "-h" {
				fmt.Fprintf(inv.Stdout, "%s %s: %s\n\narguments:\n", name, args[0], strings.TrimSpace(tool.Description))
				for _, p := range s.params() {
					prop := s.Properties[p]
					typ, _ := prop.Type.(string)
					note := ""
					if s.required(p) {
						note = " (required)"
					}
					if len(prop.Enum) > 0 {
						enum, _ := json.Marshal(prop.Enum)
						note += " one of " + string(enum)
					}
					fmt.Fprintf(inv.Stdout, "  --%-24s %s%s %s\n", Kebab(p), typ, note, oneLine(prop.Description))
				}
				fmt.Fprintln(inv.Stdout, "  --json '{...}'             every argument as one JSON object")
				return nil
			}
			flag, value, inline := strings.Cut(strings.TrimPrefix(args[i], "--"), "=")
			if !strings.HasPrefix(args[i], "--") {
				return commands.Exitf(inv, 2, "%s %s: unexpected %q; arguments are --name value", name, args[0], args[i])
			}
			if !inline {
				if i++; i >= len(args) {
					return commands.Exitf(inv, 2, "%s %s: --%s needs a value", name, args[0], flag)
				}
				value = args[i]
			}
			if flag == "json" {
				var object map[string]any
				if err := json.Unmarshal([]byte(value), &object); err != nil {
					return commands.Exitf(inv, 2, "%s %s: --json: %v", name, args[0], err)
				}
				for k, v := range object {
					values[k] = v
				}
				continue
			}
			p, ok := flags[flag]
			if !ok {
				return commands.Exitf(inv, 2, "%s %s: no argument --%s; run `%s %s --help`", name, args[0], flag, name, args[0])
			}
			var err error
			switch typ, _ := s.Properties[p].Type.(string); typ {
			case "string":
				values[p] = value
			case "integer":
				values[p], err = strconv.ParseInt(value, 10, 64)
			case "number":
				values[p], err = strconv.ParseFloat(value, 64)
			case "boolean":
				values[p], err = strconv.ParseBool(value)
			default: // an object, a list or a value of several types: JSON when it parses, else the text
				var v any
				if json.Unmarshal([]byte(value), &v) != nil {
					v = value
				}
				values[p] = v
			}
			if err != nil {
				return commands.Exitf(inv, 2, "%s %s: --%s: %q is not a %v", name, args[0], flag, value, s.Properties[p].Type)
			}
		}
		arguments, _ := json.Marshal(values)
		result, err := Call(ctx, sources[tool.Source].Session, tool.Name, arguments)
		if err != nil {
			return commands.Exitf(inv, 1, "%s %s: %v", name, args[0], err)
		}
		var pretty bytes.Buffer
		if text := ""; json.Unmarshal(result, &text) == nil {
			pretty.WriteString(strings.TrimSuffix(text, "\n"))
		} else if json.Indent(&pretty, result, "", "  ") != nil {
			pretty.Write(result)
		}
		pretty.WriteByte('\n')
		if _, err := inv.Stdout.Write(pretty.Bytes()); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			return err
		}
		return nil
	})
}

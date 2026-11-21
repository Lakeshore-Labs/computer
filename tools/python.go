package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// pythonInit is the package's own module. call takes source and tool positionally only, so that a tool with an
// argument named source or tool can still be called. _host is the built-in module of the Python build that makes the host call.
const pythonInit = `%s
import json as _json
import _host

GROUPS = %s


class ToolError(Exception):
    pass


def call(source, tool, /, **arguments):
    """Call a tool of the numbered source by its full name and return its result as Python data."""
    reply = _json.loads(_host.call(_json.dumps({"source": source, "tool": tool, "arguments": arguments}).encode()))
    if not reply.get("ok"):
        raise ToolError(reply.get("error"))
    return reply.get("result")
`

var pythonKeywords = map[string]bool{"False": true, "None": true, "True": true, "and": true, "as": true, "assert": true,
	"async": true, "await": true, "break": true, "class": true, "continue": true, "def": true, "del": true, "elif": true,
	"else": true, "except": true, "finally": true, "for": true, "from": true, "global": true, "if": true, "import": true,
	"in": true, "is": true, "lambda": true, "nonlocal": true, "not": true, "or": true, "pass": true, "raise": true,
	"return": true, "try": true, "while": true, "with": true, "yield": true}

var pythonTypes = map[string]string{"string": "str", "integer": "int", "number": "float", "boolean": "bool", "array": "list", "object": "dict"}

// literal is s as a Python string literal. Names and descriptions come from an MCP server and are written into
// Python source only this way, so that none of their text can be read as code. A JSON string is a Python string.
func literal(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// identifier is a name as Python spells it, and one not yet in taken: listNotes is list_notes, a keyword or a
// name that starts with a digit gets a leading x_, and a second list_notes is list_notes_2.
func identifier(s string, taken map[string]bool) string {
	name := strings.Join(words(s), "_")
	if name == "" || pythonKeywords[name] || name[0] >= '0' && name[0] <= '9' {
		name = "x_" + name
	}
	return unique(name, "_", taken)
}

// WritePython writes the tools as a Python package in the directory dir, replacing what is there: a module per
// group and a function per tool, with the tool's description and arguments as the function's own. A script that
// has dir's parent on its path writes `from <dir's name> import notes; notes.list_notes()`.
func WritePython(dir string, tools []Tool) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	groups := Groups(tools)
	names := sortedKeys(groups)
	modules := map[string]bool{"call": true} // a group's module must not hide the package's own call
	list, _ := json.Marshal(names)
	doc := "The tools, a module per group: `from " + filepath.Base(dir) + " import <group>`, then help(<group>).\n\nGroups: " + strings.Join(names, ", ")
	init := fmt.Sprintf(pythonInit, literal(doc), list)
	if err := os.WriteFile(filepath.Join(dir, "__init__.py"), []byte(init), 0o600); err != nil {
		return err
	}
	for _, group := range names {
		var m strings.Builder
		fmt.Fprintf(&m, "%s\nfrom . import call as _call\n", literal("The "+group+" tools."))
		functions := map[string]bool{"_call": true}
		for _, t := range byName(groups[group]) {
			s := schemaOf(t)
			arguments := map[string]bool{}
			var signature, doc, pass []string
			for _, p := range s.params() {
				prop := s.Properties[p]
				typ, _ := prop.Type.(string)
				py := pythonTypes[typ]
				if py == "" {
					py = "object"
				}
				arg, line := identifier(p, arguments), ""
				if s.required(p) {
					signature = append(signature, arg+": "+py)
					line = "    " + arg + " (" + py + ")"
				} else {
					signature = append(signature, arg+": "+py+" | None = None")
					line = "    " + arg + " (" + py + ", optional)"
				}
				if d := oneLine(prop.Description); d != "" {
					line += ": " + d
				}
				if len(prop.Enum) > 0 {
					enum, _ := json.Marshal(prop.Enum)
					line += " One of " + string(enum) + "."
				}
				doc = append(doc, line)
				key, _ := json.Marshal(p)
				pass = append(pass, string(key)+": "+arg)
			}
			star := ""
			if len(signature) > 0 {
				star = "*, "
			}
			text := strings.TrimSpace(t.Description)
			if len(doc) > 0 {
				text += "\n\n" + strings.Join(doc, "\n")
			}
			name, _ := json.Marshal(t.Name)
			fmt.Fprintf(&m, "\n\ndef %s(%s%s):\n    %s\n    return _call(%d, %s, **{k: v for k, v in {%s}.items() if v is not None})\n",
				identifier(t.Short, functions), star, strings.Join(signature, ", "), literal(text), t.Source, name, strings.Join(pass, ", "))
		}
		if err := os.WriteFile(filepath.Join(dir, identifier(group, modules)+".py"), []byte(m.String()), 0o600); err != nil {
			return err
		}
	}
	return nil
}

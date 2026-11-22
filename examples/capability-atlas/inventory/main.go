// Inventory functions declared on exported types and top-level functions in the library itself.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type symbol struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Line   int    `json:"line"`
}

func receiver(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiver(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}
func main() {
	if len(os.Args) != 2 {
		panic("usage: inventory checkout")
	}
	var rows []symbol
	for _, dir := range []string{".", "agent", "extra", "files", "tools", "wasm"} {
		path := filepath.Join(os.Args[1], dir)
		pkg, err := build.Default.ImportDir(path, 0)
		if err != nil {
			panic(err)
		}
		fset := token.NewFileSet()
		for _, name := range pkg.GoFiles {
			src := filepath.Join(dir, name)
			file, err := parser.ParseFile(fset, filepath.Join(path, name), nil, 0)
			if err != nil {
				panic(err)
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || !fn.Name.IsExported() {
					continue
				}
				full := pkg.Name + "."
				if fn.Recv != nil {
					r := receiver(fn.Recv.List[0].Type)
					if !ast.IsExported(r) {
						continue
					}
					full += r + "."
				}
				rows = append(rows, symbol{full + fn.Name.Name, filepath.ToSlash(strings.TrimPrefix(src, "./")), fset.Position(fn.Pos()).Line})
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rows); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

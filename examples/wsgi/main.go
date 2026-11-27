// Command wsgi demonstrates embedding Computer in a Go web server.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Lakeshore-Labs/computer"
	"github.com/Lakeshore-Labs/computer/tools"
	"github.com/Lakeshore-Labs/computer/wasm"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	appDir := flag.String("app", "examples/wsgi/app", "Flask project directory, including vendor/")
	dataDir := flag.String("data", "", "directory for the SQLite database")
	addr := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	deepWiki := flag.Bool("deepwiki", false, "use the public DeepWiki MCP server")
	flag.Parse()
	if *dataDir == "" {
		log.Fatal("provide an existing directory with -data")
	}
	if err := run(*appDir, *dataDir, *addr, *deepWiki); err != nil {
		log.Fatal(err)
	}
}

func run(appDir, dataDir, addr string, deepWiki bool) error {
	ctx := context.Background()
	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var transport mcp.Transport
	if deepWiki {
		transport = &mcp.StreamableClientTransport{Endpoint: "https://mcp.deepwiki.com/mcp"}
	} else {
		server := mcp.NewServer(&mcp.Implementation{Name: "github-readme"}, nil)
		mcp.AddTool(server, &mcp.Tool{
			Name: "read_wiki_structure", Description: "List the headings in a public GitHub README",
		}, readOutline)
		host, peer := mcp.NewInMemoryTransports()
		session, err := server.Connect(ctx, host, nil)
		if err != nil {
			return err
		}
		defer session.Close()
		transport = peer
	}
	wiki, err := mcp.NewClient(&mcp.Implementation{Name: "computer-wsgi-demo"}, nil).Connect(connectCtx, transport, nil)
	if err != nil {
		return err
	}
	defer wiki.Close()

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	python, err := computer.EmbeddedPython(ctx, wasm.Program{CacheDir: filepath.Join(cacheDir, "computer"), MaxRuns: 2})
	if err != nil {
		return err
	}
	c, err := computer.New(computer.Config{
		WorkDir: "/app",
		Mounts: []computer.Mount{
			{Host: appDir, Guest: "/app", ReadOnly: true},
			{Host: dataDir, Guest: "/data"},
		},
		Tools:  []tools.Source{{Name: "wiki", Session: wiki}},
		Python: python,
	})
	if err != nil {
		return err
	}
	defer c.Close()

	flask, err := c.WSGIHandler("app:app", 2*time.Minute)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok\n"))
	})
	mux.Handle("/", flask)
	log.Printf("open http://%s", addr)
	return (&http.Server{
		Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second,
	}).ListenAndServe()
}

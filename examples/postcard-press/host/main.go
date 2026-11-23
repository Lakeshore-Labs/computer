// Command postcard-press serves one shared Computer workspace to local MCP clients.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/Lakeshore-Labs/computer"
	"github.com/Lakeshore-Labs/computer/extra"
	"github.com/Lakeshore-Labs/computer/wasm"
	"github.com/ewhauser/gbash"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/afero"
)

func run(ctx context.Context) error {
	collection := flag.String("collection", "", "directory containing the Getty and Cleveland source folders")
	recipe := flag.String("recipe", "", "directory containing postcard.py")
	ui := flag.String("ui", "", "directory containing index.html, desk.js and style.css")
	python := flag.String("python-dir", "", "Computer WASI Python release")
	flag.Parse()
	if *collection == "" || *recipe == "" || *ui == "" || *python == "" {
		return errors.New("supply -collection, -recipe, -ui and -python-dir")
	}
	runtime, err := computer.LoadPython(ctx, filepath.Join(*python, "python.wasm"), filepath.Join(*python, "lib"), wasm.Program{MemoryPages: 8192, HostModule: "computer"})
	if err != nil {
		return err
	}
	c, err := computer.New(computer.Config{WorkDir: "/desk", Commands: extra.Commands(), Python: runtime, Mounts: []computer.Mount{
		{Guest: "/desk", FS: afero.NewMemMapFs()},
		{Guest: "/collection", Host: *collection, ReadOnly: true},
		{Guest: "/recipe", Host: *recipe, ReadOnly: true},
	}})
	if err != nil {
		return err
	}
	defer c.Close()
	result, err := c.Exec(ctx, &gbash.ExecutionRequest{Script: `set -eo pipefail
for museum in getty cleveland; do
  magick /collection/$museum/painting.jpg -thumbnail 260x180 $museum.jpg
done`})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("prepare the two print images: %s", result.Stderr)
	}
	server, err := c.MCPServer(ctx, 45*time.Second)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	addr := listener.Addr().String()
	origin := "http://" + addr
	mux := http.NewServeMux()
	for route, name := range map[string]string{"GET /{$}": "index.html", "GET /desk.js": "desk.js", "GET /style.css": "style.css"} {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, filepath.Join(*ui, name)) })
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	mux.Handle("/mcp", handler)
	web := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != addr || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != origin) {
			http.Error(w, "Use the local press address.", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})}
	go func() { <-ctx.Done(); _ = web.Close() }()
	fmt.Println(origin)
	if err := web.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

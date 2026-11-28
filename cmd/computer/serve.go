package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Lakeshore-Labs/computer"
)

// serve is the serve mode: a folder of the computer on a local address until ctx ends, as python3 -m http.server
// serves one and with its flags, where an application is also asked for by its name (see computer.WebHandler).
// It is for trying a page; a host that serves people mounts the handler on its own mux.
func serve(ctx context.Context, c *computer.Computer, args []string) error {
	flags := flag.NewFlagSet("computer serve", flag.ContinueOnError)
	bind := flags.String("b", "127.0.0.1", "the address to listen on")
	dir := flags.String("d", "", "the folder of the computer to serve; the working directory when empty")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("usage: computer [flags] serve [-b address] [-d folder] [port]")
	}
	handler := computer.WebHandler(func(*http.Request) (*computer.Computer, string, error) { return c, *dir, nil })
	listener, err := net.Listen("tcp", net.JoinHostPort(*bind, cmp.Or(flags.Arg(0), "8000")))
	if err != nil {
		return err
	}
	var connections sync.WaitGroup
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx }, // an interrupt stops the requests that run
		ConnState: func(_ net.Conn, state http.ConnState) {
			switch state {
			case http.StateNew:
				connections.Add(1)
			case http.StateClosed, http.StateHijacked:
				connections.Done()
			}
		},
	}
	defer connections.Wait() // the computer stays open until its last handler returns
	defer server.Close()
	defer context.AfterFunc(ctx, func() { _ = server.Close() })()
	fmt.Fprintf(os.Stderr, "computer: serving at http://%s/\n", listener.Addr())
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

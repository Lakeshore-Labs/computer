package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

func serveWSGI(ctx context.Context, addr string, handler http.Handler) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	stop := make(chan struct{})
	defer close(stop)
	done := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
			done <- server.Shutdown(context.Background())
		case <-stop:
		}
	}()
	fmt.Fprintf(os.Stderr, "computer: WSGI listening on http://%s\n", listener.Addr())
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return <-done
	}
	return err
}

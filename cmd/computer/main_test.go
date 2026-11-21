package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ewhauser/gbash"

	"github.com/Lakeshore-Labs/computer"
)

func TestTheCacheMayNotBeInsideAMount(t *testing.T) {
	home := t.TempDir()
	cache := filepath.Join(home, "cache", "computer")
	other := t.TempDir()
	for name, c := range map[string]struct {
		host string
		ok   bool
	}{"a mount that holds it": {home, false}, "the cache itself": {cache, false}, "a mount within it": {filepath.Join(cache, "wazero"), false}, "a mount elsewhere": {other, true}} {
		if err := os.MkdirAll(c.host, 0o700); err != nil {
			t.Fatal(err)
		}
		err := outside(cache, []computer.Mount{{Guest: "/home/user", Host: c.host}})
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTabRunsOnlyACommandThatCallsTools(t *testing.T) {
	dir := t.TempDir()
	c, err := computer.New(computer.Config{Mounts: []computer.Mount{{Guest: "/home/user", Host: dir}}, WorkDir: "/home/user"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := c.Exec(ctx, &gbash.ExecutionRequest{Script: "true"}); err != nil {
		t.Fatal(err)
	}
	for _, before := range []string{"touch ran-touch -", "$(touch${IFS}ran-substitution) -", "touch "} {
		candidates(ctx, c, "/home/user", before, before[len(before)-1:])
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		t.Errorf("completing a line ran a command: %s exists", e.Name())
	}
}

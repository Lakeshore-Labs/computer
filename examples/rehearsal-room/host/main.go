// Register the WASI score writer as a Computer shell command and run both parts.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Lakeshore-Labs/computer"
	"github.com/Lakeshore-Labs/computer/wasm"
	"github.com/ewhauser/gbash/commands"
)

func run() error {
	if len(os.Args) != 4 {
		return errors.New("usage: rehearsal part.wasm score-directory new-output-directory")
	}
	ctx := context.Background()
	program, err := os.ReadFile(os.Args[1])
	if err != nil {
		return err
	}
	module, err := wasm.Compile(ctx, wasm.Program{Binary: program, MaxRuns: 2})
	if err != nil {
		return err
	}
	defer module.Close(ctx)
	command := commands.DefineCommand("part", func(ctx context.Context, inv *commands.Invocation) error {
		code, err := module.Run(ctx, wasm.Run{Args: append([]string{"part"}, inv.Args...), Stdin: inv.Stdin, Stdout: inv.Stdout, Stderr: inv.Stderr})
		if err != nil {
			return err
		}
		if code != 0 {
			return &commands.ExitError{Code: code}
		}
		return nil
	})
	out := os.Args[3]
	if err := os.Mkdir(out, 0755); err != nil {
		return err
	}
	dir := filepath.Join(out, "parts")
	if err := os.Mkdir(dir, 0755); err != nil {
		return err
	}
	c, err := computer.New(computer.Config{WorkDir: "/parts", Mounts: []computer.Mount{{Guest: "/score", Host: os.Args[2], ReadOnly: true}, {Guest: "/parts", Host: dir}}, Commands: []commands.Command{command}})
	if err != nil {
		return err
	}
	defer c.Close()
	type result struct {
		Name   string `json:"instrument"`
		Output string `json:"hashes"`
		Err    error  `json:"-"`
	}
	completed := make(chan result, 2)
	for _, instrument := range []string{"flute", "clarinet"} {
		go func() {
			var stdout, stderr bytes.Buffer
			script := fmt.Sprintf("set -e\npart %s musicxml < /score/score.csv > %s.musicxml\npart %s midi < /score/score.csv > %s.mid\nsha256sum %s.musicxml %s.mid", instrument, instrument, instrument, instrument, instrument, instrument)
			code, err := c.Environment().Run(ctx, script, "/parts", nil, false, &stdout, &stderr)
			if err == nil && code != 0 {
				err = fmt.Errorf("%s exited %d: %s", instrument, code, stderr.String())
			}
			completed <- result{instrument, stdout.String(), err}
		}()
	}
	results := map[string]string{}
	var failures error
	for range 2 {
		r := <-completed
		failures = errors.Join(failures, r.Err)
		results[r.Name] = r.Output
	}
	if failures != nil {
		return failures
	}
	sum := module.Sum()
	receipt := map[string]any{"command": "part", "compiled_modules": 1, "shells": 2, "module_sum": hex.EncodeToString(sum[:]), "parts": results}
	b, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "computer.json"), append(b, '\n'), 0644)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

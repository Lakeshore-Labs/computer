package wasm

import (
	"bytes"
	"context"
	_ "embed"
	"strings"
	"testing"
)

//go:embed testdata/door.wasm
var door []byte

func TestTheDoorRepliesInMemoryTheProgramGivesAndRefusesNone(t *testing.T) {
	ctx := context.Background()
	m, err := Compile(ctx, Program{Binary: door, Host: func(_ context.Context, req []byte) []byte {
		return append([]byte("pong to "), req...)
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(ctx)
	for args, want := range map[string]string{"door": "pong to ping\n", "door null": "status -2\n"} {
		var out bytes.Buffer
		if code, err := m.Run(ctx, Run{Args: strings.Fields(args), Stdout: &out}); code != 0 || err != nil || out.String() != want {
			t.Errorf("%s: exit %d, %v, %q; want %q", args, code, err, out.String(), want)
		}
	}
}

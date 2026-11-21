package computer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ewhauser/gbash"
)

func TestAProgramThatReadsNoInputLeavesItForTheNextCommand(t *testing.T) {
	c := newComputer(t, Config{Python: python(t)})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	res, err := c.Exec(ctx, &gbash.ExecutionRequest{Script: `python3 -c "pass"; cat`, Stdin: strings.NewReader("kept for cat\n")})
	if err != nil || res.Stdout != "kept for cat\n" {
		t.Fatalf("cat printed %q (%v): the program took input it did not read", res.Stdout, err)
	}
}

package computer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Lakeshore-Labs/gbash"
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

// Each run has a memory cap, so the number of runs at once is what bounds the memory a script can take.
func TestNoMoreProgramsRunAtOnceThanTheLimit(t *testing.T) {
	c := newComputer(t, Config{Python: python(t)}) // which allows two at once
	start := time.Now()
	res := exec(t, c, `for i in 1 2 3 4; do python3 -c "import time; time.sleep(1); print('done')" & done; wait`, time.Minute)
	if strings.Count(res.Stdout, "done") != 4 {
		t.Fatalf("stdout %q, stderr %q", res.Stdout, res.Stderr)
	}
	if took := time.Since(start); took < 2*time.Second {
		t.Errorf("four one-second programs ended in %v: more than two ran at once", took)
	}
}

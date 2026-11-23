// Command what-if-workspaces compares two edits to a ferry schedule shared by three Computers.
package main

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/Lakeshore-Labs/computer"
	"github.com/Lakeshore-Labs/computer/extra"
	"github.com/ewhauser/gbash"
	"github.com/spf13/afero"
)

//go:embed schedule.csv
var schedule embed.FS

func compare(output string) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(output, 0o755); err != nil {
		return fmt.Errorf("choose a fresh output directory: %w", err)
	}
	ctx := context.Background()
	original, err := schedule.ReadFile("schedule.csv")
	if err != nil {
		return err
	}
	shared := afero.NewMemMapFs()
	if err := afero.WriteFile(shared, "/schedule.csv", original, 0o644); err != nil {
		return err
	}
	base := afero.NewReadOnlyFs(shared)
	plans := []struct {
		name, before, after string
		total               int
	}{
		{"published", "", "", 11},
		{"matinee", "12:00,3", "12:00,7", 15},
		{"evening", "20:00,2", "20:00,6", 15},
	}
	var outputs [3][]byte
	var computers []*computer.Computer
	for i, plan := range plans {
		layer := afero.NewCopyOnWriteFs(base, afero.NewMemMapFs())
		c, err := computer.New(computer.Config{
			WorkDir: "/work",
			Mounts:  []computer.Mount{{Guest: "/work", FS: layer}},
		})
		if err != nil {
			return err
		}
		defer c.Close()
		computers = append(computers, c)
		env := c.Environment()
		input, err := env.Read(ctx, "schedule.csv", 0, -1)
		if err != nil {
			return fmt.Errorf("read %s schedule: %w", plan.name, err)
		}
		if !bytes.Equal(input, original) {
			return fmt.Errorf("plan %s did not start from the published schedule", plan.name)
		}
		outputs[i] = input
		if plan.before != "" {
			outputs[i] = bytes.Replace(input, []byte(plan.before), []byte(plan.after), 1)
			if err := env.Write(ctx, "schedule.csv", outputs[i], false); err != nil {
				return err
			}
		}
		result, err := c.Exec(ctx, &gbash.ExecutionRequest{
			Script: "awk -F, 'NR > 1 {n += $2} END {print n}' schedule.csv",
		})
		if err != nil {
			return err
		}
		if result.ExitCode != 0 || result.Stdout != fmt.Sprintln(plan.total) {
			return fmt.Errorf("%s total: %q (%s)", plan.name, result.Stdout, result.Stderr)
		}
		fmt.Printf("%-10s %d sailings\n", plan.name, plan.total)
	}

	// Read every workspace again after both edits. Export exactly what each environment sees.
	for i, c := range computers {
		data, err := c.Environment().Read(ctx, "schedule.csv", 0, -1)
		if err != nil {
			return err
		}
		if !bytes.Equal(data, outputs[i]) {
			return fmt.Errorf("the schedule for %s changed after another plan was edited", plans[i].name)
		}
		if err := os.WriteFile(filepath.Join(output, plans[i].name+".csv"), data, 0o644); err != nil {
			return err
		}
	}
	c, err := computer.New(computer.Config{
		WorkDir:  "/work",
		Mounts:   []computer.Mount{{Guest: "/work", Host: output}},
		Commands: extra.Commands(),
	})
	if err != nil {
		return err
	}
	defer c.Close()
	result, err := c.Exec(ctx, &gbash.ExecutionRequest{Script: `set -eo pipefail
paste -d, published.csv matinee.csv evening.csv | awk -F, 'BEGIN {print "time,published,matinee,evening"} NR>1 {print $1 "," $2 "," $4 "," $6}' > comparison.csv
chart bar --x time --y published,matinee,evening comparison.csv --out comparison.png
identify comparison.png`})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return errors.New(result.Stderr)
	}
	fmt.Print(result.Stdout)
	return nil
}

func main() {
	output := flag.String("output", "", "new directory for the three schedules and chart")
	flag.Parse()
	if *output == "" {
		log.Fatal("provide -output with a fresh directory name")
	}
	if err := compare(*output); err != nil {
		log.Fatal(err)
	}
}

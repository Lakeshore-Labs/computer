// Package extra holds commands for everyday data and document work that the shell lacks. They are not part of the
// library's default computer: an embedder passes Commands in Config.Commands. A command reaches files only through
// its invocation's filesystem and talks only on its invocation's streams.
package extra

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/ewhauser/gbash/commands"
)

// Commands is every command in the package.
func Commands() []commands.Command {
	return []commands.Command{imageCommand}
}

// usageError is a mistake in the command line, as opposed to a failure of the work: it exits 2, not 1.
type usageError string

func (e usageError) Error() string { return string(e) }

func usagef(format string, args ...any) error { return usageError(fmt.Sprintf(format, args...)) }

// exit prints err as "name: message" and returns it as the command's exit status.
func exit(inv *commands.Invocation, name string, err error) error {
	code := 1
	if errors.As(err, new(usageError)) {
		code = 2
	}
	return commands.Exitf(inv, code, "%s: %v", name, err)
}

// parse reads the flags in args wherever they stand among the other arguments, as the shell's own commands do,
// and returns the other arguments. The flag package stops at the first of them, so it is run again after each.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var rest []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, usageError(err.Error())
		}
		if fs.NArg() == 0 {
			return rest, nil
		}
		rest = append(rest, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

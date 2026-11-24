// Package extra holds the PDF commands the shell lacks, pdftotext and pdftoppm. They are not part of the
// library's default computer: an embedder passes Commands in Config.Commands. A command reaches files only through
// its invocation's filesystem and talks only on its invocation's streams.
package extra

import (
	"errors"
	"fmt"

	"github.com/Lakeshore-Labs/gbash/commands"
)

// Commands is every command in the package.
func Commands() []commands.Command {
	return []commands.Command{pdftotextCommand, pdftoppmCommand}
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

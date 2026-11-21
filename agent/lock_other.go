//go:build !unix

package agent

// lock does nothing where there is no flock: there, two processes given one conversation are not kept apart.
func lock(string) (func(), error) { return func() {}, nil }

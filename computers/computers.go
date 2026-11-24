// Package computers gives each session of an agent that serves many its own computer: one Environment whose calls
// go to the computer of the session their context names, made when that session first uses it and closed once it
// has been idle. Which session a context belongs to, and what its computer is, are the host's to say.
package computers

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"sync"
	"time"

	"github.com/Lakeshore-Labs/computer"
)

// Computers is an Environment for many sessions. Its methods are safe for concurrent use.
type Computers struct {
	session func(context.Context) (string, bool)
	config  func(ctx context.Context, session string) (computer.Config, error)
	idle    time.Duration

	mu     sync.Mutex
	open   map[string]*entry
	closed chan struct{}
}

type entry struct {
	ready chan struct{} // closed once c or err is set
	c     *computer.Computer
	tools []io.Closer // the tool sources' sessions, closed with the computer
	err   error
	busy  int // calls under way, during which the computer is not closed
	used  time.Time
}

// New returns the computers of the sessions that session names from a call's context. config gives a session's
// computer when it is first used, with ctx that first call's. idle is how long a computer may go unused before it
// is closed; zero keeps every computer until Close. The tool sources' sessions in a config are closed with its
// computer.
func New(session func(context.Context) (string, bool), config func(ctx context.Context, session string) (computer.Config, error), idle time.Duration) *Computers {
	cs := &Computers{session: session, config: config, idle: idle, open: map[string]*entry{}, closed: make(chan struct{})}
	if idle > 0 {
		go cs.reap()
	}
	return cs
}

var errNoSession = errors.New("sessions: the call names no session")

// use is the environment of ctx's session, made if it has none, and done, to be called when the call ends.
func (cs *Computers) use(ctx context.Context) (env *computer.Environment, done func(), err error) {
	id, ok := cs.session(ctx)
	if !ok {
		return nil, nil, errNoSession
	}
	cs.mu.Lock()
	e, found := cs.open[id]
	if !found {
		e = &entry{ready: make(chan struct{})}
		cs.open[id] = e
	}
	e.busy++
	cs.mu.Unlock()

	if !found {
		cfg, err := cs.config(ctx, id)
		if err == nil {
			e.c, err = computer.New(cfg)
		}
		for _, s := range cfg.Tools {
			if s.Session != nil {
				e.tools = append(e.tools, s.Session)
			}
		}
		e.err = err
		close(e.ready)
	} else {
		select {
		case <-e.ready:
		case <-ctx.Done():
			cs.release(id, e)
			return nil, nil, ctx.Err()
		}
	}
	if e.err != nil {
		cs.mu.Lock()
		if cs.open[id] == e { // a later call tries again
			delete(cs.open, id)
		}
		cs.mu.Unlock()
		if !found {
			closeAll(e.tools)
		}
		return nil, nil, e.err
	}
	return e.c.Environment(), func() { cs.release(id, e) }, nil
}

func (cs *Computers) release(id string, e *entry) {
	cs.mu.Lock()
	e.busy--
	e.used = time.Now()
	cs.mu.Unlock()
}

// reap closes the computers idle for longer than cs.idle, until Close.
func (cs *Computers) reap() {
	tick := time.NewTicker(max(cs.idle/2, time.Second))
	defer tick.Stop()
	for {
		select {
		case <-cs.closed:
			return
		case now := <-tick.C:
			cs.mu.Lock()
			var idle []*entry
			for id, e := range cs.open {
				if e.busy == 0 && e.c != nil && now.Sub(e.used) > cs.idle {
					delete(cs.open, id)
					idle = append(idle, e)
				}
			}
			cs.mu.Unlock()
			for _, e := range idle {
				_ = e.close()
			}
		}
	}
}

func (e *entry) close() error {
	return errors.Join(e.c.Close(), closeAll(e.tools))
}

func closeAll(cs []io.Closer) error {
	var err error
	for _, c := range cs {
		err = errors.Join(err, c.Close())
	}
	return err
}

// End closes session's computer, if it has one, as when its thread is deleted. A call under way keeps the computer
// it has.
func (cs *Computers) End(session string) error {
	cs.mu.Lock()
	e, ok := cs.open[session]
	if ok {
		delete(cs.open, session)
	}
	cs.mu.Unlock()
	if !ok {
		return nil
	}
	<-e.ready
	if e.c == nil {
		return nil
	}
	return e.close()
}

// Close closes every computer and stops closing idle ones.
func (cs *Computers) Close() error {
	cs.mu.Lock()
	open := cs.open
	cs.open = map[string]*entry{}
	cs.mu.Unlock()
	select {
	case <-cs.closed:
	default:
		close(cs.closed)
	}
	var err error
	for _, e := range open {
		<-e.ready
		if e.c != nil {
			err = errors.Join(err, e.close())
		}
	}
	return err
}

// The Environment's methods, each the session's computer's.

func (cs *Computers) Run(ctx context.Context, script, dir string, env map[string]string, replaceEnv bool, stdout, stderr io.Writer) (int, error) {
	e, done, err := cs.use(ctx)
	if err != nil {
		return 0, err
	}
	defer done()
	return e.Run(ctx, script, dir, env, replaceEnv, stdout, stderr)
}

func (cs *Computers) Stat(ctx context.Context, name string, follow bool) (fs.FileInfo, error) {
	e, done, err := cs.use(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	return e.Stat(ctx, name, follow)
}

func (cs *Computers) Read(ctx context.Context, name string, offset, length int64) ([]byte, error) {
	e, done, err := cs.use(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	return e.Read(ctx, name, offset, length)
}

func (cs *Computers) Write(ctx context.Context, name string, content []byte, appending bool) error {
	e, done, err := cs.use(ctx)
	if err != nil {
		return err
	}
	defer done()
	return e.Write(ctx, name, content, appending)
}

func (cs *Computers) Truncate(ctx context.Context, name string, size int64) error {
	e, done, err := cs.use(ctx)
	if err != nil {
		return err
	}
	defer done()
	return e.Truncate(ctx, name, size)
}

func (cs *Computers) List(ctx context.Context, name string) ([]fs.FileInfo, error) {
	e, done, err := cs.use(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	return e.List(ctx, name)
}

func (cs *Computers) Mkdir(ctx context.Context, name string, parents bool) error {
	e, done, err := cs.use(ctx)
	if err != nil {
		return err
	}
	defer done()
	return e.Mkdir(ctx, name, parents)
}

func (cs *Computers) Remove(ctx context.Context, name string, recursive bool) error {
	e, done, err := cs.use(ctx)
	if err != nil {
		return err
	}
	defer done()
	return e.Remove(ctx, name, recursive)
}

func (cs *Computers) Rename(ctx context.Context, from, to string) error {
	e, done, err := cs.use(ctx)
	if err != nil {
		return err
	}
	defer done()
	return e.Rename(ctx, from, to)
}

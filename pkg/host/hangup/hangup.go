// Package hangup keeps hardn from waiting on a terminal that has hung up
// (since next/s3-hangup; the maintainer, 2026-10-28: "Make hardn stop
// writing to its terminal once a hangup is caught"). Under sudo-rs
// 0.2.13, once the SSH session drops, sudo-rs keeps the pseudo-terminal
// it gave hardn open but stops reading it, so a write of hardn's to it
// blocks for good once its buffer is full, and with it the apply or the
// rollback that wrote it (docs/engine/cmd-engine.md §11). Where hardn
// catches the hangup rather than dying of it (the screens' and -g's
// apply, hardn confirm's clean-up, the engine's hold around a rollback),
// the catch calls Seen; the writers Writer returns, which hardn's stdout
// and stderr go through, then drop what they are given, and give up on a
// write still under way.
//
// It keeps to the standard library, as pkg/host does (depguard
// host-stdlib).
package hangup

import (
	"bytes"
	"io"
	"sync"
)

// A Terminal is a terminal that may hang up: its writers (Writer) write
// to it until its hangup is seen (Seen). The zero value is not usable;
// New returns one. hardn's own is the package's: Seen and Writer.
type Terminal struct {
	once sync.Once
	gone chan struct{} // closed by Seen
}

// New returns a Terminal whose hangup has not been seen.
func New() *Terminal { return &Terminal{gone: make(chan struct{})} }

// Seen records that the terminal has hung up: from then on its writers
// drop what they are given, and a write to it still under way is given
// up on. It may be called any number of times, from any goroutine; it
// cannot be undone.
func (t *Terminal) Seen() { t.once.Do(func() { close(t.gone) }) }

// Writer returns w as a writer to the terminal. Until the hangup is seen,
// each Write goes to w, one at a time, in the order they came, and
// returns what w's Write returned. After it, Write writes nothing and
// returns len(p) and no error, as if written: nothing reads what hardn
// writes there any more, and a report written to it does not fail the
// run. A Write under way when the hangup is seen returns so at once,
// leaving w's Write to finish, or block, on its own; so each Write to w
// is made on a goroutine of its own, with a copy of p. Writers over the
// same file each wait their turn alone: give each file one writer.
func (t *Terminal) Writer(w io.Writer) io.Writer {
	return &writer{t: t, w: w, turn: make(chan struct{}, 1)}
}

type writer struct {
	t    *Terminal
	w    io.Writer
	turn chan struct{} // held while a Write to w is under way
}

func (g *writer) Write(p []byte) (int, error) {
	select {
	case <-g.t.gone:
		return len(p), nil
	case g.turn <- struct{}{}:
	}
	select {
	case <-g.t.gone: // seen as the turn came
		<-g.turn
		return len(p), nil
	default:
	}
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	buf := bytes.Clone(p)
	go func() {
		n, err := g.w.Write(buf)
		<-g.turn
		done <- result{n, err}
	}()
	select {
	case r := <-done:
		return r.n, r.err
	case <-g.t.gone:
		return len(p), nil
	}
}

// std is hardn's terminal.
var std = New()

// Seen records that hardn's terminal has hung up (Terminal.Seen).
func Seen() { std.Seen() }

// Writer returns w as a writer to hardn's terminal (Terminal.Writer).
func Writer(w io.Writer) io.Writer { return std.Writer(w) }

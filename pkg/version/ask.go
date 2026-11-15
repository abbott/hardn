package version

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The question that turns the release check on
// (docs/engine/design-release-ask.md): what was answered, kept in hardn's
// state directory beside the check's cache, and when to ask again.
const (
	// AskEvery and AskOpens: after anything but a yes, the question is
	// due again once AskEvery has passed or the main menu has opened
	// AskOpens times, whichever comes first.
	AskEvery = 30 * 24 * time.Hour
	AskOpens = 15
	// AskName is the state file, in hardn's state directory
	// (revert.DefaultDir), and AskMax the largest one read.
	AskName = "release-ask.json"
	AskMax  = 4 << 10
)

// AskAnswer is what the question was answered.
type AskAnswer string

// The answers. AskNone is a question left unanswered: the end of the
// input, or an answer hardn could not save to hardn.yml.
const (
	AskYes  AskAnswer = "yes"
	AskNo   AskAnswer = "no"
	AskNone AskAnswer = "none"
)

// asked is release-ask.json: the last time the question was asked.
type asked struct {
	Answer AskAnswer `json:"answer"`
	// Asked is when, and Opens the main menu's opens since, this one
	// not counted.
	Asked time.Time `json:"asked"`
	Opens int       `json:"opens"`
}

// decodeAsked reads data strictly: one JSON object with no unknown field
// and nothing after it, an answer that is one of the three, a time that
// is set and a count not below zero. A time after now is kept: Due
// handles a clock that went backwards.
func decodeAsked(data []byte) (asked, error) {
	var s asked
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return asked{}, fmt.Errorf("not valid: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return asked{}, errors.New("not valid: more than one value")
	}
	switch {
	case s.Answer != AskYes && s.Answer != AskNo && s.Answer != AskNone:
		return asked{}, errors.New("not valid: its answer")
	case s.Asked.IsZero():
		return asked{}, errors.New("not valid: no time")
	case s.Opens < 0:
		return asked{}, errors.New("not valid: its count")
	}
	return s, nil
}

// Asker keeps the question's answer on one host. Only root reads or
// writes it: the state directory is root's alone.
type Asker struct {
	// System is the host the state file is on.
	System host.System
	// Dir is hardn's state directory; "" is revert.DefaultDir.
	Dir string
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

func (a *Asker) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Asker) opts() revert.Options { return revert.Options{Dir: a.Dir} }

// read is the state file, where it passes every check: the state
// directory's and the file's (revert.ReadStateFile), then its content.
func (a *Asker) read(ctx context.Context) (asked, error) {
	data, err := revert.ReadStateFile(ctx, a.System, a.opts(), AskName, AskMax)
	if err != nil {
		return asked{}, err
	}
	return decodeAsked(data)
}

func (a *Asker) write(ctx context.Context, s asked) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return revert.WriteStateFile(ctx, a.System, a.opts(), AskName, append(data, '\n'))
}

// Yes reports whether the question was answered yes. A state file that is
// missing, fails a check or cannot be read (a user other than root) is no.
// It writes nothing.
func (a *Asker) Yes(ctx context.Context) bool {
	s, err := a.read(ctx)
	return err == nil && s.Answer == AskYes
}

// Due counts one open of the main menu and reports whether the question is
// due: never asked (no state file, or one that fails a check), or, after
// anything but a yes, AskEvery passed or this the AskOpens-th open since.
// After a yes it never is.
//
// It writes the count either way. Due, what it writes is due at the next
// open too, so that a question ended by Ctrl-C is asked again, and where
// that can't be written (a state directory that fails a check, a
// read-only /var) the question is not due: hardn never asks where it
// can't keep the answer, or it would ask at every open.
//
// A time asked that is after now, the clock having gone backwards, counts
// from now: the question is neither due at once nor put off for ever, and
// the opens are counted either way.
func (a *Asker) Due(ctx context.Context) bool {
	now := a.now()
	s, err := a.read(ctx)
	if err != nil {
		s = asked{Answer: AskNone, Asked: now, Opens: AskOpens}
		return a.write(ctx, s) == nil
	}
	if s.Answer == AskYes {
		return false
	}
	if s.Asked.After(now) {
		s.Asked = now
	}
	s.Opens++
	if s.Opens >= AskOpens || now.Sub(s.Asked) >= AskEvery {
		s.Opens = max(s.Opens, AskOpens)
		return a.write(ctx, s) == nil
	}
	_ = a.write(ctx, s) // not written: the time asked still ends the wait
	return false
}

// Record keeps answer as asked now, with no open since.
func (a *Asker) Record(ctx context.Context, answer AskAnswer) error {
	return a.write(ctx, asked{Answer: answer, Asked: a.now(), Opens: 0})
}

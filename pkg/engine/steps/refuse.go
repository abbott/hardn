package steps

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/logview"
)

// What a control that sets a service's setting shares with the next one
// (docs/engine/gallery-timesync.md §5): the making of a refusal, and the
// few words of an error it says. The words and the codes are each
// control's.

// Refuse is a refusal: its code, its sentinel wrapping cause where there is
// one, and the Why and Fix a screen joins (screenkit.RefusalWords).
func Refuse(code string, sentinel, cause error, why, fix string) error {
	err := sentinel
	if cause != nil {
		err = fmt.Errorf("%w: %w", sentinel, cause)
	}
	return &Refusal{Code: code, Why: why, Fix: fix, Err: err}
}

// HostWords is a reading's error in one safe line (logview.Safe), without
// its package's own prefix ("logfwd: ") and its sentinel's words: what is
// left names files of the host.
func HostWords(err error, prefix string, sentinel error) string {
	words := strings.TrimPrefix(FirstLine(err.Error()), prefix)
	return logview.Safe(strings.TrimSuffix(words, ": "+sentinel.Error()))
}

// FirstLine is s to its first line break.
func FirstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

// IDs are the ids of cs.
func IDs(cs []engine.Change) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

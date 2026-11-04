package main

// The confirm window (refactor plan §3.14, approved 2026-10-17, wired by
// Phase 7's step 5 after the maintainer's "adopt all", 2026-11-04):
// --confirm-timeout and hardn.yml's confirmTimeout through
// engine.ConfirmTimeout, the engine's Options.Timeout (engineOptions);
// docs/engine/steps.md item 17.

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host/revert"
)

// confirmTimeoutFlag is --confirm-timeout as given, and whether it was
// (an empty value given is a bad one).
var (
	confirmTimeoutFlag  string
	confirmTimeoutGiven bool
)

// timeoutValue is --confirm-timeout's pflag.Value: the value as given,
// parsed by confirmTimeout once the configuration loads.
type timeoutValue struct{}

func (timeoutValue) String() string { return confirmTimeoutFlag }
func (timeoutValue) Set(s string) error {
	confirmTimeoutFlag, confirmTimeoutGiven = s, true
	return nil
}
func (timeoutValue) Type() string { return "duration" }

// addTimeoutFlag adds --confirm-timeout to flags, the root command's
// persistent flags. The words are its help (reported to p7-docs).
func addTimeoutFlag(flags *pflag.FlagSet) {
	flags.Var(timeoutValue{}, "confirm-timeout", "How long a lockout-risk change waits for sudo hardn confirm before it is rolled back, such as 300 or 5m (default 3m, at least 1m; hardn.yml's confirmTimeout)")
}

// confirmTimeout is the confirm window of a lockout-risk change, the
// engine's Options.Timeout: --confirm-timeout, else hardn.yml's
// confirmTimeout (c, nil for the flag alone), else 180 s, at least 60 s
// (engine.ConfirmTimeout). A value that is not one is the usage error
// confirmTimeoutError words.
func confirmTimeout(c *config.Config) (time.Duration, error) {
	var flag *time.Duration
	if confirmTimeoutGiven {
		d, err := revert.ParseTimeout(confirmTimeoutFlag)
		if err != nil {
			return 0, confirmTimeoutError{err}
		}
		flag = &d
	}
	d, err := engine.ConfirmTimeout(flag, c)
	if err != nil {
		return 0, confirmTimeoutError{err}
	}
	return d, nil
}

// confirmTimeoutError is a bad --confirm-timeout or confirmTimeout, said
// on stderr with no "engine: " or "revert: " prefix, its code the
// registry's (CONFIRM_TIMEOUT_INVALID, exit status 2).
type confirmTimeoutError struct{ err error }

func (e confirmTimeoutError) Error() string {
	s := e.err.Error()
	for _, p := range []string{"engine: ", "revert: "} {
		s = strings.ReplaceAll(s, p, "")
	}
	return fmt.Sprintf("hardn: %s (CONFIRM_TIMEOUT_INVALID)", s)
}

func (e confirmTimeoutError) Unwrap() error { return e.err }

// refuseConfirmTimeout refuses the command line, exit status 2, where the
// confirm window c gives (confirmTimeout) is not one; nil c checks the
// flag alone, before the configuration loads.
func refuseConfirmTimeout(c *config.Config) bool {
	_, err := confirmTimeout(c)
	return refuse(err)
}

package main

// The question that turns the release check on
// (docs/engine/design-release-ask.md), asked before the main menu and
// nowhere else: never for a flag or a subcommand, which never reach
// releaseAsk, and only on a terminal, as root (the root command has
// refused any other user by then). Under -n nothing is asked, counted or
// written, as the first-run question is not asked under -n; a yes hardn's
// state already holds still turns the check on there, a dry run changing
// nothing about the check (release.go). hardn.yml's dryRun: true is a
// lasting setting, not one run's: the question is asked there as in a
// live session, or that host would never be asked.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/version"
)

// checkReleasesKey is the release check's key in hardn.yml.
const checkReleasesKey = "checkReleases"

// Seams for the tests: whether stdin and stdout are terminals, the
// question itself, and the save of its answer to hardn.yml.
var (
	releaseAskTerminals = func() bool { return prompt.IsTerminal(os.Stdin) && prompt.IsTerminal(os.Stdout) }
	askCheckReleases    = config.AskCheckReleases
	saveCheckReleases   = func(ctx context.Context, c *config.Config) error {
		return config.SaveKeys(ctx, releaseHost(), c, checkReleasesKey)
	}
)

// releaseAsker is the question's state, on the release check's host and
// clock.
func releaseAsker() *version.Asker {
	return &version.Asker{System: releaseHost(), Now: releaseNow}
}

// releaseAskYes reports whether hardn's state holds a yes to the question:
// what turns the check on while no hardn.yml is found.
func releaseAskYes() bool { return releaseAsker().Yes(context.Background()) }

// releaseAskOut is where the question's lines are written: pkg/config's
// Stdout, the output frame's, as its prompts are.
func releaseAskOut() io.Writer {
	if config.Stdout != nil {
		return config.Stdout
	}
	return os.Stdout
}

// releaseAsk asks the question where it is due, before the main menu
// opens on c, and sets c.CheckReleases for this session where the answer,
// or one kept before, is yes.
//
// A hardn.yml that sets checkReleases decides alone: nothing is asked or
// written, except a false the question wrote there itself (hardn's state
// names that file: version.Asker.WroteFalse), asked about again when due
// (2026-11-16). That mark is dropped, so the false is the person's from
// then on, where hardn.yml says true or does not set the key, or the
// hardn.yml loaded is not the one named, or none is, or this run created
// it (created: the first-run questions asked for its false), and where
// the Configuration screen saves the key. With no hardn.yml, a yes in hardn's state turns the check on;
// otherwise the question is asked when due (version.Asker.Due) and the
// answer kept there. A hardn.yml without the key is asked about when due,
// at once the first time, and the answer, yes or no, is written to it in
// place (config.SaveKeys); a no is marked as the question's. Left
// unanswered, or the file not one hardn can edit, nothing is written to it
// and the question is due again by the same rule. Where hardn's state
// can't be written the question is never due (version.Asker.Due), with or
// without a hardn.yml.
func releaseAsk(ctx context.Context, c *config.Config, created bool) {
	path := c.Path()
	sets := path != "" && c.FileSets(checkReleasesKey)
	a := releaseAsker()
	asking := c.DryRunSource() != config.DryRunFlag && releaseAskTerminals()
	if !asking {
		if path == "" && a.Yes(ctx) {
			c.CheckReleases = true
		}
		return
	}
	marked := a.WroteFalse(ctx)
	if marked != "" && (created || marked != path || !sets || c.CheckReleases) {
		_ = a.Forget(ctx)
		marked = ""
	}
	if path == "" {
		if a.Yes(ctx) {
			c.CheckReleases = true
			return
		}
		if !a.Due(ctx) {
			return
		}
		yes, answered := askCheckReleases()
		answer := version.AskNone
		switch {
		case yes:
			answer = version.AskYes
		case answered:
			answer = version.AskNo
		}
		if err := a.Record(ctx, answer); err != nil {
			_, _ = fmt.Fprintf(releaseAskOut(), "Answer not kept: %v\n", err)
		}
		c.CheckReleases = yes
		return
	}
	switch {
	case sets && marked == "":
		return // set by hand, by the Configuration screen, or true
	case sets:
		if !a.Due(ctx) { // the question's own false
			return
		}
	case !a.Yes(ctx) && !a.Due(ctx): // a yes answered before there was a hardn.yml
		return
	}
	yes, answered := askCheckReleases()
	if !answered {
		_, _ = fmt.Fprintf(releaseAskOut(), "No answer: %s not changed\n", path)
		_ = a.RecordIn(ctx, version.AskNone, marked)
		return
	}
	was := c.CheckReleases
	c.CheckReleases = yes
	if err := saveCheckReleases(ctx, c); err != nil {
		c.CheckReleases = was
		_, _ = fmt.Fprintf(releaseAskOut(), "%s not changed: %s. Set %s there by hand\n", path, notSaved(err), checkReleasesKey)
		_ = a.RecordIn(ctx, version.AskNone, marked)
		return
	}
	_, _ = fmt.Fprintf(releaseAskOut(), "Saved %s: %t to %s\n", checkReleasesKey, yes, path)
	if yes {
		_ = a.Forget(ctx)
	} else {
		_ = a.RecordIn(ctx, version.AskNo, path) // asked again by the count
	}
}

// notSaved is why hardn.yml was not saved, in a few words: what about the
// file stops an edit in place (config.EditError), or the error.
func notSaved(err error) string {
	var e *config.EditError
	switch {
	case errors.As(err, &e):
		return "it " + e.Why
	case errors.Is(err, config.ErrChanged):
		return "it changed since it was read"
	}
	return err.Error()
}

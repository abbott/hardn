package main

// hardn check, the drift check (docs/engine/design-check.md, adopted
// 2026-11-13): `hardn check` compares the host with its saved baseline and
// `hardn check --accept` saves the host as the baseline. pkg/engine/drift
// reads and compares, pkg/output reports; this file wires them in. A plain
// check writes no file, takes no lock, opens no log and plans nothing;
// --accept writes the baseline and one record of the audit log.

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/abbott/hardn/pkg/engine/drift"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
)

func init() { rootCmd.AddCommand(checkCmd(runCheck)) }

// CheckForm is what hardn check's flags ask for: the comparison or, with
// Accept, the baseline saved; the report as text or, with JSON, as hardn's
// JSON document.
type CheckForm struct {
	Accept bool
	JSON   bool
}

// The command's refusals, in system-details' form (sdRefused).
const (
	checkNoDryRun = "hardn check has no dry run"
	checkUseJSON  = "use --output json"
)

// checkFlags are the command's flags as given.
type checkFlags struct {
	accept bool
	output string
}

// form is the command line as a CheckForm. Root's operation flags, which
// this command would not run, are refused, and so are -n, which previews a
// change and has nothing to preview here, and any --output but json: each
// the error, for exit status 2 (output.ExitUsage). given says which flags
// were on the command line.
func (f checkFlags) form(given func(name string) bool) (CheckForm, error) {
	if err := refuseRootOperations(given, "check"); err != nil {
		return CheckForm{}, err
	}
	switch {
	case given("dry-run"):
		return CheckForm{}, fmt.Errorf(sdRefused, "-n", checkNoDryRun)
	case given("output") && f.output != "json":
		return CheckForm{}, fmt.Errorf(sdRefused, "--output "+f.output, checkUseJSON)
	}
	return CheckForm{Accept: f.accept, JSON: given("output")}, nil
}

// checkCmd returns the check command, whose flags it reads and whose work
// is run's: run gets the form, or the refusal. It shows no revert notice
// (noRevertNotices): acknowledging one writes, and a check writes nothing.
func checkCmd(run func(CheckForm, error)) *cobra.Command {
	var f checkFlags
	cmd := &cobra.Command{
		Use:   "check [--accept]",
		Short: "Compare the host with its saved baseline",
		Long: `Read the host and compare it with the baseline saved earlier: the accounts that
can become root and their SSH keys, accounts with an empty password, sshd's
root login, password login, ports and allowed users, the firewall and its
rules, the listening ports the firewall lets in, the nameservers, automatic
updates, and the syslog and NTP servers. Each difference is one line: what it
was and what it is.

It changes nothing and makes no network request. It exits 0 with no difference,
3 with one, and 1 on an error or with no baseline, so it suits cron and CI.

With --accept, it saves the host as it is now as the baseline,
/var/lib/hardn/baseline.json. Run it once the host is as you want it, and
again after each change you mean to keep.

Examples (doas in place of sudo on a doas host):
  sudo hardn check --accept
  sudo hardn check
  sudo hardn check --output json`,
		Args:             cobra.NoArgs,
		PersistentPreRun: noRevertNotices,
		Run: func(cmd *cobra.Command, _ []string) {
			run(f.form(cmd.Flags().Changed))
		},
	}
	cmd.Flags().BoolVar(&f.accept, "accept", false, "Save the host as it is now as the baseline")
	cmd.Flags().StringVar(&f.output, "output", "", "Print the result in this format (json)")
	return cmd
}

// Seams for the tests: the host a check reads, with no run log, as every
// reader's (readerHost); the reading; and where the baseline is kept.
var (
	checkHost = func() host.System { return host.NewReal(host.Options{}) }
	checkRead = drift.Read
	checkOpts = revert.Options{}
)

// runCheck is hardn check: the form's refusal on stderr, exit status 2,
// nothing read; a user other than root refused, exit status 1, as hardn
// confirm's (the baseline and half the readers are root's: decision 9);
// then the run (checkRun), its report on stdout, as text or as the JSON
// document, an error said on stderr either way, and the exit status
// output.CheckExit gives: 0 no difference or the baseline saved, 3 a
// difference, 1 an error or no baseline, 130 interrupted. --accept's run is
// one record of the audit log, whatever its outcome; a plain check's is
// none (decision 7). Nothing opens hardn's log.
func runCheck(form CheckForm, refused error) {
	if refuse(refused) {
		return
	}
	if revertEUID() != 0 {
		sayNotRoot()
		exit(output.ExitError)
		return
	}
	if form.JSON {
		outFrame.disarm() // stdout holds one document (frame.go)
	}
	ctx, stop := confirmSignals(context.Background())
	sys := checkHost()
	run := checkRun(ctx, sys, form)
	stop()
	if run.Err != nil {
		sayError("%s", checkErrorWords(run))
	}
	var err error
	if form.JSON {
		err = output.WriteJSON(engineStdout, output.BuildCheck(run))
	} else {
		err = output.WriteCheck(engineStdout, stdoutEnv(), run)
	}
	if form.Accept {
		auditAppend(context.Background(), auditHost(sys), output.NewCheckRecord(run, revertNow()))
	}
	code := output.CheckExit(run)
	if err != nil {
		sayError("%v", err)
		code = max(code, output.ExitError)
	}
	if code != 0 {
		exit(code)
	}
}

// checkRun reads sys and compares it with the baseline or, for
// form.Accept, saves it as the baseline. It reads the baseline before the
// host, so that a host with none is told so at once.
func checkRun(ctx context.Context, sys host.System, form CheckForm) output.CheckRun {
	run := output.CheckRun{HardnVersion: Version, Hostname: auditHostname(ctx, sys), Accept: form.Accept,
		Path: drift.Path}
	if checkOpts.Dir != "" {
		run.Path = checkOpts.Dir + "/" + drift.BaselineName
	}
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		run.Err = err
		return run
	}
	run.Facts = &facts
	var base drift.Baseline
	if !form.Accept {
		if base, run.Err = drift.Load(ctx, sys, checkOpts); run.Err != nil {
			return run
		}
		run.Baseline = &base
	}
	now, err := checkRead(ctx, sys, facts)
	if err != nil {
		run.Err = err
		return run
	}
	if !form.Accept {
		run.Differences, run.Notes = drift.Compare(base.Facts, now)
		return run
	}
	b := drift.Baseline{Format: drift.Format, HardnVersion: Version, Saved: revertNow().UTC().Truncate(1e9),
		User: whoOf(revert.Session{}, nil).user, Facts: now}
	if run.Err = drift.Save(ctx, sys, checkOpts, b); run.Err != nil {
		return run
	}
	run.Baseline = &b
	_, run.Notes = drift.Compare(now, now)
	return run
}

// checkErrorWords is run's error for a person: the baseline's refusals in
// plain words, with what to run; any other error as it is, with its code.
func checkErrorWords(run output.CheckRun) string {
	accept := revertAsRoot("hardn check --accept")
	switch err := run.Err; {
	case errors.Is(err, drift.ErrNoBaseline):
		return fmt.Sprintf("No baseline: run `%s`", accept)
	case errors.Is(err, drift.ErrFormat):
		return fmt.Sprintf("The baseline is from another version of hardn, so it isn't compared. Run `%s` to save a new one.", accept)
	case errors.Is(err, drift.ErrUnsafe):
		return fmt.Sprintf("The baseline isn't root's alone, so it isn't read: %s", run.Path)
	case errors.Is(err, drift.ErrInvalid):
		return fmt.Sprintf("The baseline isn't one hardn wrote, so it isn't read: %s", run.Path)
	default:
		return fmt.Sprintf("%v (%s)", err, output.CodeOf(err))
	}
}

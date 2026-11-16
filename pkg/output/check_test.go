package output_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine/drift"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

func strp(s string) *string { return &s }

// checkFixtures are hardn check's runs (docs/reference/output.md §1.11):
// no difference; differences of each kind, a fact not established and one
// not compared among them; a baseline saved; no baseline; a baseline saved
// before the NTP servers were read, which says so and differs in nothing.
func checkFixtures() []struct {
	name string
	run  output.CheckRun
	exit int
} {
	facts := debian13()
	base := &drift.Baseline{Format: drift.Format, HardnVersion: fixtureVersion, Saved: time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC), User: "george"}
	run := output.CheckRun{HardnVersion: fixtureVersion, Hostname: fixtureHost, Facts: &facts, Path: drift.Path, Baseline: base}
	diffs := run
	diffs.Differences = []drift.Difference{
		{Fact: drift.Admins, Title: "Admin", Change: drift.Added, Was: strp(""), Is: strp("carol")},
		{Fact: drift.Keys, Title: "SSH key", Change: drift.Removed, Was: strp("george ssh-ed25519 SHA256:RXm/ruZ0eTzRXKwi1AQEDynB0VgHQ2ac9KPSFdf/YnA"), Is: strp("")},
		{Fact: drift.RootLogin, Title: "SSH root login", Change: drift.Changed, Was: strp("prohibit-password"), Is: nil},
		{Fact: drift.PasswordLogin, Title: "SSH password login", Change: drift.Changed, Was: strp("no"), Is: strp("yes")},
		{Fact: drift.AllowUsers, Title: "SSH allowed users", Change: drift.Changed, Was: strp("george"), Is: strp("")},
		{Fact: drift.Rules, Title: "Firewall rule", Change: drift.Added, Was: strp(""), Is: strp("allow in 8080/tcp")},
	}
	diffs.Notes = []drift.Note{{Fact: drift.Ports, Title: "Listening ports", Why: drift.NoSS}}
	one := run
	one.Differences = diffs.Differences[3:4]
	accepted := run
	accepted.Accept = true
	none := output.CheckRun{HardnVersion: fixtureVersion, Hostname: fixtureHost, Facts: &facts, Path: drift.Path,
		Err: fmt.Errorf("drift: %w", drift.ErrNoBaseline)}
	older := run
	older.Notes = []drift.Note{{Fact: drift.TimeServers, Title: "NTP servers", Why: drift.NotInBaseline}}
	return []struct {
		name string
		run  output.CheckRun
		exit int
	}{
		{"drift-same", run, output.ExitOK},
		{"drift-differences", diffs, output.ExitChanges},
		{"drift-one-difference", one, output.ExitChanges},
		{"drift-accepted", accepted, output.ExitOK},
		{"drift-no-baseline", none, output.ExitError},
		{"drift-older-baseline", older, output.ExitOK},
	}
}

// TestCheckDocuments pins hardn check's documents and exit statuses: each
// one JSON value, the nine fields and check, its arrays never null.
func TestCheckDocuments(t *testing.T) {
	for _, fx := range checkFixtures() {
		t.Run(fx.name, func(t *testing.T) {
			d := output.BuildCheck(fx.run)
			var b bytes.Buffer
			if err := output.WriteJSON(&b, d); err != nil {
				t.Fatal(err)
			}
			golden(t, "json/"+fx.name+".json", b.Bytes())
			if got := output.CheckExit(fx.run); got != fx.exit || d.Result.ExitCode != fx.exit {
				t.Errorf("exit %d, the document's %d, want %d", got, d.Result.ExitCode, fx.exit)
			}
			var m map[string]any
			if err := json.Unmarshal(b.Bytes(), &m); err != nil {
				t.Fatal(err)
			}
			check, _ := m["check"].(map[string]any)
			if len(m) != 10 || check == nil || check["differences"] == nil || m["changes"] == nil || m["status"] == nil || m["mode"] != "check" {
				t.Errorf("the document's fields: %v", m)
			}
		})
	}
}

// TestCheckOutcomes: the outcomes are the existing ones, by the run.
func TestCheckOutcomes(t *testing.T) {
	want := map[string]output.Outcome{"drift-same": output.OutcomeNothingToDo, "drift-differences": output.OutcomeChangesNeeded,
		"drift-one-difference": output.OutcomeChangesNeeded, "drift-accepted": output.OutcomeApplied, "drift-no-baseline": output.OutcomeFailed,
		"drift-older-baseline": output.OutcomeNothingToDo}
	for _, fx := range checkFixtures() {
		d := output.BuildCheck(fx.run)
		if d.Result.Outcome != want[fx.name] {
			t.Errorf("%s: %s", fx.name, d.Result.Outcome)
		}
	}
	for err, code := range map[error]string{drift.ErrNoBaseline: "BASELINE_MISSING", drift.ErrFormat: "BASELINE_OTHER_FORMAT", drift.ErrInvalid: "BASELINE_INVALID",
		fmt.Errorf("drift: %w: %w", drift.ErrUnsafe, errors.New("revert: /var/lib/hardn/baseline.json: unsafe")): "BASELINE_UNSAFE"} {
		d := output.BuildCheck(output.CheckRun{Err: err})
		if len(d.Errors) != 1 || d.Errors[0].Code != code || d.Result.Outcome != output.OutcomeFailed || d.Result.ExitCode != output.ExitError || d.Check.Baseline != nil {
			t.Errorf("%v: %+v, %+v", err, d.Errors, d.Result)
		}
	}
	d := output.BuildCheck(output.CheckRun{Err: fmt.Errorf("drift: %w", context.Canceled)})
	if d.Result.ExitCode != output.ExitInterrupted {
		t.Errorf("interrupted: %+v", d.Result)
	}
	for name, r := range map[string]output.CheckRun{"not saved": {Accept: true, Err: errors.New("drift: no space left")},
		"interrupted": {Accept: true, Err: fmt.Errorf("drift: %w", context.Canceled)}} {
		if d := output.BuildCheck(r); d.Check.Accepted || d.Check.Baseline != nil || d.Result.Outcome == output.OutcomeApplied {
			t.Errorf("--accept, %s: %+v, %+v", name, d.Check, d.Result)
		}
		if rec := output.NewCheckRecord(r, fixtureNow); rec.Check.Accepted {
			t.Errorf("--accept, %s: the audit record says accepted", name)
		}
	}
	rec := output.NewCheckRecord(checkFixtures()[3].run, fixtureNow)
	if !rec.Time.Equal(fixtureNow) || !rec.Check.Accepted || rec.Check.Baseline.User != "george" {
		t.Errorf("the audit record: %+v", rec)
	}
}

// TestHumanCheck pins hardn check's report for a person.
func TestHumanCheck(t *testing.T) {
	for _, fx := range checkFixtures() {
		t.Run(fx.name, func(t *testing.T) {
			uitest.Golden(t, "human/"+fx.name, uitest.Core(), func(env term.Env) string {
				return output.HumanCheck(env, fx.run)
			})
		})
	}
	if got := output.HumanCheck(plainEnv(), checkFixtures()[4].run); got != "" {
		t.Errorf("an error is the caller's to say: %q", got)
	}
}

// TestDifferenceLine: one line for each difference, with what it was and
// what it is; what the host holds cannot write to the terminal.
func TestDifferenceLine(t *testing.T) {
	for _, tc := range []struct {
		d    drift.Difference
		want string
	}{
		{drift.Difference{Title: "SSH root login", Change: drift.Changed, Was: strp("no"), Is: strp("yes")}, "SSH root login: was no, is yes"},
		{drift.Difference{Title: "SSH root login", Change: drift.Changed, Was: nil, Is: strp("yes")}, "SSH root login: was not established, is yes"},
		{drift.Difference{Title: "Nameservers", Change: drift.Changed, Was: strp("192.0.2.53"), Is: strp("")}, "Nameservers: was 192.0.2.53, is none"},
		{drift.Difference{Title: "Firewall rule", Change: drift.Added, Was: strp(""), Is: strp("allow in 8080/tcp")}, "Firewall rule added: allow in 8080/tcp"},
		{drift.Difference{Title: "Admin", Change: drift.Removed, Was: strp("carol"), Is: strp("")}, "Admin removed: carol"},
		{drift.Difference{Title: "Admin", Change: drift.Added, Was: strp(""), Is: strp("ca\x1b[2Jrol\nroot")}, "Admin added: ca^[[2Jrol^Jroot"},
	} {
		if got := output.DifferenceLine(tc.d); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
	run := checkFixtures()[2].run
	run.Differences = []drift.Difference{{Title: "Admin", Change: drift.Added, Was: strp(""), Is: strp("ev\x1b]0;x\x07il")}}
	if got := output.HumanCheck(plainEnv(), run); strings.Contains(got, "\x1b]") || strings.Contains(got, "\x07") {
		t.Errorf("an escape sequence reached the report: %q", got)
	}
}

// TestBaselineReminder: after a change applied on a host with a baseline,
// the report says to accept the host again, from Human and from AfterFlow;
// for a change waiting for confirmation, once it is confirmed. Never
// without a baseline, for a plan, or for a run that failed; and the
// document is the same either way.
func TestBaselineReminder(t *testing.T) {
	const keep, once = "Run `sudo hardn check --accept` to keep this as the baseline.", "Once confirmed, run `sudo hardn check --accept` to keep this as the baseline."
	fx := fixtures()
	awaiting, rolledBack, plan := fx[3].run, fx[4].run, fx[1].run
	applied := awaiting
	res := *awaiting.Result
	res.Confirm = nil
	applied.Result = &res
	if o := output.Build(applied).Result.Outcome; o != output.OutcomeApplied {
		t.Fatalf("the applied run is %s", o)
	}
	for _, tc := range []struct {
		name     string
		run      output.Run
		baseline bool
		want     string
	}{
		{"applied", applied, true, keep},
		{"applied, no baseline", applied, false, ""},
		{"awaiting confirmation", awaiting, true, once},
		{"awaiting confirmation, no baseline", awaiting, false, ""},
		{"rolled back", rolledBack, true, ""},
		{"a plan", plan, true, ""},
	} {
		tc.run.Baseline = tc.baseline
		for which, got := range map[string]string{"Human": output.Human(plainEnv(), tc.run, fixtureNow), "AfterFlow": output.AfterFlow(plainEnv(), tc.run, fixtureNow)} {
			has := strings.Contains(got, "check --accept")
			if has != (tc.want != "") || tc.want != "" && !strings.Contains(squash(got), squash(tc.want)) {
				t.Errorf("%s, %s: want %q in:\n%s", tc.name, which, tc.want, got)
			}
			if tc.want == keep && strings.Contains(got, "Once confirmed") {
				t.Errorf("%s, %s: the wrong words:\n%s", tc.name, which, got)
			}
		}
		with, without := tc.run, tc.run
		with.Baseline, without.Baseline = true, false
		var a, b bytes.Buffer
		_ = output.WriteJSON(&a, output.Build(with))
		_ = output.WriteJSON(&b, output.Build(without))
		if a.String() != b.String() {
			t.Errorf("%s: Baseline changes the document", tc.name)
		}
	}
	applied.Baseline = true
	uitest.Golden(t, "human/applied-baseline", uitest.Core(), func(env term.Env) string {
		return output.Human(env, applied, fixtureNow)
	})
}

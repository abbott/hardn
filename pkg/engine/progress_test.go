package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/revert"
)

// renderEvents renders the events Options.Progress received, one per line.
func renderEvents(evs []Event) string {
	var b strings.Builder
	for _, ev := range evs {
		fmt.Fprintf(&b, "%d %s %v", ev.Index, ev.Change, ev.State)
		if ev.Step != "" {
			b.WriteString(" at " + ev.Step)
		}
		if ev.Detail != "" {
			b.WriteString(": " + ev.Detail)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// TestProgress: Options.Progress hears of each change of the plan, by its
// index in Plan.Changes, as it runs and once it is done; of the failure;
// and, once the rollback in this process has finished, of each change done
// rolled back, last first (what pkg/ui/flow's Report needs).
func TestProgress(t *testing.T) {
	for _, c := range []struct {
		name    string
		failVer bool
		want    string
	}{
		{"applied", false, `0 example.port/allow running
0 example.port/allow done
1 example.port/write running
1 example.port/write done: written
2 example.port/reload running
2 example.port/reload done
3 example.port/verify running
3 example.port/verify done: 2230 effective
`},
		{"failed and rolled back", true, `0 example.port/allow running
0 example.port/allow done
1 example.port/write running
1 example.port/write done: written
2 example.port/reload running
2 example.port/reload done
3 example.port/verify running
3 example.port/verify failed at apply
2 example.port/reload rolled-back
1 example.port/write rolled-back
0 example.port/allow rolled-back
`},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			h := newHost(t)
			h.AddFile(confPath, []byte(oldConf), 0o644, 0, 0)
			o := testOptions(h, &holds{}, nil, debian13)
			var evs []Event
			o.Progress = func(ev Event) { evs = append(evs, ev) }
			e := New(h, o)
			pp := &portPlan{at: map[string]revert.Journal{}, failVer: c.failVer}
			p, err := e.Plan(ctx, pp.controls(), nil)
			if err != nil || p.Err() != nil {
				t.Fatalf("Plan: %v, %v", err, p.Err())
			}
			if _, err := e.Apply(ctx, p); (err != nil) != c.failVer {
				t.Fatalf("Apply: %v", err)
			}
			if got := renderEvents(evs); got != c.want {
				t.Errorf("events:\n%s\nwant:\n%s", got, c.want)
			}
			if c.failVer && evs[7].Err == nil {
				t.Error("the failure's event carries no error")
			}
		})
	}
}

// TestStore: a unit that takes backups of its own (dns.Apply, alpine.Apply
// take a *backup.Run) takes them in the plan's backup run, Run.Store, beside
// the engine's: one run directory, no ErrRunExists, and only the engine's
// entries journalled.
func TestStore(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	h.AddFile(confPath, []byte(oldConf), 0o644, 0, 0)
	const resolv = "/etc/example/resolv.conf"
	h.AddFile(resolv, []byte("nameserver 192.0.2.53\n"), 0o644, 0, 0)
	hs := &holds{}
	e := New(h, testOptions(h, hs, nil, debian13))
	old := oldConf
	w := writeChange("example.port/write", confPath, &old, newConf, RiskPlain)
	var unit backup.Result
	dns := Change{ID: "example.dns/apply", Summary: "Set the resolvers", Risk: RiskPlain, Stage: StageFiles, After: []string{"example.port/write"},
		Apply: func(ctx context.Context, sys host.System, run Run) (Ran, error) {
			if run.Store == nil {
				return Ran{}, errors.New("fixture: no store")
			}
			var err error
			unit, err = run.Store.Backup(ctx, resolv, backup.RollbackRequired)
			if err != nil {
				return Ran{}, err
			}
			return Ran{}, errors.New("fixture: stop here, to see the journal")
		}}
	p, err := e.Plan(ctx, []Control{&stub{id: "example", title: "Example", changes: []Change{w, dns}}}, nil)
	if err != nil || p.Err() != nil {
		t.Fatalf("Plan: %v, %v", err, p.Err())
	}
	_, err = e.Apply(ctx, p)
	if !errors.Is(err, ErrRolledBack) || !strings.Contains(err.Error(), "stop here") {
		t.Fatalf("Apply: %v", err)
	}
	if unit.Entry.Seq != 2 {
		t.Errorf("the unit's backup %+v, want seq 2 of the plan's run", unit.Entry)
	}
	if hs.pending == nil || hs.pending.Backup == nil || hs.pending.Backup.Dir != backup.DefaultPath+"/"+p.ID || journalWords(*hs.pending) != "undo [] backup [1]" {
		t.Errorf("journal at the rollback %+v: want the engine's backup alone, in the plan's run", hs.pending)
	}
	if got, _ := h.content(t, confPath); got != oldConf {
		t.Errorf("%s: %q", confPath, got)
	}
}

// TestUndoneByItself: a change that fails saying that what it did is
// undone (Ran.Undone) is reported rolled back, first, and the ApplyError
// says so (next/s2-control; gallery-dns.md §8 item 3, control-dns.md §8
// D3): one that undid its own steps (dns.Apply's Result.Restored, its undo
// empty), whether or not the rollback here finished and whether or not
// the journal after it was written, and then it is not unjournalled; one
// whose own undo did not finish, its undo left to the journal
// (dns.ErrNotRestored), only once that rollback finished. One refused
// before it acted is not.
func TestUndoneByItself(t *testing.T) {
	undo := Undo{Steps: runSteps(argv("example-undo"))}
	const first = "0 example.port/allow running\n0 example.port/allow done\n1 example.dns/apply running\n1 example.dns/apply failed at "
	for _, tc := range []struct {
		name         string
		rollback     Undo   // the change's, journalled before it
		ran          Ran    // what its Apply returns, with an error
		fail         string // a command that fails: the earlier change's undo, or this one's
		journalAfter bool   // the journal after it is not written
		step         string
		undone       bool   // ApplyError.Undone
		rolledBack   bool   // ErrRolledBack
		events       string // after its failure
	}{
		{name: "undid itself", ran: Ran{Rollback: &Undo{}, Undone: true}, step: StepApply, undone: true, rolledBack: true,
			events: "1 example.dns/apply rolled-back\n0 example.port/allow rolled-back\n"},
		{name: "refused before it acted", ran: Ran{Rollback: &Undo{}}, step: StepApply, rolledBack: true,
			events: "0 example.port/allow rolled-back\n"},
		{name: "undid itself, the rollback not finished", ran: Ran{Rollback: &Undo{}, Undone: true}, fail: delete2230, step: StepApply, undone: true,
			events: "1 example.dns/apply rolled-back\n"},
		{name: "undid itself, the journal after it not written", ran: Ran{Rollback: &Undo{}, Undone: true}, journalAfter: true, step: StepJournalAfter,
			undone: true, rolledBack: true, events: "1 example.dns/apply rolled-back\n0 example.port/allow rolled-back\n"},
		{name: "its undo left to the journal, which finished", rollback: undo, ran: Ran{Undone: true}, step: StepApply, undone: true, rolledBack: true,
			events: "1 example.dns/apply rolled-back\n0 example.port/allow rolled-back\n"},
		{name: "its undo left to the journal, which did not finish", rollback: undo, ran: Ran{Undone: true}, fail: "example-undo", step: StepApply},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			h := newHost(t)
			if tc.fail != "" {
				h.failOn(tc.fail)
			}
			o := testOptions(h, &holds{}, nil, debian13)
			var evs []Event
			o.Progress = func(ev Event) { evs = append(evs, ev) }
			e := New(h, o)
			allow := command("example.port/allow", StageFirewall, RiskPlain, argv(allow2230), argv(delete2230))
			unit := Change{ID: "example.dns/apply", Summary: "Set the resolvers", Risk: RiskPlain, Stage: StageFiles, Rollback: tc.rollback,
				Apply: func(context.Context, host.System, Run) (Ran, error) {
					if tc.journalAfter {
						h.failNextJournal(1)
					}
					return tc.ran, errors.New("fixture: the reload had no effect")
				}}
			p, err := e.Plan(ctx, []Control{&stub{id: "example", title: "Example", changes: []Change{allow, unit}}}, nil)
			if err != nil || p.Err() != nil {
				t.Fatalf("Plan: %v, %v", err, p.Err())
			}
			_, err = e.Apply(ctx, p)
			var ae *ApplyError
			if !errors.As(err, &ae) || ae.Step != tc.step || ae.Undone != tc.undone || errors.Is(err, ErrRolledBack) != tc.rolledBack ||
				ae.Unjournalled != nil || (ae.RollbackErr != nil) != (tc.fail != "") || strings.Contains(err.Error(), "nothing of it is left") != tc.undone {
				t.Fatalf("Apply: %v (Undone %v)", err, ae != nil && ae.Undone)
			}
			if got, want := renderEvents(evs), first+tc.step+"\n"+tc.events; got != want {
				t.Errorf("events:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// TestWireWords: the words State, RiskClass, Stage and EventState write,
// which nothing persisted may see change, and a plan's changes marshal to
// JSON (Apply aside).
func TestWireWords(t *testing.T) {
	type texter interface{ MarshalText() ([]byte, error) }
	for _, c := range []struct {
		v    texter
		want string
	}{
		{Unknown, "unknown"}, {Configured, "configured"}, {NotConfigured, "not-configured"}, {Partial, "partial"}, {NotApplicable, "not-applicable"},
		{RiskPlain, "plain"}, {RiskPackageInstall, "package-install"}, {RiskServiceRestart, "service-restart"}, {RiskLockout, "lockout"},
		{StageBoot, "boot"}, {StagePackages, "packages"}, {StageAccounts, "accounts"}, {StageFirewall, "firewall"}, {StageFiles, "files"}, {StageReload, "reload"}, {StageVerify, "verify"},
	} {
		if got, err := c.v.MarshalText(); err != nil || string(got) != c.want || fmt.Sprint(c.v) != c.want {
			t.Errorf("%#v: %q, %v, %v; want %q", c.v, got, err, c.v, c.want)
		}
	}
	for _, v := range []texter{State(9), RiskClass(9), Stage(0), Stage(9)} {
		if _, err := v.MarshalText(); err == nil {
			t.Errorf("%#v: no error", v)
		}
	}
	if got := fmt.Sprint(EventRunning, EventDone, EventFailed, EventRolledBack, EventState(0)); got != "running done failed rolled-back event(0)" {
		t.Errorf("event states %q", got)
	}
	data, err := json.Marshal([]Change{command("example.x/run", StageFiles, RiskLockout, argv("example-tool"), nil)})
	if err != nil || !strings.Contains(string(data), `"Risk":"lockout","Stage":"files"`) || strings.Contains(string(data), `"Apply"`) {
		t.Errorf("changes as JSON: %s, %v", data, err)
	}
}

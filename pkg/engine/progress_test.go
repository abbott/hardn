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

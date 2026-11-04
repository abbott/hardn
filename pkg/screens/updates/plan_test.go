package updates_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	updatesctl "github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/host"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	updscreen "github.com/abbott/hardn/pkg/screens/updates"
)

// hardnYML is the loaded hardn.yml's path.
const hardnYML = "/etc/hardn/hardn.yml"

// TestSaveAdopted: on a loaded hardn.yml saying enableUnattendedUpgrades
// true, Disable saves it false, its review naming the save; then Enable,
// then Disable again, each applying, the file and the configuration in
// memory agreeing after each (screenkit.Adopt), so that no save is refused
// as CONFIG_CHANGED.
func TestSaveAdopted(t *testing.T) {
	ctx := context.Background()
	m := newHost(t, "by-hardn-debian")
	m.f.AddFile(hardnYML, []byte("# hardn's configuration (web-01.example.net)\nusername: alice\nenableUnattendedUpgrades: true\n"), 0o600, 0, 0)
	cfg, err := config.Read(ctx, m.f, hardnYML)
	if err != nil {
		t.Fatal(err)
	}
	agree := func(after string) {
		t.Helper()
		data, err := m.f.ReadFile(ctx, hardnYML)
		if err != nil || !cfg.LoadedFrom(data) || cfg.EnableUnattendedUpgrades || !strings.Contains(string(data), "enableUnattendedUpgrades: false") {
			t.Errorf("after %s: the file %q (%v), in memory %v, loaded from it %v", after, data, err, cfg.EnableUnattendedUpgrades, cfg.LoadedFrom(data))
		}
	}
	engines := 0
	p := playWith(m, false, func(o *updscreen.Options) {
		o.Config = cfg
		o.NewEngine = func(progress func(engine.Event)) *engine.Engine {
			engines++
			switch engines { // the plan before asking of the second and third change
			case 3:
				agree("Disable")
			case 5:
				agree("Enable")
			}
			return m.engine(progress)
		}
	}, "1", "y", "1", "y", "1", "y", "0")
	if p.err != nil || engines != 6 || len(p.runs) != 3 || p.unread != 0 {
		t.Fatalf("Run = %v, %d engines, %d runs:\n%s", p.err, engines, len(p.runs), p.out)
	}
	for i, r := range p.runs {
		if r.Err != nil {
			t.Errorf("change %d: %v", i+1, r.Err)
		}
	}
	agree("Disable again")
	first, _, _ := strings.Cut(p.out, askOff)
	if !shows(first, "Save the configuration") {
		t.Errorf("the first Disable's review names no save:\n%s", first)
	}
	if _, third, _ := strings.Cut(p.out[strings.LastIndex(p.out, "1) "+disable):], askOff); shows(third, "Save the configuration") {
		t.Error("the second Disable saves again: the file already says false")
	}
}

// TestPreplanCancelled: the context cancelled while the item is planned
// before asking: Run returns the context's error, and nothing is asked.
func TestPreplanCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newHost(t, "debian-off")
	p := playIn(ctx, m, false, func(o *updscreen.Options) {
		o.NewEngine = func(progress func(engine.Event)) *engine.Engine {
			cancel()
			return m.engine(progress)
		}
	}, "1", "y", "0")
	if !errors.Is(p.err, context.Canceled) || asked(p.out) {
		t.Errorf("Run = %v, asked %v:\n%s", p.err, asked(p.out), p.out)
	}
}

// TestPreplanEngineError: the engine's Plan failing with an error that is
// no cancellation (its detect failing) says its words, and asks nothing.
func TestPreplanEngineError(t *testing.T) {
	m := newHost(t, "debian-off")
	p := playWith(m, false, func(o *updscreen.Options) {
		o.NewEngine = func(progress func(engine.Event)) *engine.Engine {
			return engine.New(m.f, engine.Options{
				Revert: revert.Options{Now: clock, Hardn: "/usr/local/bin/hardn"},
				Detect: func(context.Context, host.System) (detect.Facts, error) {
					return detect.Facts{}, errors.New("synthetic detect failure")
				},
				Hold: func() func() { return func() {} }, PID: 5000,
				Backups: hostbackup.Settings{Path: hostbackup.DefaultPath, Enabled: true}, Progress: progress,
			})
		}
	}, "1", "0")
	if p.err != nil || asked(p.out) || !shows(p.out, "engine: detect: synthetic detect failure.") {
		t.Errorf("Run = %v, asked %v:\n%s", p.err, asked(p.out), p.out)
	}
}

// TestRecordCarries: after an Enable applies, the control's record holds
// Options.Version and the time of Options.Records' clock.
func TestRecordCarries(t *testing.T) {
	const v = "v9.8.7-synthetic"
	at := time.Date(2026, 12, 1, 8, 15, 0, 0, time.UTC)
	m := newHost(t, "debian-installer")
	p := playWith(m, false, func(o *updscreen.Options) {
		o.Version, o.Records = v, revert.Options{Now: func() time.Time { return at }}
	}, "1", "y", "0")
	if p.err != nil || len(p.runs) != 1 || p.runs[0].Err != nil {
		t.Fatalf("Run = %v, runs %+v:\n%s", p.err, p.runs, p.out)
	}
	rec, err := revert.ReadRecord(context.Background(), m.f, revert.Options{}, updatesctl.ID)
	if err != nil || rec == nil || len(rec.Entries) != 1 {
		t.Fatalf("record %+v, %v", rec, err)
	}
	if e := rec.Entries[0]; e.Hardn != v || !e.Time.Equal(at) {
		t.Errorf("record entry: version %q at %v, want %q at %v", e.Hardn, e.Time, v, at)
	}
}

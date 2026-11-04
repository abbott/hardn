package runall_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	planner "github.com/abbott/hardn/pkg/engine/runall"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	runallscreen "github.com/abbott/hardn/pkg/screens/runall"
)

// drawn is what out showed before the first choice was read.
func drawn(out string) string { return out[:strings.Index(out, "Enter your choice")] }

// TestReadAtTheChoice: Apply all acts on the host as it is when 1 is read,
// not as it was drawn. A change journalled since says the planner's
// pending words, asking nothing and running nothing; Auto Updates refusing
// since (apt's files broken) is said skipped before the question.
func TestReadAtTheChoice(t *testing.T) {
	h := newHost(t, "debian13")
	p := playAt(context.Background(), h, false, func() { confirmWindow(t, h) }, nil, "1", "0")
	if !shows(drawn(p.out), itemPlanned) || p.err != nil || p.unread != 0 || !shows(p.out, planner.PendingWords) || asked(p.out) || len(p.runs) > 0 {
		t.Errorf("journalled after the draw: Run = %v, %d unread, runs %+v:\n%s", p.err, p.unread, p.runs, p.out)
	}
	h = newHost(t, "debian13")
	p = playAt(context.Background(), h, false, func() { h.mu.Lock(); h.aptBroken = true; h.mu.Unlock() }, nil, "1", "0", "0")
	if shows(drawn(p.out), "Auto Updates Refused:") || p.err != nil || p.unread != 0 ||
		at(p.out, "Auto Updates skipped:") < 0 || at(p.out, "Auto Updates skipped:") > at(p.out, planner.Question) {
		t.Errorf("refusing after the draw: Run = %v, %d unread:\n%s", p.err, p.unread, p.out)
	}
}

// TestVersion: the record Auto Updates writes on fresh carries the
// screen's Version, as the planner was given it.
func TestVersion(t *testing.T) {
	const version = "v9.8.7-synthetic"
	h := newHost(t, "fresh")
	p := playWith(h, false, func(o *runallscreen.Options) { o.Version = version }, "1", hostName, "0")
	if p.err != nil || len(p.runs) != 1 || p.runs[0].Err != nil {
		t.Fatalf("Run = %v, runs %+v:\n%s", p.err, p.runs, p.out)
	}
	rec, err := revert.ReadRecord(context.Background(), h, revert.Options{Now: clock, Hardn: hardnBinary}, updates.ID)
	if err != nil || rec == nil || len(rec.Entries) == 0 || rec.Entries[len(rec.Entries)-1].Hardn != version {
		t.Errorf("Auto Updates' record %+v, %v; want hardn %s", rec, err, version)
	}
}

// TestRevertDir: the planner reads the change journal where the screen's
// Revert options keep it: a change pending there draws Change pending.
func TestRevertDir(t *testing.T) {
	const dir = "/var/lib/hardn-synthetic"
	h := newHost(t, "debian13")
	opts := revert.Options{Dir: dir, Now: clock, Hardn: hardnBinary}
	e := engine.New(h, engine.Options{
		Revert: opts, Detect: func(context.Context, host.System) (detect.Facts, error) { return h.facts(), nil },
		Hold: func() func() { return func() {} }, PID: 5000, Session: h.sess,
		Backups: backup.Settings{Path: h.cfg.BackupPath, Enabled: h.cfg.EnableBackups},
	})
	ctx := context.Background()
	plan, err := e.Plan(ctx, []engine.Control{sshchange.Port{To: 2230}}, &config.Config{})
	if err != nil || len(plan.Refused) > 0 {
		t.Fatalf("plan: %v, refused %v", err, plan.Refused)
	}
	if _, err := e.Apply(ctx, plan); err != nil {
		t.Fatalf("apply: %v", err)
	}
	p := playWith(h, false, func(o *runallscreen.Options) { o.Revert = opts }, "0")
	if p.err != nil || !shows(p.out, itemPending) {
		t.Errorf("a change pending in %s: Run = %v; want %q:\n%s", dir, p.err, itemPending, p.out)
	}
}

// skippedHost is debian13 in an LXC container, ufw not installed, root
// login already off, hardn.yml asking george (there, the admin), root login
// off and the nameservers (Not Applicable in a container): nothing is
// planned, and the firewall refuses.
func skippedHost(t *testing.T) *runHost {
	t.Helper()
	pl := debian13()
	pl.noUFW = true
	h := vm(pl).sshFile(sshd.HardnDropIn, "PermitRootLogin no\n", 0o644).withLog("publickey", "george", 1)
	h.plat.facts.Container = detect.Container{Detected: detect.Yes, Signal: "/proc/1/environ", Manager: "lxc"}
	return withYML(t, h, askYML("george", 1, 22, false))
}

// TestSkipped: where nothing is planned and rows are refused, the item
// reads "Nothing to change" and is available: choosing it says each
// "<Row> skipped: <refusal>" and asks nothing, writing nothing.
func TestSkipped(t *testing.T) {
	r := skippedHost(t).read(t, "skipped")
	if len(r.Planned()) > 0 || len(r.Refused()) == 0 {
		t.Fatalf("planned %v, refused %v; want nothing planned and a refusal", r.Planned(), r.Refused())
	}
	h := skippedHost(t)
	n := len(h.Calls())
	p := play(h, false, "1", "0")
	if p.err != nil || p.unread != 0 || !shows(drawn(p.out), itemNothing) || asked(p.out) {
		t.Errorf("Run = %v, %d unread:\n%s", p.err, p.unread, p.out)
	}
	for _, rw := range r.Refused() {
		if !shows(p.out, rw.Title+" skipped: "+rw.Refusal) {
			t.Errorf("%s's refusal not said:\n%s", rw.ID, p.out)
		}
	}
	if w := writes(h, n); len(w) > 0 {
		t.Errorf("wrote %v", w)
	}
	checkHost(t, "skipped", h.sshHost)
}

// TestCancelledAtTheChoice: the screen's context cancelled once it is
// drawn, Apply all's reading fails on it, and Run returns the context's
// error.
func TestCancelledAtTheChoice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := playAt(ctx, newHost(t, "debian13"), false, cancel, nil, "1", "0")
	if !errors.Is(p.err, context.Canceled) {
		t.Errorf("Run = %v, want context.Canceled:\n%s", p.err, p.out)
	}
}

// TestFailedApplyLog: an apply that fails names the log, LogPath.
func TestFailedApplyLog(t *testing.T) {
	const log = "/var/log/hardn-synthetic.log"
	p := playWith(newHost(t, "debian13"), false, func(o *runallscreen.Options) {
		o.LogPath = log
		o.ApplyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
			c, cancel := context.WithCancel(ctx)
			cancel()
			return c, cancel
		}
	}, "1", hostName, "0")
	if p.err != nil || len(p.runs) != 1 || p.runs[0].Err == nil || !shows(p.out, log) {
		t.Errorf("Run = %v, runs %+v; want the failure naming %s:\n%s", p.err, p.runs, log, p.out)
	}
}

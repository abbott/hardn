package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// checkOnly is a check-only control: CheckOnly gives it its Plan.
type checkOnly struct{ CheckOnly }

func (checkOnly) ID() string                          { return "example.check-only" }
func (checkOnly) Title() string                       { return "Check Only" }
func (checkOnly) Applies(detect.Facts) (bool, string) { return true, "" }
func (checkOnly) Check(context.Context, host.System, detect.Facts) (Status, error) {
	return Status{State: Configured, Detail: "as wanted"}, nil
}

// TestCheckOnly: CheckOnly's Plan is ErrNotMigrated with no change;
// Engine.Check reports the control's own Check; and Engine.Plan refuses a
// plan with it, beside a control that plans, before anything changes
// (next/p4-check-ssh, docs/engine/checks.md §1.1).
func TestCheckOnly(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	if cs, err := (checkOnly{}).Plan(ctx, h, Input{Facts: debian13}); cs != nil || !errors.Is(err, ErrNotMigrated) {
		t.Errorf("Plan: %v, %v; want no change and ErrNotMigrated", cs, err)
	}
	e := New(h, testOptions(h, nil, nil, debian13))
	reps, err := e.Check(ctx, []Control{checkOnly{}})
	if err != nil || len(reps) != 1 || reps[0].Status.State != Configured || reps[0].Err != nil {
		t.Fatalf("Check: %+v, %v", reps, err)
	}
	other := command("ssh.port/allow", StageFirewall, RiskPlain, []string{"ufw", "allow", "2230/tcp"}, []string{"ufw", "delete", "allow", "2230/tcp"})
	p, err := e.Plan(ctx, []Control{checkOnly{}, &stub{id: "ssh.port", title: "SSH Port", changes: []Change{other}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Err(); !errors.Is(err, ErrRefused) || !errors.Is(err, ErrNotMigrated) {
		t.Errorf("Plan.Err: %v; want ErrRefused and ErrNotMigrated", err)
	}
	if _, err := e.Apply(ctx, p); !errors.Is(err, ErrNotMigrated) {
		t.Errorf("Apply: %v, want ErrNotMigrated", err)
	}
	if got := h.changedOutside(runRoot); len(got) > 0 {
		t.Errorf("changed outside %s: %q", runRoot, got)
	}
}

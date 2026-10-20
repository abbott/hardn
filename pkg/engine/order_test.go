package engine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// ch is a change for the order alone.
func ch(id string, stage Stage, after ...string) Change {
	return Change{ID: id, Summary: id, Stage: stage, After: after}
}

func ids(cs []Change) string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return strings.Join(out, " ")
}

// TestOrder: brief 2.3 as stages, then prerequisites within a stage, then
// the plan's order; the same input always gives the same order.
func TestOrder(t *testing.T) {
	cases := []struct {
		name string
		in   []Change
		want string
		err  error
	}{
		{"the stages in brief 2.3's order, whatever the plan's",
			[]Change{ch("verify", StageVerify), ch("reload", StageReload), ch("write", StageFiles), ch("fw", StageFirewall), ch("key", StageAccounts), ch("pkg", StagePackages)},
			"pkg key fw write reload verify", nil},
		{"the plan's order within a stage",
			[]Change{ch("b", StageFiles), ch("a", StageFiles), ch("c", StageFiles)},
			"b a c", nil},
		{"a prerequisite in the stage goes first; the others keep their order",
			[]Change{ch("b", StageFiles, "d"), ch("a", StageFiles), ch("c", StageFiles), ch("d", StageFiles, "c")},
			"a c d b", nil},
		{"a prerequisite in an earlier stage is met by the stages",
			[]Change{ch("write", StageFiles, "fw"), ch("fw", StageFirewall)},
			"fw write", nil},
		{"a prerequisite in a later stage", []Change{ch("fw", StageFirewall, "write"), ch("write", StageFiles)}, "", ErrOrder},
		{"a prerequisite no control planned", []Change{ch("write", StageFiles, "gone")}, "", ErrOrder},
		{"two changes that need each other", []Change{ch("a", StageFiles, "b"), ch("b", StageFiles, "a")}, "", ErrOrder},
		{"a cycle of three, beside a change that is fine",
			[]Change{ch("ok", StageFiles), ch("a", StageFiles, "c"), ch("b", StageFiles, "a"), ch("c", StageFiles, "b")}, "", ErrOrder},
		{"an id planned twice", []Change{ch("a", StageFiles), ch("a", StageReload)}, "", ErrChange},
		{"nothing", nil, "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := order(c.in)
			if !errors.Is(err, c.err) || (c.err != nil) != (err != nil) {
				t.Fatalf("order: %v, want %v", err, c.err)
			}
			if ids(got) != c.want {
				t.Errorf("order %q, want %q", ids(got), c.want)
			}
			again, _ := order(slices.Clone(c.in))
			if ids(again) != ids(got) {
				t.Errorf("not stable: %q then %q", ids(got), ids(again))
			}
		})
	}
}

// TestPlanOrder: the plan shows the order Apply runs, across controls; a
// cycle is a refusal at plan time, and Apply refuses it with nothing
// changed.
func TestPlanOrder(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	e := New(h, testOptions(h, nil, nil, debian13))
	noop := func(id string, stage Stage, after ...string) Change {
		c := command(id, stage, RiskLockout, []string{"example-tool", id}, nil)
		c.After = after
		return c
	}
	sshd := &stub{id: "ssh.port", title: "SSH Port", changes: []Change{
		noop("ssh.port/verify", StageVerify), noop("ssh.port/reload", StageReload),
		noop("ssh.port/write", StageFiles, "example.ufw/allow"),
	}}
	fw := &stub{id: "example.ufw", title: "Firewall", changes: []Change{noop("example.ufw/allow", StageFirewall)}}
	p, err := e.Plan(ctx, []Control{sshd, fw}, nil)
	if err != nil || p.Err() != nil {
		t.Fatalf("Plan: %v, %v", err, p.Err())
	}
	if got, want := ids(p.Changes), "example.ufw/allow ssh.port/write ssh.port/reload ssh.port/verify"; got != want {
		t.Errorf("plan order %q, want %q", got, want)
	}
	if p.Changes[1].Control != "ssh.port" || p.Items[1].Changes[0] != "example.ufw/allow" {
		t.Errorf("items %+v", p.Items)
	}

	cyclic := &stub{id: "ssh.port", title: "SSH Port", changes: []Change{
		noop("ssh.port/a", StageFiles, "ssh.port/b"), noop("ssh.port/b", StageFiles, "ssh.port/a"),
	}}
	p, err = e.Plan(ctx, []Control{cyclic}, nil)
	if err != nil || !errors.Is(p.Err(), ErrRefused) || !errors.Is(p.Err(), ErrOrder) {
		t.Fatalf("cycle: %v, %v; want ErrRefused with ErrOrder", err, p.Err())
	}
	before := len(h.Calls())
	if _, err := e.Apply(ctx, p); !errors.Is(err, ErrOrder) {
		t.Errorf("Apply of a cyclic plan: %v, want ErrOrder", err)
	}
	if n := len(h.Calls()); n != before {
		t.Errorf("Apply of a refused plan made %d calls", n-before)
	}
}

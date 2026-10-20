package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestConflicts: two changes of one plan that back up one file, and two
// controls whose changes name one resource, are refused at plan time,
// before anything changes (next/p3-steps): each was planned from the host
// as it is, not as the other leaves it. One control's changes may share a
// resource.
func TestConflicts(t *testing.T) {
	ctx := context.Background()
	write := func(id string) Change { return writeChange(id, confPath, nil, "example\n", RiskPlain) }
	uses := func(c Change, rs ...string) Change { c.Resources = rs; return c }
	allow := func(id string) Change {
		return command(id, StageFirewall, RiskPlain, []string{"ufw", "allow", "2230/tcp"}, []string{"ufw", "delete", "allow", "2230/tcp"})
	}
	ctl := func(id string, cs ...Change) Control { return &stub{id: id, title: "Example " + id, changes: cs} }
	for _, tc := range []struct {
		name     string
		controls []Control
		want     error  // nil: not refused
		words    string // in the refusal
		item     string // the control whose Item carries it
	}{
		{"two controls back up one file", []Control{ctl("a", write("a/write")), ctl("b", write("b/write"))},
			ErrConflict, `changes "a/write" and "b/write" both back up ` + confPath, "b"},
		{"one control's two changes back up one file", []Control{ctl("a", write("a/one"), write("a/two"))},
			ErrConflict, `changes "a/one" and "a/two" both back up`, "a"},
		{"two controls name one resource", []Control{ctl("a", uses(allow("a/allow"), "ufw")), ctl("b", uses(write("b/write"), "sshd", "ufw"))},
			ErrConflict, `controls "a" and "b" both plan changes of ufw`, "b"},
		{"two controls name two resources: refused once", []Control{ctl("a", uses(allow("a/allow"), "sshd", "ufw")),
			ctl("b", uses(write("b/write"), "sshd", "ufw"), uses(allow("b/allow"), "sshd", "ufw"))},
			ErrConflict, `controls "a" and "b" both plan changes of sshd and ufw`, "b"},
		{"a resource that is not a word", []Control{ctl("a", uses(allow("a/allow"), "the firewall"))}, ErrChange, `resource "the firewall"`, "a"},
		{"a resource twice", []Control{ctl("a", uses(allow("a/allow"), "ufw", "ufw"))}, ErrChange, `resource "ufw"`, "a"},
		{"one control's changes share resources", []Control{ctl("a", uses(allow("a/allow"), "sshd", "ufw"), uses(write("a/write"), "sshd", "ufw"))}, nil, "", ""},
		{"two controls, other resources", []Control{ctl("a", uses(allow("a/allow"), "ufw")), ctl("b", uses(write("b/write"), "dns"))}, nil, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			p, err := New(h, testOptions(h, nil, nil, debian13)).Plan(ctx, tc.controls, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = p.Err()
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Plan.Err = %v, want none", err)
				}
				return
			}
			if !errors.Is(err, ErrRefused) || !errors.Is(err, tc.want) || !strings.Contains(err.Error(), tc.words) {
				t.Fatalf("Plan.Err = %v; want %v with %q", err, tc.want, tc.words)
			}
			if n := strings.Count(err.Error(), "both plan changes of"); n > 1 {
				t.Errorf("one pair of controls refused %d times: %v", n, err)
			}
			for _, it := range p.Items {
				if got := it.Err != nil && errors.Is(it.Err, tc.want); got != (it.Control == tc.item) {
					t.Errorf("item %s: err %v; want the refusal on %s's", it.Control, it.Err, tc.item)
				}
			}
			if got := h.changedOutside(runRoot); len(got) > 0 {
				t.Errorf("changed outside %s: %q", runRoot, got)
			}
		})
	}
}

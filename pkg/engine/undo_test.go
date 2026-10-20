package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
)

// TestJournalUndo: the journal's undo is every started change's, the last
// change's first, and an ssh reload two changes journal (the write before
// it, the reload change that runs it) is one entry, IfUp only while both
// say IfUp (next/p3-steps).
func TestJournalUndo(t *testing.T) {
	ssh := func(ifUp bool, kws ...string) revert.Reload {
		return revert.Reload{Service: revert.ReloadSSH, Keywords: kws, After: []revert.Listener{any2230}, IfUp: ifUp}
	}
	step := func(argv ...string) revert.Step { return revert.Step{Op: revert.OpRun, Argv: argv, Why: "synthetic"} }
	for _, tc := range []struct {
		name    string
		undos   []Undo
		steps   string
		reloads string
	}{
		{"the write and its reload, before the reload ran",
			[]Undo{{Steps: []revert.Step{step("example-undo", "1"), step("example-undo", "2")}}, {Reloads: []revert.Reload{ssh(true, "Port")}}, {Reloads: []revert.Reload{ssh(true, "Port")}}},
			"[[example-undo 1] [example-undo 2]]", "[Port:true]"},
		{"once the reload ran", []Undo{{Reloads: []revert.Reload{ssh(true, "Port")}}, {Reloads: []revert.Reload{ssh(false, "Port")}}},
			"[]", "[Port:false]"},
		{"two reloads of other keywords", []Undo{{Reloads: []revert.Reload{ssh(true, "Port")}}, {Reloads: []revert.Reload{ssh(true, "PasswordAuthentication")}}},
			"[]", "[PasswordAuthentication:true Port:true]"},
		{"the steps, last change's first", []Undo{{Steps: []revert.Step{step("a")}}, {Steps: []revert.Step{step("b"), step("c")}}},
			"[[b] [c] [a]]", "[]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps, reloads := journalUndo(tc.undos)
			var argvs [][]string
			for _, s := range steps {
				argvs = append(argvs, s.Argv)
			}
			var rs []string
			for _, r := range reloads {
				rs = append(rs, fmt.Sprintf("%s:%v", r.Keywords[0], r.IfUp))
			}
			if got := fmt.Sprint(argvs); got != tc.steps {
				t.Errorf("steps %s, want %s", got, tc.steps)
			}
			if got := fmt.Sprint(rs); got != tc.reloads {
				t.Errorf("reloads %s, want %s", got, tc.reloads)
			}
		})
	}
	// The undos themselves are not changed.
	undos := []Undo{{Reloads: []revert.Reload{ssh(true, "Port")}}, {Reloads: []revert.Reload{ssh(false, "Port")}}}
	journalUndo(undos)
	if !undos[0].Reloads[0].IfUp {
		t.Error("journalUndo changed a change's undo")
	}
}

// sessionControl records the Input it plans from.
type sessionControl struct{ got *Input }

func (sessionControl) ID() string                          { return "example.session" }
func (sessionControl) Title() string                       { return "Example session" }
func (sessionControl) Applies(detect.Facts) (bool, string) { return true, "" }
func (sessionControl) Check(context.Context, host.System, detect.Facts) (Status, error) {
	return Status{}, nil
}
func (c sessionControl) Plan(_ context.Context, _ host.System, in Input) ([]Change, error) {
	*c.got = in
	return nil, nil
}

// TestInputSession: a control plans with the session the journal names
// (Options.Session), so a listener change can keep the port it is on.
func TestInputSession(t *testing.T) {
	h := newHost(t)
	var in Input
	e := New(h, testOptions(h, nil, nil, debian13))
	if _, err := e.Plan(context.Background(), []Control{sessionControl{&in}}, nil); err != nil {
		t.Fatal(err)
	}
	if in.Session != applySession || in.ID == "" || in.RunDir == "" {
		t.Errorf("Input %+v, want the session %+v", in, applySession)
	}
}

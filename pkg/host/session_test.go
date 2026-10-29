package host

import (
	"context"
	"testing"
)

// TestOwnSessions: Run and RunInput start their command in a session of
// its own (Setsid) while an OwnSessions is held, and as before (no
// SysProcAttr) otherwise; holders nest, and a release counts once.
// pkg/engine/hold's TestGroupHangupDuringHold runs it for real (a hangup
// sent to the process group during a hold leaves the command running),
// and TestReleaseGivesUpSessions the hold's release of it.
func TestOwnSessions(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "tool", 0o755)
	s := newSeamed(t, Options{}, []string{dir}, nil)
	run := func(want bool, when string) {
		t.Helper()
		s.cmds = nil
		if _, err := s.Run(context.Background(), "tool"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.RunInput(context.Background(), []byte("x"), "tool"); err != nil {
			t.Fatal(err)
		}
		for _, cmd := range s.cmds {
			got := cmd.SysProcAttr != nil && cmd.SysProcAttr.Setsid
			if got != want || !want && cmd.SysProcAttr != nil {
				t.Errorf("%s: %v: SysProcAttr = %+v, want a session of its own %t", when, cmd.Args, cmd.SysProcAttr, want)
			}
		}
	}
	run(false, "before")
	first := OwnSessions()
	run(true, "held")
	second := OwnSessions()
	first()
	first()
	run(true, "one of two released, twice")
	second()
	run(false, "both released")
}

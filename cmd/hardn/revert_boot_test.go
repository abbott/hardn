package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The boot after newRevertSeams's change was applied.
const revertNextBootID = "1c6f2e9a-7d3b-4e5f-9a10-fedcba987654"

// rebootSeams reboots s's host after after: a new boot id, the boot unit
// installed, ssh.service not up yet.
func rebootSeams(s *revertSeams, after time.Duration) {
	s.f.AddFile(revertBootIDFile, []byte(revertNextBootID+"\n"), 0o444, 0, 0)
	revertNow = func() time.Time { return revertTestNow.Add(after) }
	u, _ := revert.PlanBootUnit(revertInit(context.Background(), s.f), revertTestHardn)
	s.f.AddFile(u.Path, u.Data, 0o644, 0, 0)
	s.f.AddSymlink(u.Link, u.Path)
	s.f.Respond(hostfake.Response{}, u.Disable[0], u.Disable[1:]...)
}

// `hardn rollback --boot` (P-34 (c)): root only, refused under -n, no
// <id>, not with --abandon; re-arms with the time left (exit 0), leaves
// another binary's journal (exit 1), rolls back past the deadline (exit
// 0), and with nothing journalled removes the boot unit (exit 0).
func TestRevertBootCommand(t *testing.T) {
	pending := revert.DefaultDir + "/" + revert.PendingName
	for _, tt := range []struct {
		name string
		euid int
		args []string
		exit int
		says string
	}{
		{"as alice", 1000, []string{"rollback", "--boot"}, 1, "hardn rollback --boot must be run as root"},
		{"with -n", 0, []string{"-n", "rollback", "--boot"}, 2, "-n only previews, and hardn rollback --boot changes the host"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newRevertSeams(t, tt.euid)
			rebootSeams(s, time.Minute)
			calls := len(s.f.Calls())
			out := s.output(t, tt.args...)
			if s.exit != tt.exit || !strings.Contains(out, tt.says) || len(s.f.Calls()) != calls {
				t.Errorf("exit %d, want %d; %d calls; output:\n%s", s.exit, tt.exit, len(s.f.Calls())-calls, out)
			}
			dryRun = false
		})
	}
	for _, args := range [][]string{{"rollback", "--boot", revertTestID}, {"rollback", "--boot", "--abandon", revertTestID}} {
		s := newRevertSeams(t, 0)
		rootCmd.SetArgs(args)
		if err := rootCmd.Execute(); err == nil || !s.has(pending) {
			t.Errorf("%q: %v", args, err)
		}
		rollbackBoot, rollbackAbandon = false, false
	}

	t.Run("re-armed", func(t *testing.T) {
		s := newRevertSeams(t, 0)
		rebootSeams(s, time.Minute)
		s.f.Respond(hostfake.Response{}, "systemd-run", "--unit=hardn-revert-"+revertTestID, "--description=hardn: roll back change "+revertTestID+" unless confirmed",
			"--on-active=120s", "--timer-property=AccuracySec=1s", revertTestHardn, "rollback", revertTestID)
		out := s.output(t, "rollback", "--boot")
		if s.exit != -1 || !strings.Contains(out, "[SUCCESS] At boot, change "+revertTestID+" (SSH port 22 to 2230 on web-01.example.net): its timer is re-armed for what remains of its window, 2m0s") {
			t.Fatalf("exit %d:\n%s", s.exit, out)
		}
		if s.held != 1 || s.released != 1 {
			t.Errorf("signals held %d, released %d", s.held, s.released)
		}
		if !strings.Contains(out, "revert: boot: change "+revertTestID) {
			t.Errorf("the decision is not logged:\n%s", out)
		}
		// Run again in the same boot: its timer was started in this boot.
		if out := s.output(t, "rollback", "--boot"); s.exit != -1 || !strings.Contains(out, "nothing to do: its timer was started in this boot") {
			t.Errorf("again: exit %d:\n%s", s.exit, out)
		}
	})
	t.Run("another binary's journal", func(t *testing.T) {
		s := newRevertSeams(t, 0)
		rebootSeams(s, time.Minute)
		revertExecutable = func() (string, error) { return "/usr/sbin/hardn", nil }
		out := s.output(t, "rollback", "--boot")
		if s.exit != 1 || !strings.Contains(out, "[WARNING] At boot, change "+revertTestID) || !strings.Contains(out, "not this hardn (/usr/sbin/hardn)") || !s.has(pending) {
			t.Errorf("exit %d:\n%s", s.exit, out)
		}
	})
	t.Run("past the deadline", func(t *testing.T) {
		s := newRevertSeams(t, 0)
		rebootSeams(s, time.Hour)
		out := s.output(t, "rollback", "--boot")
		if s.exit != -1 || !strings.Contains(out, "has passed: rolled back") || s.has(pending) || !s.has(revert.DefaultDir+"/"+revert.RevertedName) {
			t.Errorf("exit %d:\n%s", s.exit, out)
		}
		if !strings.Contains(out, "No change is journalled, so hardn's boot unit is removed") {
			t.Errorf("the boot unit is not removed:\n%s", out)
		}
	})
	t.Run("nothing journalled", func(t *testing.T) {
		s := newRevertSeams(t, 0)
		executeHardn(t, "confirm")
		rebootSeams(s, time.Minute)
		out := s.output(t, "rollback", "--boot")
		if s.exit != -1 || !strings.Contains(out, "[INFO] At boot: nothing to do: no change is journalled; the last change confirmed is "+revertTestID) ||
			!strings.Contains(out, "hardn's boot unit is removed") || s.has("/etc/systemd/system/hardn-boot.service") {
			t.Errorf("exit %d:\n%s", s.exit, out)
		}
	})
}

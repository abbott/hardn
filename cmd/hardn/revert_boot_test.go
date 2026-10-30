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
// installed and running its start, ssh.service not up yet. It returns the
// boot unit.
func rebootSeams(s *revertSeams, after time.Duration) revert.BootUnit {
	s.f.AddFile(revertBootIDFile, []byte(revertNextBootID+"\n"), 0o444, 0, 0)
	revertNow = func() time.Time { return revertTestNow.Add(after) }
	u, _ := revert.PlanBootUnit(revertInit(context.Background(), s.f), revertTestHardn)
	s.f.AddFile(u.Path, u.Data, 0o644, 0, 0)
	s.f.AddSymlink(u.Link, u.Path)
	s.f.Respond(hostfake.Response{Stdout: "activating\n", ExitCode: 3}, "systemctl", "is-active", revert.BootUnitName+".service")
	s.f.Handler = func(argv []string, _ []byte) hostfake.Response {
		if strings.Join(argv, " ") == strings.Join(u.Disable, " ") {
			_ = s.f.Remove(context.Background(), u.Link) // as systemctl disable does
			return hostfake.Response{}
		}
		return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
	}
	return u
}

// `hardn rollback --boot` (P-34 (c)): root only, refused under -n, no
// <id>, not with --abandon, each running nothing; re-arms with the time
// left (exit 0), leaves another binary's journal (exit 1), rolls back past
// the deadline (exit 0), or not to the end (exit 1, the unit kept), and with
// nothing journalled removes the boot unit (exit 0), or warns that it
// could not (exit 0).
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
			wrote := false
			for _, c := range s.f.Calls()[calls:] {
				wrote = wrote || changed(c) // as alice it looks for doas
			}
			if s.exit != tt.exit || !strings.Contains(out, tt.says) || wrote {
				t.Errorf("exit %d, want %d; changed the host %v; output:\n%s", s.exit, tt.exit, wrote, out)
			}
			dryRun = false
		})
	}
	for _, args := range [][]string{{"rollback", "--boot", revertTestID}, {"rollback", "--boot", "--abandon", revertTestID}} {
		s := newRevertSeams(t, 0)
		calls := len(s.f.Calls())
		rootCmd.SetArgs(args)
		if err := rootCmd.Execute(); err == nil || !s.has(pending) || len(s.f.Calls()) != calls || s.exit != -1 {
			t.Errorf("%q: %v; %d calls, exit %d: want a usage error from cobra, with nothing run", args, err, len(s.f.Calls())-calls, s.exit)
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
		// Run again in the same boot: its timer was started in this boot,
		// and is there.
		s.f.Respond(hostfake.Response{Stdout: "active\n"}, "systemctl", "is-active", "hardn-revert-"+revertTestID+".timer")
		if out := s.output(t, "rollback", "--boot"); s.exit != -1 || !strings.Contains(out, "nothing to do: its timer was started in this boot, and is there") {
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
		if !strings.Contains(out, "No change is journalled, so hardn's boot unit is removed") || strings.Contains(out, "[WARNING]") {
			t.Errorf("the boot unit is not removed:\n%s", out)
		}
	})
	t.Run("past the deadline: the rollback does not finish", func(t *testing.T) {
		s := newRevertSeamsWith(t, 0, unfinishable)
		s.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
		u := rebootSeams(s, time.Hour)
		out := s.output(t, "rollback", "--boot")
		if s.exit != 1 || !strings.Contains(out, "[ERROR] At boot, change "+revertTestID) || !strings.Contains(out, "has passed; the rollback did not finish") ||
			!s.has(revert.DefaultDir+"/"+revert.RollbackName) || !s.has(u.Path) || strings.Contains(out, "boot unit is removed") {
			t.Errorf("exit %d, rollback.json %v, unit %v:\n%s", s.exit, s.has(revert.DefaultDir+"/"+revert.RollbackName), s.has(u.Path), out)
		}
	})
	t.Run("nothing journalled, the unit not hardn's: kept, with a warning", func(t *testing.T) {
		s := newRevertSeams(t, 0)
		executeHardn(t, "confirm")
		u := rebootSeams(s, time.Minute)
		s.f.AddFile(u.Path, []byte("[Service]\nExecStart=/usr/local/bin/other\n"), 0o644, 0, 0)
		if err := s.f.Remove(context.Background(), u.Link); err != nil { // disabled by hand
			t.Fatal(err)
		}
		out := s.output(t, "rollback", "--boot")
		if s.exit != -1 || !strings.Contains(out, "[WARNING] hardn's boot unit was not removed: ") || !strings.Contains(out, "kept, disabled") ||
			strings.Contains(out, "boot unit is removed") || !s.has(u.Path) {
			t.Errorf("exit %d:\n%s", s.exit, out)
		}
	})
	t.Run("nothing journalled", func(t *testing.T) {
		s := newRevertSeams(t, 0)
		executeHardn(t, "confirm")
		rebootSeams(s, time.Minute)
		out := s.output(t, "rollback", "--boot")
		if s.exit != -1 || !strings.Contains(out, "[INFO] At boot: nothing to do: no change is journalled; the last change confirmed is "+revertTestID) ||
			!strings.Contains(out, "hardn's boot unit is removed") || strings.Contains(out, "[WARNING]") || s.has("/etc/systemd/system/hardn-boot.service") ||
			s.has("/etc/systemd/system/multi-user.target.wants/hardn-boot.service") {
			t.Errorf("exit %d:\n%s", s.exit, out)
		}
	})
}

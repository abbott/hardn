package confirm

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The words the clean-up's report uses, per case and per platform: the
// clean-up itself runs over the fake VM in internal/tools/p2drive
// (confirm_test.go) and through cmd/hardn's seams. Synthetic values only.

const testID = "20261018T120000Z-1a2b3c4d"

func held(firewall ...string) Held {
	h := Held{Change: revert.Journal{ID: testID, Summary: "SSH port 22 to 2230",
		ConfirmVia: []revert.Listener{{Addr: netip.IPv4Unspecified(), Port: 2230}, {Addr: netip.IPv6Unspecified(), Port: 2230}}},
		Cleanup: steps.Cleanup{Drop: []uint16{22}}}
	for _, p := range firewall {
		h.Cleanup.Firewall = append(h.Cleanup.Firewall, []string{"ufw", "delete", "allow", p})
	}
	return h
}

func facts(mode detect.SSHMode, doas bool) detect.Facts {
	f := detect.Facts{SSH: detect.SSH{Mode: mode}, Elevation: detect.Elevation{Sudo: detect.Tool{Present: detect.Yes}, Doas: detect.Tool{Present: detect.No}}}
	if doas {
		f.Elevation = detect.Elevation{Sudo: detect.Tool{Present: detect.No}, Doas: detect.Tool{Present: detect.Yes}}
	}
	return f
}

// TestReport: what is done, what is left and how to finish it, for each
// case: nothing held; a console; a session that may not finish it; applied
// (with the rule the change added, and one it did not); nothing left; a
// refusal and a step rolled back on Debian's ssh.service, Ubuntu's
// ssh.socket and Alpine's OpenRC with doas (the reload by hand as each
// starts sshd); a rollback that did not finish.
func TestReport(t *testing.T) {
	plan := func(f detect.Facts) *engine.Plan { return &engine.Plan{ID: "x", Facts: f} }
	refused := fmt.Errorf("%w: %w", engine.ErrRefused, steps.ErrNoWriteStrategy)
	rolledBack := &engine.ApplyError{Change: ControlID + "/reload", Step: engine.StepApply, Err: errors.New("fixture: reload failed")}
	incomplete := &engine.ApplyError{Change: ControlID + "/reload", Step: engine.StepApply, Err: errors.New("fixture: reload failed"),
		RollbackErr: fmt.Errorf("revert: %w", revert.ErrIncomplete)}
	for _, tc := range []struct {
		name                string
		h                   Held
		o                   Outcome
		done, left, finish  []string // each must be in the line(s); nil: none at all
		noDone, noLeft, end bool     // end: Finish must be ""
	}{
		{"nothing held", Held{Change: revert.Journal{ID: testID}}, Outcome{}, nil, nil, nil, true, true, true},
		{"a console", func() Held { h := held(); h.Console = true; return h }(), Outcome{},
			nil, []string{"Port 22, which change " + testID + " held during its confirm window, stays open", "a console shows nothing about SSH"},
			[]string{"`sudo hardn confirm`"}, true, false, false},
		{"a session that may not finish it", func() Held {
			h := held()
			h.Finishing, h.Refused = true, fmt.Errorf("revert: confirm: x: %w", revert.ErrSameSession)
			return h
		}(), Outcome{}, nil, []string{"this session cannot finish it", "this is the SSH connection that applied the change"},
			[]string{"If `sshd -T` still shows port 22", "a new SSH connection to port 2230, made from another machine"}, true, false, false},
		{"applied, the rule the change added deleted", held("2230/tcp", "22/tcp"), Outcome{Plan: plan(facts(detect.SSHService, false)),
			Result: &engine.Result{Applied: []engine.Applied{{Change: ControlID + "/firewall"}, {Change: ControlID + "/files"}}}},
			[]string{"is dropped from /etc/ssh/sshd_config.d/00-hardn.conf", "ufw's rule for 22/tcp, which change " + testID + " added, is deleted."},
			nil, nil, false, true, true},
		{"applied, a rule the change did not add kept", held("2230/tcp"), Outcome{Plan: plan(facts(detect.SSHService, false)),
			Result: &engine.Result{Applied: []engine.Applied{{Change: ControlID + "/files"}}}},
			[]string{"is dropped", "ufw keeps any rule for 22/tcp: change " + testID + " did not add it", "`sudo ufw delete allow 22/tcp` removes it"},
			nil, nil, false, true, true},
		{"applied, no ufw", held(), Outcome{Plan: plan(facts(detect.SSHOpenRC, true)), Result: &engine.Result{}},
			[]string{"is dropped"}, nil, nil, false, true, true},
		{"nothing left", held(), Outcome{Plan: plan(facts(detect.SSHService, false)), Result: &engine.Result{Nothing: true}},
			[]string{"Nothing was left to clean up after change " + testID + ": sshd is no longer configured for port 22."}, nil, nil, false, true, true},
		{"refused, Debian's ssh.service", held("22/tcp"), Outcome{Plan: plan(facts(detect.SSHService, false)), Err: refused},
			nil, []string{"Port 22, which change " + testID + " held during its confirm window, is still open: the `Port 22` line stays in",
				"and ufw keeps its rule for 22/tcp, which change " + testID + " added"},
			[]string{"run `sudo hardn confirm` again from a new SSH connection to port 2230",
				"delete the `Port 22` line from /etc/ssh/sshd_config.d/00-hardn.conf, check the configuration with `sudo sshd -t`, then `sudo systemctl reload ssh.service`, and `sudo ufw delete allow 22/tcp`."},
			true, false, false},
		{"rolled back, Ubuntu's ssh.socket", held(), Outcome{Plan: plan(facts(detect.SSHSocket, false)),
			Result: &engine.Result{Failed: rolledBack}, Err: rolledBack},
			nil, []string{"is still open"}, []string{"then `sudo systemctl daemon-reload` and `sudo systemctl restart ssh.socket`."}, true, false, false},
		{"rolled back, Alpine's OpenRC with doas", held(), Outcome{Plan: plan(facts(detect.SSHOpenRC, true)),
			Result: &engine.Result{Failed: rolledBack}, Err: rolledBack},
			nil, []string{"is still open"}, []string{"run `doas hardn confirm` again", "`doas sshd -t`, then `doas rc-service sshd reload`."}, true, false, false},
		{"the rollback did not finish", held("22/tcp"), Outcome{Plan: plan(facts(detect.SSHService, false)),
			Result: &engine.Result{Failed: incomplete}, Err: incomplete},
			nil, []string{"did not finish, nor did its rollback here, so whether port 22 is still open is not established"},
			[]string{"Once that is done, run `sudo hardn confirm` again"}, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.h.Report(tc.o)
			check := func(what string, got []string, want []string, none bool) {
				t.Helper()
				all := strings.Join(got, "\n")
				if none && len(got) > 0 {
					t.Errorf("%s: %q, want none", what, got)
				}
				for _, w := range want {
					if !strings.Contains(all, w) {
						t.Errorf("%s: no %q in %q", what, w, all)
					}
				}
			}
			check("done", r.Done, tc.done, tc.noDone)
			check("left", r.Left, tc.left, tc.noLeft)
			var finish []string
			if r.Finish != "" {
				finish = append(finish, r.Finish)
			}
			check("finish", finish, tc.finish, tc.end)
		})
	}
}

// TestBackupRoot: the clean-up's backups go where the change's own went,
// the parent of its journal's run directory when that is <path>/<id>;
// backup.DefaultPath otherwise.
func TestBackupRoot(t *testing.T) {
	for _, tc := range []struct {
		b    *revert.Backups
		want string
	}{
		{&revert.Backups{Dir: "/srv/backups/hardn/" + testID}, "/srv/backups/hardn"},
		{nil, backup.DefaultPath},
		{&revert.Backups{Dir: "/srv/backups/hardn/another-id"}, backup.DefaultPath},
		{&revert.Backups{Dir: "relative/" + testID}, backup.DefaultPath},
	} {
		if got := backupRoot(revert.Journal{ID: testID, Backup: tc.b}); got != tc.want {
			t.Errorf("backupRoot(%+v) = %q, want %q", tc.b, got, tc.want)
		}
	}
}

// TestCleanupNotRun: Cleanup does nothing, and touches no host, unless the
// change held a port and this session cleans it up.
func TestCleanupNotRun(t *testing.T) {
	for _, h := range []Held{{}, func() Held { h := held(); h.Console = true; return h }(),
		func() Held { h := held(); h.Refused = revert.ErrOldConnection; return h }()} {
		if o := Cleanup(t.Context(), nil, h, revert.Session{}, Options{}); o.Plan != nil || o.Result != nil || o.Err != nil {
			t.Errorf("Cleanup(%+v) = %+v, want nothing", h, o)
		}
	}
}

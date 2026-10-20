package confirm

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
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
// case: nothing held; a console; a session that may not finish it; a
// clean-up carried from the change confirmed before (a console, a session
// that could not have confirmed it, a step rolled back: by hand alone);
// applied (with the rule hardn added, and one it did not); nothing left; a
// refusal and a step rolled back on Debian's ssh.service, Ubuntu's
// ssh.socket and Alpine's OpenRC with doas (the reload by hand as each
// starts sshd); a rollback that did not finish; another hardn run (the
// state not established); the refusals that forbid dropping the port (no
// way to drop it given). With no plan, the report says that doas takes
// sudo's place where sudo is not installed.
func TestReport(t *testing.T) {
	plan := func(f detect.Facts) *engine.Plan { return &engine.Plan{ID: "x", Facts: f} }
	refused := fmt.Errorf("%w: %w", engine.ErrRefused, steps.ErrNoWriteStrategy)
	rolledBack := &engine.ApplyError{Change: ControlID + "/reload", Step: engine.StepApply, Err: errors.New("fixture: reload failed")}
	incomplete := &engine.ApplyError{Change: ControlID + "/reload", Step: engine.StepApply, Err: errors.New("fixture: reload failed"),
		RollbackErr: fmt.Errorf("revert: %w", revert.ErrIncomplete)}
	const later = "20261018T130000Z-9a8b7c6d"
	carried := func(h Held) Held {
		h.Carried, h.Confirmed = true, &revert.ConfirmResult{Journal: revert.Journal{ID: later, Summary: "SSH: key-only login"}}
		return h
	}
	console := func(h Held) Held { h.Console = true; return h }
	debian, ubuntu, alpine := facts(detect.SSHService, false), facts(detect.SSHSocket, false), facts(detect.SSHOpenRC, true)
	const doasNote = "Where sudo is not installed (Alpine), use doas."
	for _, tc := range []struct {
		name               string
		h                  Held
		o                  Outcome
		done, left, finish []string // each must be in the line(s)
		not                []string // none may be anywhere in the report
		noDone, noLeft     bool     // none at all
		end                bool     // Finish must be ""
	}{
		{name: "nothing held", h: Held{Change: revert.Journal{ID: testID}}, noDone: true, noLeft: true, end: true},
		{name: "a console", h: console(held()),
			left:   []string{"Port 22, which change " + testID + " held during its confirm window, stays open", "a console shows nothing about SSH"},
			finish: []string{"`sudo hardn confirm`", doasNote}, noDone: true},
		{name: "a session that may not finish it", h: func() Held {
			h := held()
			h.Finishing, h.Refused = true, fmt.Errorf("revert: confirm: x: %w", revert.ErrSameSession)
			return h
		}(), left: []string{"Port 22, which change " + testID + ", the last one confirmed, held during its confirm window, stays open: the `Port 22` line stays in",
			"this session cannot finish its clean-up", "this is the SSH connection that applied the change"},
			finish: []string{"Run `sudo hardn confirm` from a new SSH connection to port 2230, made from another machine.", doasNote}, noDone: true},
		{name: "carried, a console", h: carried(console(held("22/tcp"))),
			left: []string{"Port 22, which change " + testID + ", confirmed before change " + later + ", held during its confirm window, stays open",
				"a console shows nothing about SSH", "now that change " + later + " is the last one confirmed, `hardn confirm` no longer finds it"},
			finish: []string{"To drop it, from a new SSH connection to port 2230, made from another machine, by hand: delete the `Port 22` line",
				"`sudo ufw delete allow 22/tcp`", doasNote},
			not: []string{"hardn confirm` from", "hardn confirm` again"}, noDone: true},
		{name: "carried, a session that could not have confirmed it", h: func() Held {
			h := carried(held())
			h.Refused = fmt.Errorf("revert: confirm: connected to 192.0.2.20:22: %w", revert.ErrWrongListener)
			return h
		}(), left: []string{"stays open", "this session cannot drop it (revert: confirm: connected to 192.0.2.20:22", "`hardn confirm` no longer finds it"},
			finish: []string{"by hand: delete the `Port 22` line"}, not: []string{"hardn confirm` again"}, noDone: true},
		{name: "carried, rolled back", h: carried(held()), o: Outcome{Plan: plan(debian), Result: &engine.Result{Failed: rolledBack}, Err: rolledBack},
			left:   []string{"Port 22, which change " + testID + ", confirmed before change " + later + ", held during its confirm window, is still open"},
			finish: []string{"drop it by hand, from a new SSH connection to port 2230", "`hardn confirm` no longer finds it", "`sudo systemctl reload ssh.service`"},
			not:    []string{"hardn confirm` again", doasNote}, noDone: true},
		{name: "applied, the rule hardn added deleted", h: held("2230/tcp", "22/tcp"), o: Outcome{Plan: plan(debian),
			Result: &engine.Result{Applied: []engine.Applied{{Change: ControlID + "/firewall"}, {Change: ControlID + "/files"}}}},
			done:   []string{"is dropped from /etc/ssh/sshd_config.d/00-hardn.conf", "ufw's rule for 22/tcp, which hardn added, is deleted."},
			noLeft: true, end: true},
		{name: "applied, a rule hardn did not add kept", h: held("2230/tcp"), o: Outcome{Plan: plan(debian),
			Result: &engine.Result{Applied: []engine.Applied{{Change: ControlID + "/files"}}}},
			done:   []string{"is dropped", "ufw keeps any rule for 22/tcp: hardn did not add it", "`sudo ufw delete allow 22/tcp` removes it"},
			noLeft: true, end: true},
		{name: "applied, no ufw", h: held(), o: Outcome{Plan: plan(alpine), Result: &engine.Result{}},
			done: []string{"is dropped"}, noLeft: true, end: true},
		{name: "nothing left", h: held(), o: Outcome{Plan: plan(debian), Result: &engine.Result{Nothing: true}},
			done: []string{"Nothing was left to clean up after change " + testID + ": sshd is no longer configured for port 22."}, noLeft: true, end: true},
		{name: "refused, Debian's ssh.service", h: held("22/tcp"), o: Outcome{Plan: plan(debian), Err: refused},
			left: []string{"Port 22, which change " + testID + " held during its confirm window, is still open: the `Port 22` line stays in",
				"and ufw keeps its rule for 22/tcp, which hardn added"},
			finish: []string{"run `sudo hardn confirm` again from a new SSH connection to port 2230",
				"delete the `Port 22` line from /etc/ssh/sshd_config.d/00-hardn.conf, check the configuration with `sudo sshd -t`, then `sudo systemctl reload ssh.service`, and `sudo ufw delete allow 22/tcp`."},
			not: []string{doasNote}, noDone: true},
		{name: "rolled back, Ubuntu's ssh.socket", h: held(), o: Outcome{Plan: plan(ubuntu), Result: &engine.Result{Failed: rolledBack}, Err: rolledBack},
			left: []string{"is still open"}, finish: []string{"then `sudo systemctl daemon-reload` and `sudo systemctl restart ssh.socket`."}, noDone: true},
		{name: "rolled back, Alpine's OpenRC with doas", h: held(), o: Outcome{Plan: plan(alpine), Result: &engine.Result{Failed: rolledBack}, Err: rolledBack},
			left: []string{"is still open"}, finish: []string{"run `doas hardn confirm` again", "`doas sshd -t`, then `doas rc-service sshd reload`."},
			not: []string{"sudo"}, noDone: true},
		{name: "the rollback did not finish", h: held("22/tcp"), o: Outcome{Plan: plan(debian), Result: &engine.Result{Failed: incomplete}, Err: incomplete},
			left:   []string{"did not finish, nor did its rollback here, so whether port 22 is still open is not established"},
			finish: []string{"Once that is done, run `sudo hardn confirm` again"}, noDone: true},
		{name: "another hardn changed the host since the plan", h: held("22/tcp"),
			o:      Outcome{Plan: plan(debian), Result: &engine.Result{}, Err: fmt.Errorf("engine: change x: %w", engine.ErrPlanChanged)},
			left:   []string{"Another hardn run changed the host", "may have dropped port 22", "whether it is still open is not established"},
			finish: []string{"If `sudo sshd -T` still shows port 22, run `sudo hardn confirm` again"},
			not:    []string{"is still open:", "stays in"}, noDone: true},
		{name: "another hardn holds the run lock", h: held(), o: Outcome{Plan: plan(debian), Result: &engine.Result{}, Err: fmt.Errorf("engine: lock: %w", host.ErrLocked)},
			left: []string{"is not established"}, not: []string{"is still open:"}, noDone: true},
		{name: "a change journalled", h: held(), o: Outcome{Plan: plan(debian), Err: fmt.Errorf("%w: %w", engine.ErrRefused, &revert.PendingError{})},
			left: []string{"is not established"}, not: []string{"is still open:"}, noDone: true},
		{name: "Proxmox VE's port 22", h: held(), o: Outcome{Plan: plan(debian), Err: fmt.Errorf("%w: ssh.cleanup: pve: port [2230]: %w", engine.ErrRefused, pve.ErrPortChange)},
			left: []string{"Port 22, which change " + testID + " held during its confirm window, stays open: on Proxmox VE the SSH port stays 22"},
			not:  []string{"delete the `Port 22` line", "hardn confirm"}, noDone: true, end: true},
		{name: "no port left", h: held(), o: Outcome{Plan: plan(debian), Err: fmt.Errorf("%w: %w", engine.ErrRefused, reload.ErrDropsListener)},
			left: []string{"stays open: sshd is configured for no other port now"},
			not:  []string{"delete the `Port 22` line", "hardn confirm"}, noDone: true, end: true},
		{name: "the session on the port dropped", h: held(), o: Outcome{Plan: plan(debian), Err: fmt.Errorf("%w: %w", engine.ErrRefused, steps.ErrSessionNotKept)},
			left:   []string{"stays open: this session is not connected to a port the clean-up keeps"},
			finish: []string{"Run `sudo hardn confirm` again from a new SSH connection to port 2230, made from another machine."},
			not:    []string{"delete the `Port 22` line"}, noDone: true},
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
			all := strings.Join(append(append(slices.Clone(r.Done), r.Left...), r.Finish), "\n")
			for _, n := range tc.not {
				if strings.Contains(all, n) {
					t.Errorf("%q in the report:\n%s", n, all)
				}
			}
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

// unreadable fails every read, as a file hardn may not read would.
type unreadable struct{ *hostfake.System }

func (unreadable) ReadFile(context.Context, string) ([]byte, error) {
	return nil, errors.New("fixture: permission denied")
}

// TestStillHeld: the held ports 00-hardn.conf still has a Port line for,
// read as sshd reads its lines (any case, comments skipped); none without
// the file; all of them when it cannot be read.
func TestStillHeld(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string // "": no 00-hardn.conf
		ps   []uint16
		want []uint16
	}{
		{"no file", "", []uint16{22}, nil},
		{"held", "# Managed by hardn.\nPort 22\nPort 2230\n", []uint16{22}, []uint16{22}},
		{"dropped", "# Managed by hardn.\nPort 2230\n", []uint16{22}, nil},
		{"any case, comments skipped", "# Port 2222\nport 2222\nPORT 22\nPort 2230\n", []uint16{22, 2222, 2224}, []uint16{22, 2222}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := hostfake.New(host.Options{})
			if tc.data != "" {
				f.AddFile(sshd.HardnDropIn, []byte(tc.data), 0o644, 0, 0)
			}
			if got := stillHeld(t.Context(), f, tc.ps); !slices.Equal(got, tc.want) {
				t.Errorf("stillHeld(%v) = %v, want %v", tc.ps, got, tc.want)
			}
		})
	}
	if got := stillHeld(t.Context(), unreadable{hostfake.New(host.Options{})}, []uint16{22, 2222}); !slices.Equal(got, []uint16{22, 2222}) {
		t.Errorf("stillHeld of an unreadable file = %v, want every port", got)
	}
}

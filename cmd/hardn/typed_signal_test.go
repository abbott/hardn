package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine/hold"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// The typed-hostname flow's signal decision (runTyped: settle over its
// apply windows, then endControls), as signalInRollback and
// TestConfigureDNSFlagSignalInRollbackEveryTime check runControls': a
// signal during the apply's rollback or during the report ends hardn as
// runControls ends a flag, the flags after it not run.

// typedReportWrite is w with fire called before the first write of the
// apply's report ("Operation Successful"): as the flow reports the apply.
type typedReportWrite struct {
	w    io.Writer
	fire func()
}

func (f typedReportWrite) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("Operation Successful")) {
		f.fire()
	}
	return f.w.Write(p)
}

// typedSignal runs -w -e on a terminal on the Firewall host whose
// Enable's plan is lockout-risk, the hostname typed, sig sent to this
// test process during the apply's rollback (inRollback: ufw's start
// fails and the change is rolled back, ufw stopped and 22 no longer
// allowed; sig is sent under the engine's own hold, hold.Signals, which
// catches it) or during the report of an apply that succeeded; it
// returns what hardn did other than end with the failure's status, 1,
// after the rollback, or 130 after the report, -e not run, the apply's
// context cancelled, or "".
func typedSignal(t *testing.T, sig syscall.Signal, inRollback bool) string {
	t.Helper()
	signals := engineSignals // the real one: useDNSHost replaces it
	h := newFWHost(t, "debian13")
	ran := withSudo(h.dnsHost) // -e keeps HARDN_CONFIG for alice there
	if inRollback {
		answer := h.Handler
		h.Handler = func(argv []string, in []byte) hostfake.Response {
			if strings.Join(argv, " ") == "ufw --force enable" {
				return hostfake.Response{Stderr: "ERROR: problem running ufw-init\n", ExitCode: 1}
			}
			return answer(argv, in)
		}
	}
	ended, fired := 0, 0
	var applying context.Context
	r, s := runW(t, h, onTTY, []string{"web-01"}, func(s *dnsSeams) {
		engineSignals = func(c context.Context) (context.Context, context.CancelFunc) {
			s.applies++
			ctx, stop := signals(c)
			applying = ctx
			return ctx, stop
		}
		if inRollback {
			engineHold = func() func() {
				s.held++
				release := hold.Signals(nil) // the engine's own
				fired++
				sendSignal(t, sig)
				return release
			}
		} else {
			engineStdout = typedReportWrite{&s.out, func() {
				if fired++; fired > 1 {
					return
				}
				sendSignal(t, sig)
				select {
				case <-applying.Done():
				case <-time.After(5 * time.Second):
					t.Errorf("%v did not reach the apply's context", sig)
				}
			}}
		}
		endHangup = func() { ended++ } // a screen's end of hardn, not a flag's
	}, "-w", "-e")
	out := stripANSI(s.out.String())
	where, want := "during the report", 130
	if inRollback {
		where, want = "during the rollback", 1
	}
	cancelled := applying != nil && applying.Err() != nil
	if fired != 1 || !r.ended || r.code != want || ran() || ended != 0 || s.applies != 1 || !cancelled {
		return fmt.Sprintf("%v %s: fired %d; ended %v with %d, want %d; -e ran %v, ended by a hangup %d times, %d applies, the apply's context cancelled %v; report:\n%s\nstderr:\n%s",
			sig, where, fired, r.ended, r.code, want, ran(), ended, s.applies, cancelled, out, r.stderr)
	}
	if !strings.Contains(out, "Type the hostname") {
		return fmt.Sprintf("%v %s: the hostname not asked:\n%s", sig, where, out)
	}
	if inRollback && (h.active || len(h.allowed) != 0 || s.held != 1) {
		return fmt.Sprintf("%v %s: ufw running %v after %d holds, changed by %q; report:\n%s", sig, where, h.active, s.held, h.changed(), out)
	}
	if !inRollback && (!h.active || !strings.Contains(out, "Operation Successful")) {
		return fmt.Sprintf("%v %s: not applied as reported, ufw running %v:\n%s", sig, where, h.active, out)
	}
	return ""
}

// TestConfigureFirewallFlagSignalTypedEveryTime: on a terminal, -w's
// lockout-risk plan confirmed by the hostname typed (runTyped), a hangup
// or SIGTERM during the apply's rollback ends hardn with the failure's
// status, 1, and one during the report ends it with 130, as runControls
// ends -g (signalInRollback; TestConfigureDNSFlagWithOthers), -e not run
// and the apply's context cancelled, on every run: each 50 times on one
// CPU (GOMAXPROCS 1), where the race the fix of 8367f031 closed showed
// most.
func TestConfigureFirewallFlagSignalTypedEveryTime(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	for _, inRollback := range []bool{true, false} {
		for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGTERM} {
			where := map[bool]string{true: "during the rollback", false: "during the report"}[inRollback]
			t.Run(fmt.Sprintf("%v %s", sig, where), func(t *testing.T) {
				fails, first := 0, ""
				for range 50 {
					if failed := typedSignal(t, sig, inRollback); failed != "" {
						if fails++; first == "" {
							first = failed
						}
					}
				}
				if fails > 0 {
					t.Errorf("%d runs in 50 not ended by %v as a flag's; the first:\n%s", fails, sig, first)
				}
			})
		}
	}
}

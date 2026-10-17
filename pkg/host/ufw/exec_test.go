package ufw_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
)

func plan(t *testing.T, m *model, ls sshd.Listeners, add []uint16) ufw.Plan {
	t.Helper()
	p, err := ufw.PlanFor(ufw.Input{Proxmox: notPVE, Listeners: ls, Add: add, State: read(t, m), Desired: hardening})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func undos(rep ufw.Report) []string {
	var out []string
	for _, u := range rep.Undo {
		out = append(out, strings.Join(u, " "))
	}
	return out
}

// TestExecute2230: the brief's scenario on a fresh, inactive ufw. SSH on
// 22 (IPv4 and IPv6) and the new 2230 are let through before ufw starts,
// the status shows all four, the second plan is empty, and the undo puts
// the host back.
func TestExecute2230(t *testing.T) {
	ctx := context.Background()
	m := newModel(t)
	h := m.host()
	p := plan(t, m, stock22(), []uint16{2230})
	rep, err := ufw.Execute(ctx, h, p)
	if err != nil || !rep.Verified {
		t.Fatalf("Execute: %v, Verified %t (%s)", err, rep.Verified, rep.Unverified)
	}
	want := []string{"ufw allow 22/tcp", "ufw allow 2230/tcp", "ufw --force enable", "ufw status verbose"}
	if got := runs(h); !slices.Equal(got, want) {
		t.Errorf("runs\n got %q\nwant %q", got, want)
	}
	for _, c := range rep.Coverage {
		if c.Verdict != ufw.Allowed {
			t.Errorf("%s %d: %s by %q", c.Need.Family, c.Need.Port, c.Verdict, c.By)
		}
	}
	if len(rep.Coverage) != 4 || len(rep.Status.Rules) != 4 {
		t.Errorf("coverage %d, rules %d; want 4 and 4", len(rep.Coverage), len(rep.Status.Rules))
	}
	if again := plan(t, m, stock22(), []uint16{2230}); !again.Empty() {
		t.Errorf("second plan %q, want empty", argvs(again.Steps))
	}
	wantUndo := []string{"ufw disable", "ufw delete allow 2230/tcp", "ufw delete allow 22/tcp"}
	if got := undos(rep); !slices.Equal(got, wantUndo) {
		t.Errorf("undo %q, want %q", got, wantUndo)
	}
	done, cancel := context.WithCancel(ctx)
	cancel()
	if err := ufw.Rollback(context.WithoutCancel(done), h, rep); err != nil {
		t.Fatal(err)
	}
	if m.active || len(m.rules) != 0 {
		t.Errorf("after rollback: active %t, rules %v", m.active, m.rules)
	}
}

// TestExecuteSocket: Ubuntu's ssh.socket on [::]:22, dual-stack, and an
// active ufw that lets only IPv6 through: IPv4 needs the rule too.
func TestExecuteSocket(t *testing.T) {
	m := newModel(t)
	m.active = true
	m.rules = []modelRule{{fam: ufw.IPv6, action: "allow", port: "22"}}
	sock := []sshd.Listener{listener("::", 22)}
	ls := sshd.Listeners{SSHD: stock22().SSHD, Socket: sock, SocketRead: true, All: stock22().SSHD, Known: true}
	h := m.host()
	rep, err := ufw.Execute(context.Background(), h, plan(t, m, ls, nil))
	if err != nil || !rep.Verified {
		t.Fatalf("Execute: %v, Verified %t", err, rep.Verified)
	}
	if got := runs(h); !slices.Equal(got, []string{"ufw allow 22/tcp", "ufw status verbose"}) {
		t.Errorf("runs %q", got)
	}
	// ufw added IPv4's rule and skipped IPv6's: the undo would delete both.
	if len(rep.Undo) != 0 || !strings.Contains(rep.Steps[0].Note, "not undone") {
		t.Errorf("undo %q, note %q", undos(rep), rep.Steps[0].Note)
	}
}

func TestExecuteFailures(t *testing.T) {
	ctx := context.Background()
	t.Run("ufw answers Aborted and stays inactive", func(t *testing.T) {
		m := newModel(t)
		m.enableOut = "Aborted\n"
		h := m.host()
		rep, err := ufw.Execute(ctx, h, plan(t, m, stock22(), nil))
		if !errors.Is(err, ufw.ErrVerify) {
			t.Fatalf("Execute: %v, want ErrVerify", err)
		}
		if got := undos(rep); !slices.Equal(got, []string{"ufw disable", "ufw delete allow 22/tcp"}) {
			t.Errorf("undo %q", got)
		}
	})
	t.Run("a rule missing from the status", func(t *testing.T) {
		m := newModel(t)
		m.forget = "22" // ufw answers "Rule added" and saves nothing
		h := m.host()
		_, err := ufw.Execute(ctx, h, plan(t, m, stock22(), nil))
		if !errors.Is(err, ufw.ErrVerify) || !strings.Contains(err.Error(), "listener 0.0.0.0:22") {
			t.Errorf("Execute: %v, want ErrVerify naming the listener", err)
		}
	})
	t.Run("a rollback runs every undo, even after one fails", func(t *testing.T) {
		m := newModel(t)
		h := m.host()
		rep, err := ufw.Execute(ctx, h, plan(t, m, stock22(), []uint16{2230}))
		if err != nil {
			t.Fatal(err)
		}
		m.fail = "ufw disable"
		var xe *host.ExitError
		if err := ufw.Rollback(ctx, h, rep); !errors.As(err, &xe) || !strings.Contains(err.Error(), "ufw disable") {
			t.Errorf("Rollback: %v", err)
		}
		if len(m.rules) != 0 {
			t.Errorf("rules left %v", m.rules)
		}
	})
	t.Run("a step fails: later steps do not run", func(t *testing.T) {
		m := newModel(t)
		m.fail = "ufw allow 2230/tcp"
		h := m.host()
		rep, err := ufw.Execute(ctx, h, plan(t, m, stock22(), []uint16{2230}))
		var xe *host.ExitError
		if !errors.As(err, &xe) || xe.ExitCode != 1 {
			t.Fatalf("Execute: %v, want *host.ExitError", err)
		}
		if got := runs(h); !slices.Equal(got, []string{"ufw allow 22/tcp", "ufw allow 2230/tcp"}) {
			t.Errorf("runs %q", got)
		}
		if got := undos(rep); !slices.Equal(got, []string{"ufw delete allow 22/tcp"}) {
			t.Errorf("undo %q", got)
		}
	})
	t.Run("an existing rule is not undone", func(t *testing.T) {
		m := newModel(t)
		m.rules = []modelRule{{fam: ufw.IPv4, action: "allow", port: "22"}, {fam: ufw.IPv6, action: "allow", port: "22"}}
		h := m.host()
		p := ufw.Plan{Needs: ufw.Needs(stock22(), nil, nil), Incoming: ufw.PolicyDeny, IPv6: true,
			Steps: []ufw.Step{{Kind: ufw.StepAllow, Argv: []string{"ufw", "allow", "22/tcp"}, Undo: []string{"ufw", "delete", "allow", "22/tcp"}}}}
		rep, err := ufw.Execute(ctx, h, p)
		if err != nil || rep.Verified || rep.Unverified == "" {
			t.Fatalf("Execute: %v, Verified %t, Unverified %q", err, rep.Verified, rep.Unverified)
		}
		if len(rep.Undo) != 0 || rep.Steps[0].Note != "ufw kept an existing rule" {
			t.Errorf("undo %q, note %q", undos(rep), rep.Steps[0].Note)
		}
	})
	t.Run("a done context runs nothing", func(t *testing.T) {
		m := newModel(t)
		h := m.host()
		p := plan(t, m, stock22(), nil)
		done, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := ufw.Execute(done, h, p); !errors.Is(err, context.Canceled) || len(h.Calls()) != 0 {
			t.Errorf("Execute: %v, calls %v", err, h.Calls())
		}
	})
}

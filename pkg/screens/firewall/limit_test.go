package firewall_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	fwscreen "github.com/abbott/hardn/pkg/screens/firewall"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// Limit SSH, item 5 (docs/engine/gallery-bruteforce.md, adopted 2026-11-13;
// docs/engine/screen-bruteforce.md).

const (
	limitNotice = "ufw refuses an address that opens 6 or more SSH connections in 30 seconds, yours included."
	limitAsk    = "Limit SSH? [y/N"
)

// limitRan are the ufw commands h ran from its nth run on that are not reads.
func limitRan(h *fwHost, n int) []string {
	var out []string
	for _, r := range h.Runs()[n:] {
		if cmd := strings.Join(r, " "); r[0] == "ufw" && cmd != "ufw status verbose" {
			out = append(out, cmd)
		}
	}
	return out
}

// TestLimit: item 5 on a host whose ufw allows 22 from anywhere: the note,
// SSH's rules as the limit leaves them, the plan, y/N and never the
// hostname; then the screen again, SSH Rate limited, the rules Limit, and no
// item 5.
func TestLimit(t *testing.T) {
	h, cfg := screenHost(t, "active22")
	n := len(h.Runs())
	p := play(h, h, cfg, false, "5", "y", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 1 || p.runs[0].Err != nil {
		t.Fatalf("Run = %v, %d unread, runs %+v:\n%s", p.err, p.unread, p.runs, p.out)
	}
	if w := inOrder(p.out, "5) Limit SSH Block repeated connections", "Limit SSH #", limitNotice, "22/tcp Limit", "22/tcp (v6) Limit",
		"Limit 22/tcp", "-allow 22/tcp", "+limit 22/tcp", "$ ufw limit 22/tcp", limitAsk, "UFW Firewall Configuration #", "[::]:22 Rate limited", "0.0.0.0:22 Rate limited",
		"22/tcp Limit", "22/tcp (v6) Limit", "1) Disable firewall"); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	if strings.Contains(p.out, "Type the hostname") {
		t.Errorf("the hostname asked: the change is y/N:\n%s", p.out)
	}
	s := screens(p.out, "UFW Firewall Configuration")
	if got := items(s[len(s)-1]); strings.Contains(got, "Limit SSH") || rowOf(s[len(s)-1], "Firewall") != "Configured deny policy" {
		t.Errorf("after: items %q, the row %q", got, rowOf(s[len(s)-1], "Firewall"))
	}
	if got := limitRan(h, n); !slices.Equal(got, []string{"ufw limit 22/tcp"}) {
		t.Errorf("ran %q", got)
	}
	if len(h.rules) != 1 || h.rules[0].verb() != "limit" || !h.active {
		t.Errorf("after: active %t, rules %+v", h.active, h.rules)
	}
}

// TestLimitDeclined: n, 0 and a dry run change nothing; the dry run stops at
// its plan.
func TestLimitDeclined(t *testing.T) {
	for _, tc := range []struct {
		dryRun bool
		lines  []string
	}{{false, []string{"5", "n", "0"}}, {false, []string{"5", "0", "0"}}, {true, []string{"5", "0"}}} {
		h, cfg := screenHost(t, "active22")
		n := len(h.Runs())
		p := play(h, h, cfg, tc.dryRun, tc.lines...)
		if p.err != nil || p.unread != 0 {
			t.Fatalf("%v: Run = %v, %d unread:\n%s", tc, p.err, p.unread, p.out)
		}
		if got := limitRan(h, n); len(got) != 0 || h.rules[0].verb() != "allow" {
			t.Errorf("%v: ran %q, rules %+v", tc, got, h.rules)
		}
		if tc.dryRun && (!strings.Contains(flat(p.out), planned) || !strings.Contains(p.out, "$ ufw limit 22/tcp") || strings.Contains(p.out, limitAsk)) {
			t.Errorf("a dry run: its plan, and no question:\n%s", p.out)
		}
	}
}

// TestLimitFails: a limit ufw refuses is rolled back with the allow, and the
// screen is drawn again with SSH still allowed.
func TestLimitFails(t *testing.T) {
	h, cfg := screenHost(t, "active22")
	h.fail = "ufw limit 22/tcp"
	n := len(h.Runs())
	p := play(h, h, cfg, false, "5", "y", "0")
	if p.err != nil || len(p.runs) != 1 || p.runs[0].Err == nil {
		t.Fatalf("Run = %v, runs %+v:\n%s", p.err, p.runs, p.out)
	}
	if got := limitRan(h, n); !slices.Equal(got, []string{"ufw limit 22/tcp", "ufw allow 22/tcp"}) {
		t.Errorf("ran %q", got)
	}
	s := screens(p.out, "UFW Firewall Configuration")
	if len(h.rules) != 1 || h.rules[0].verb() != "allow" || !strings.Contains(flat(s[len(s)-1]), "0.0.0.0:22 Allowed") {
		t.Errorf("after: rules %+v:\n%s", h.rules, s[len(s)-1])
	}
}

// TestLimitTwoPorts: sshd on 22 and 2230, each allowed: the review names
// both, in ufw status' order, and one answer limits both.
func TestLimitTwoPorts(t *testing.T) {
	h, cfg := screenHost(t, "active22")
	h.ports, h.rules = []uint16{22, 2230}, []fwRule{{port: "22"}, {port: "2230"}}
	h.AddFile("/etc/ssh/sshd_config.d/00-hardn.conf", []byte("Port 22\nPort 2230\n"), 0o644, 0, 0)
	h.syncFiles()
	n := len(h.Runs())
	p := play(h, h, cfg, false, "5", "y", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 1 || p.runs[0].Err != nil {
		t.Fatalf("Run = %v, %d unread, runs %+v:\n%s", p.err, p.unread, p.runs, p.out)
	}
	if w := inOrder(p.out, "Limit SSH #", "22/tcp Limit", "2230/tcp Limit", "22/tcp (v6) Limit", "2230/tcp (v6) Limit", "Limit 22/tcp, 2230/tcp", limitAsk); w != "" {
		t.Errorf("%q not shown after what came before it:\n%s", w, p.out)
	}
	if got := limitRan(h, n); !slices.Equal(got, []string{"ufw limit 22/tcp", "ufw limit 2230/tcp"}) {
		t.Errorf("ran %q", got)
	}
}

// TestLimitNotOffered: no item 5 where ufw is not running, where SSH is
// limited already, let through from some sources only, by an application's
// rule or not at all, where what accepts SSH is not read, on Proxmox VE and
// on firewalld.
func TestLimitNotOffered(t *testing.T) {
	limited := func(h *fwHost) { h.rules = []fwRule{{port: "22", action: "limit"}} }
	for host, set := range map[string]func(*fwHost){
		"ubuntu2604": nil, "blocklist": nil, "lockout-at-start": nil, // not running
		"active22":   limited,
		"allowlist":  nil,
		"denied":     nil,
		"lockout":    nil, // 443 allowed, sshd on 2230
		"moved-2230": nil, "no-ss-active": nil, "garbled": nil, "status-fails": nil, "proxmox-unknown": nil,
		"profiles":     func(h *fwHost) { h.rules = []fwRule{{port: "22", app: "OpenSSH"}} },
		"pve-ufw-left": nil, "pve9": nil,
		"rocky-missing": nil, "rocky-stopped": nil, "rocky-at-boot": nil, "rocky-running": nil,
	} {
		h, cfg := screenHost(t, host)
		if set != nil {
			set(h)
			h.syncFiles()
		}
		p := play(system(host, h), h, cfg, false, "0")
		if p.err != nil || strings.Contains(p.out, "Limit SSH") || strings.Contains(p.out, " 5) ") {
			t.Errorf("%s: Run = %v, item 5 offered:\n%s", host, p.err, p.out)
		}
	}
}

// TestLimitRefusedAtTheKey: the plan is made again when 5 is chosen, so a
// rule that changed since the screen was drawn is refused before anything is
// asked, and nothing runs.
func TestLimitRefusedAtTheKey(t *testing.T) {
	h, cfg := screenHost(t, "active22")
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	// The administrator replaces the rule between the draw and the key.
	ty := uitest.NewTypist(&out, "5", "0")
	first := true
	p := playWith(h, h, cfg, false, func(o *fwscreen.Options) {
		o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(readerFunc(func(b []byte) (int, error) {
			if first {
				first = false
				h.rules = []fwRule{{port: "22", app: "OpenSSH"}}
				h.syncFiles()
			}
			return ty.Read(b)
		}), false), Out: env.Writer(&out)}
	})
	const words = "ufw's rule for SSH isn't a plain allow of its port, and hardn doesn't change a rule it finds. Set the limit with ufw limit."
	if p.err != nil || len(p.runs) != 0 || !strings.Contains(flat(out.String()), words) || strings.Contains(out.String(), limitAsk) {
		t.Errorf("Run = %v, runs %+v:\n%s", p.err, p.runs, out.String())
	}
}

// readerFunc is a Reader from a func.
type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(b []byte) (int, error) { return f(b) }

// TestLimitGolden pins item 5's review up to its question, declined, on the
// core set. The file is testdata/golden/limit/active22.golden.
func TestLimitGolden(t *testing.T) {
	uitest.Golden(t, "limit/active22", uitest.Core(), func(env term.Env) string {
		h, cfg := screenHost(t, "active22")
		var out bytes.Buffer
		p := playWith(h, h, cfg, false, func(o *fwscreen.Options) {
			o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, "5", "n", "0"), false), Out: env.Writer(&out)}
		})
		if p.err != nil || p.unread != 0 {
			t.Errorf("Run = %v, %d unread", p.err, p.unread)
		}
		return runID.ReplaceAllString(out.String(), runDir)
	})
}

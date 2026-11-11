package firewall_test

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/detect"
	fwscreen "github.com/abbott/hardn/pkg/screens/firewall"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// containerLine is the muted line ufw's and firewalld's sections end with in a
// container (the maintainer, 2026-11-11).
const containerLine = "The container's host may filter too; hardn can't see it."

// colours are the SGR escapes a terminal's output holds.
var colours = regexp.MustCompile("\x1b\\[[0-9;]*m")

// lxc is detect's container as an LXC's PID 1 environment establishes it.
var lxc = detect.Container{Detected: detect.Yes, Signal: detect.InitEnviron, Manager: "lxc"}

// containerHosts are the goldens of the screen in a container: each name's
// golden, the host it is drawn on with lxc's container.
var containerHosts = []struct {
	name, host string
	lines      int // drawn at 80x24 and 80x40, the title to the prompt: the host's own (TestHeights) and one
}{
	{"alpine-lxc", "alpine-stock", 22},           // Alpine 3.23 as the LXC dogfood's (alpine.md §7): no ufw, no ss
	{"alpine-lxc-active", "alpine-runlevel", 32}, // ufw active, every ufw row drawn: over 24, as decision 9 has it
	{"rocky-lxc", "rocky-stopped", 21},           // firewalld's section
}

// TestContainerGoldens pins the screen in a container, opened and left with 0,
// on the core set: the container line last in ufw's or firewalld's section.
// The files are testdata/golden/screen/<name>.golden.
func TestContainerGoldens(t *testing.T) {
	for _, c := range containerHosts {
		uitest.Golden(t, "screen/"+c.name, uitest.Core(), func(env term.Env) string {
			h, cfg := screenHost(t, c.host)
			h.facts.Container = lxc
			var out bytes.Buffer
			p := playWith(system(c.host, h), h, cfg, false, func(o *fwscreen.Options) {
				o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, "0"), false), Out: env.Writer(&out)}
			})
			if p.err != nil {
				t.Errorf("%s: Run = %v", c.name, p.err)
			}
			return out.String()
		})
	}
}

// TestContainerHeights: the line costs one line, on a terminal at 80x24 and
// 80x40, in Unicode and ASCII, the prompt on the last; within 24 where the host
// was.
func TestContainerHeights(t *testing.T) {
	for _, c := range containerHosts {
		for _, rows := range []int{24, 40} {
			for _, unicode := range []bool{true, false} {
				h, cfg := screenHost(t, c.host)
				h.facts.Container = lxc
				lang := map[bool]string{true: "LANG=en_US.UTF-8", false: "LANG=C"}[unicode]
				env := term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Height: rows, Environ: []string{"TERM=xterm-256color", lang}})
				var out bytes.Buffer
				p := playWith(h, h, cfg, false, func(o *fwscreen.Options) {
					o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(strings.NewReader("0\n"), false), Out: env.Writer(&out)}
				})
				drawn := strings.Split(colours.ReplaceAllString(out.String(), ""), " or q]:")[0]
				got := strings.Split(drawn, "\n")
				for len(got) > 0 && !strings.Contains(got[0], "Firewall Configuration") {
					got = got[1:]
				}
				if p.err != nil || len(got) != c.lines || !strings.Contains(drawn, containerLine) {
					t.Errorf("%s at 80x%d, unicode %v: Run = %v, %d lines, want %d:\n%s", c.name, rows, unicode, p.err, len(got), c.lines, drawn)
				}
			}
		}
	}
}

// TestContainerLine: the line is drawn once, after the section's other rows
// and before the next section, only where detect establishes a container: not
// where it is not established, not out of one, and not on Proxmox VE, whose
// screen is read only and filters as the node's.
func TestContainerLine(t *testing.T) {
	for _, c := range []struct {
		name, host string
		container  detect.Container
		next       string // the section after the line's, "" for the line's absence
	}{
		{"alpine, no ufw", "alpine-stock", lxc, "SSH"},
		{"debian, active", "active22", lxc, "SSH"},
		{"rocky, firewalld", "rocky-running", lxc, "Select an option"},
		{"not established", "alpine-stock", detect.Container{Detected: detect.Unknown, Err: errors.New("detect: container: /proc/1/environ: permission denied")}, ""},
		{"not a container", "alpine-stock", detect.Container{Detected: detect.No}, ""},
		{"proxmox", "pve9", lxc, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, cfg := screenHost(t, c.host)
			h.facts.Container = c.container
			p := play(system(c.host, h), h, cfg, false, "0")
			if p.err != nil {
				t.Fatalf("Run = %v", p.err)
			}
			n := strings.Count(p.out, containerLine)
			switch {
			case c.next == "" && n != 0:
				t.Errorf("the container line drawn:\n%s", p.out)
			case c.next != "" && n != 1:
				t.Errorf("the container line drawn %d times, want once:\n%s", n, p.out)
			case c.next != "":
				after := p.out[strings.Index(p.out, containerLine)+len(containerLine):]
				if i := strings.Index(after, c.next); i < 0 || strings.TrimSpace(after[:i]) != "" && !onlyRule(after[:i]) {
					t.Errorf("the container line not last before %q:\n%s", c.next, p.out)
				}
			}
		})
	}
}

// onlyRule is whether s holds nothing but blank lines and a section header's
// rule characters.
func onlyRule(s string) bool {
	return strings.TrimSpace(strings.Trim(s, " \n─-#=")) == ""
}

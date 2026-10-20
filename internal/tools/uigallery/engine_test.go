package main

import (
	"bytes"
	"flag"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The engine's scenarios in play (next/p3-gallery-engine): the real
// engine on the simulated host, through the flow, as the maintainer plays
// them. Nothing here runs a command or writes a file outside the fake.

// changeID is a change id as backup.NewRunID makes it: the clock's
// second, which is the gallery's, a hyphen and a random suffix, which a
// line may break after the hyphen, as a message wraps it, the next line
// indented and maybe styled.
var changeID = regexp.MustCompile(`(\d{8}T\d{6}Z-)((?:\x1b\[[0-9;]*m)*(?:\n[ ]*(?:\x1b\[[0-9;]*m)*)?)[0-9a-f]{8}`)

// sameIDs returns out with every change id's random suffix written
// xxxxxxxx, so that a transcript is the same on every run and wraps as the
// run did, and fails when an id was cut elsewhere, which would leave part
// of it random.
func sameIDs(t *testing.T, out string) string {
	t.Helper()
	whole := len(changeID.FindAllString(out, -1))
	out = changeID.ReplaceAllString(out, "${1}${2}xxxxxxxx")
	if n := strings.Count(out, galleryNow.Format("20060102T150405Z")+"-"); n != whole || strings.Count(out, "xxxxxxxx") != n {
		t.Errorf("%d change ids, %d of them whole", n, whole)
	}
	return out
}

// TestPlayEngineGolden plays each engine scenario as its sequence says,
// on the core set, and pins its transcript: the scenario's note, the real
// plan, the confirmation, the steps, the outcome, the countdown, and what
// the fake recorded; then the scenarios' menu, a refused plan chosen from
// it, and back. Each change id's random suffix is written xxxxxxxx
// (sameIDs). The golden files are testdata/golden/play/engine/<name>.golden.
func TestPlayEngineGolden(t *testing.T) {
	type run struct {
		name  string
		args  []string
		lines []string
	}
	var runs []run
	for _, sc := range scenarios() {
		runs = append(runs, run{sc.name, []string{enginePrefix + "/" + sc.name}, sc.answers})
	}
	runs = append(runs, run{"menu", []string{enginePrefix}, []string{"7", "0"}})
	for _, r := range runs {
		uitest.Golden(t, "play/engine/"+r.name, uitest.Core(), func(env term.Env) string {
			args := append(lookArgs(t, env), r.args...)
			code, out, errs, unread := playGallery(r.lines, args...)
			if code != exitOK || errs != "" || unread != 0 {
				t.Errorf("%s, %v: exit %d, stderr %q, %d lines unread; want exit 0", r.name, args, code, errs, unread)
			}
			return sameIDs(t, out)
		})
	}
}

// playEngine plays the scenario or menu name with the user typing lines,
// in plain mode, and returns the session, so that a test can read each
// simulated host, the transcript and the exit status.
func playEngine(t *testing.T, name string, lines ...string) (*session, string, int) {
	t.Helper()
	var out, errs bytes.Buffer
	ty := uitest.NewTypist(&out, lines...)
	fs := flag.NewFlagSet("uigallery play", flag.ContinueOnError)
	look := lookFlags(fs, true)
	if err := fs.Parse([]string{"-profile", "plain"}); err != nil {
		t.Fatal(err)
	}
	var s *session
	code := startPlay(look, fs, offTerminal, func() *prompt.Reader { return prompt.New(ty, false) }, &out, &errs, func(ss *session) int {
		s = ss
		return ss.play("play "+name, []string{engineNote}, ss.openEngine(name), &errs)
	})
	if errs.Len() != 0 || ty.Unread() != 0 {
		t.Errorf("%s: stderr %q, %d lines unread", name, errs.String(), ty.Unread())
	}
	for _, r := range s.engineRuns {
		if u := r.host.unanswered(); len(u) > 0 {
			t.Errorf("%s: the simulated host was asked to run what it does not model: %q", name, u)
		}
	}
	return s, out.String(), code
}

// dropIn is 00-hardn.conf on h, "" when absent.
func (h *fakeHost) dropIn() string {
	e, _ := h.f.Entry(sshd.HardnDropIn)
	return string(e.Data)
}

// journalled reports whether h has the state file name of revert's.
func (h *fakeHost) journalled(name string) bool {
	_, ok := h.f.Entry(path.Join(revert.DefaultDir, name))
	return ok
}

// TestPlayEngine plays each scenario and checks the simulated host it
// leaves, beside the screen: the change applied and waiting for
// confirmation, with port 22 held beside 2230 and ufw letting 2230
// through; key-only login; ufw active; alice's grant, with no journal
// left; nothing changed outside the run directory for the refused plan
// and the dry run; the failed reload rolled back, under the gallery's
// signal hold, which the engine took once; the confirmation and its
// clean-up, which leave 2230 alone, in sshd -T, ss and ufw. Each scenario's
// steps run in brief 2.3's order: the revert timer armed before the
// firewall, the firewall before the file, the file before the reload.
func TestPlayEngine(t *testing.T) {
	waiting := func(t *testing.T, h *fakeHost) {
		t.Helper()
		if !h.journalled(revert.PendingName) {
			t.Error("no change waiting for confirmation")
		}
	}
	held := func(ufw bool) func(*testing.T, *fakeHost) {
		return func(t *testing.T, h *fakeHost) {
			t.Helper()
			waiting(t, h)
			if d := h.dropIn(); !strings.Contains(d, "Port 22\nPort 2230\n") {
				t.Errorf("00-hardn.conf:\n%s\nwant Port 22 held beside Port 2230", d)
			}
			if !slices.Contains(h.bound, "0.0.0.0:2230") || !slices.Contains(h.bound, "0.0.0.0:22") {
				t.Errorf("sshd binds %v, want 22 and 2230", h.bound)
			}
			if ufw && !slices.Equal(h.allowed, []uint16{22, 2230}) {
				t.Errorf("ufw allows %v, want 22 and 2230", h.allowed)
			}
		}
	}
	// untouched: no command but reads, and nothing written, made, removed
	// or renamed outside the plan's run directory (/run/hardn): not even
	// the journal or a backup.
	untouched := func(t *testing.T, h *fakeHost) {
		t.Helper()
		if cmds, _ := changed(h.f.Calls()); len(cmds) > 0 {
			t.Errorf("the host changed: %q", cmds)
		}
		for _, c := range h.f.Calls() {
			switch c.Op {
			case "write-atomic", "write-in-place", "append", "remove", "removeall", "rename", "mkdir", "chmod", "chown":
				if c.Path != "/run/hardn" && !strings.HasPrefix(c.Path, "/run/hardn/") {
					t.Errorf("changed outside the run directory: %s", c)
				}
			}
		}
	}
	for _, tc := range []struct {
		name   string
		holds  int
		screen []string // what the transcript shows, in order
		check  func(*testing.T, *fakeHost)
		order  []string // commands and files the host changed, in this order
	}{
		{"ssh-port-debian13", 0, []string{"Set Port in /etc/ssh/sshd_config.d/00-hardn.conf", "Operation Successful", "reverts automatically in 3:01", "port 2230", "sudo hardn confirm", "rolled back at 12:03:01 UTC"},
			held(true), []string{"systemd-run", "ufw allow 2230/tcp", sshd.HardnDropIn, "systemctl reload ssh.service"}},
		{"ssh-port-ubuntu2604", 0, []string{"Reload sshd (ssh.socket)", "Operation Successful", "reverts automatically in 3:01"},
			held(true), []string{"systemd-run", "ufw allow 2230/tcp", sshd.HardnDropIn, "systemctl daemon-reload", "systemctl restart ssh.socket"}},
		{"ssh-port-alpine324", 0, []string{"Reload sshd (openrc sshd)", "Operation Successful", "doas hardn confirm", "rolled back at about 12:03:00 UTC"},
			held(false), []string{"/bin/sh -c", sshd.HardnDropIn, "rc-service sshd reload"}},
		{"keyonly-debian13", 0, []string{"Set PasswordAuthentication, KbdInteractiveAuthentication", "Operation Successful", "open a new SSH session and run"},
			func(t *testing.T, h *fakeHost) {
				waiting(t, h)
				if d := h.dropIn(); !strings.Contains(d, "PasswordAuthentication no\nKbdInteractiveAuthentication no\n") {
					t.Errorf("00-hardn.conf:\n%s", d)
				}
			}, []string{"systemd-run", sshd.HardnDropIn, "systemctl reload ssh.service"}},
		{"ufw-enable-debian13", 0, []string{"Firewall: ufw allow 22/tcp; ufw --force enable", "Operation Successful"},
			func(t *testing.T, h *fakeHost) {
				waiting(t, h)
				if h.inactive || !slices.Equal(h.allowed, []uint16{22}) {
					t.Errorf("ufw inactive %t, allows %v; want active, allowing 22", h.inactive, h.allowed)
				}
			}, []string{"systemd-run", "ufw allow 22/tcp", "ufw --force enable"}},
		{"sudoers-grant-debian13", 0, []string{"[y/N, 0 or q]: y", "Done: Let alice use sudo without a password", "Operation Successful"},
			func(t *testing.T, h *fakeHost) {
				if e, ok := h.f.Entry("/etc/sudoers.d/hardn-grant-alice"); !ok || !strings.Contains(string(e.Data), "alice ALL=(ALL) NOPASSWD: ALL") {
					t.Errorf("the grant: %q, %t", e.Data, ok)
				}
				if h.journalled(revert.PendingName) {
					t.Error("a change without lockout risk is left journalled")
				}
			}, []string{"/etc/sudoers.d/hardn-grant-alice"}},
		{"refused-d9-ubuntu2604", 0, []string{"Operation Failed", "never took effect", "(SSH_LISTEN_CONF_NOT_IN_EFFECT)", "To go on: first make sure the firewall", "Nothing was applied."},
			untouched, nil},
		{"failed-reload-debian13", 1, []string{"Failed: Reload sshd (ssh.service) (COMMAND_FAILED)", "Skipped: sshd -T", "Operation Failed",
			"Rolled back: Set Port in", "Rolled back: Firewall: ufw allow 2230/tcp", "Job for ssh.service failed", "Rolled back from the journal"},
			func(t *testing.T, h *fakeHost) {
				if d := h.dropIn(); d != "" || !slices.Equal(h.allowed, []uint16{22}) || slices.Contains(h.bound, "0.0.0.0:2230") {
					t.Errorf("not rolled back: 00-hardn.conf %q, ufw %v, sshd on %v", d, h.allowed, h.bound)
				}
				for _, name := range []string{revert.PendingName, revert.RollbackName} {
					if h.journalled(name) {
						t.Errorf("%s left", name)
					}
				}
			}, []string{"systemd-run", "ufw allow 2230/tcp", sshd.HardnDropIn, "systemctl reload ssh.service",
				"ufw delete allow 2230/tcp", "systemctl reload ssh.service", "systemctl stop hardn-revert-"}},
		{"confirm-cleanup-debian13", 0, []string{"reverts automatically in 3:01", "runs sudo hardn confirm", "confirmed (", "Drop port 22 from",
			"Operation Successful", "Port 22, which change", "ufw's rule for 22/tcp, which hardn added, is deleted"},
			func(t *testing.T, h *fakeHost) {
				if d := h.dropIn(); strings.Contains(d, "Port 22\n") || !strings.Contains(d, "Port 2230\n") {
					t.Errorf("00-hardn.conf:\n%s\nwant Port 2230 alone", d)
				}
				if !slices.Equal(h.allowed, []uint16{2230}) || slices.Contains(h.bound, "0.0.0.0:22") {
					t.Errorf("ufw allows %v, sshd binds %v; want 2230 alone", h.allowed, h.bound)
				}
				if h.journalled(revert.PendingName) || !h.journalled(revert.ConfirmedName) {
					t.Error("the confirmation is not journalled as confirmed")
				}
			}, []string{"ufw allow 22/tcp", "ufw allow 2230/tcp", "ufw --force enable", sshd.HardnDropIn, "systemctl reload ssh.service",
				"systemctl stop hardn-revert-", "ufw delete allow 22/tcp", sshd.HardnDropIn, "systemctl reload ssh.service"}},
		{"best-effort-ubuntu2204", 0, []string{"No actual changes were made to your system.", "Ubuntu 22.04 is a best-effort release", "(PLATFORM_BEST_EFFORT)", "Nothing was applied."},
			untouched, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc, ok := findScenario(tc.name)
			if !ok {
				t.Fatal("no such scenario")
			}
			s, out, code := playEngine(t, enginePrefix+"/"+tc.name, sc.answers...)
			flat := strings.Join(strings.Fields(out), " ")
			at := 0
			for _, w := range tc.screen {
				i := strings.Index(flat[at:], strings.Join(strings.Fields(w), " "))
				if i < 0 {
					t.Errorf("the transcript does not show %q after what came before it:\n%s", w, out)
					break
				}
				at += i
			}
			if code != exitOK || s.holds != tc.holds || len(s.engineRuns) == 0 {
				t.Fatalf("exit %d, the gallery's hold taken %d times (want %d), %d runs", code, s.holds, tc.holds, len(s.engineRuns))
			}
			h := s.engineRuns[0].host
			tc.check(t, h)
			var all []string // each command and file that changed the host, in order
			for _, c := range h.f.Calls() {
				cmds, files := changed([]hostfake.Call{c})
				all = append(append(all, cmds...), files...)
			}
			at = 0
			for _, w := range tc.order {
				i := slices.IndexFunc(all[at:], func(c string) bool { return strings.HasPrefix(c, w) })
				if i < 0 {
					t.Errorf("the host did not change %q after what came before it: %q", w, all)
					break
				}
				at += i + 1
			}
		})
	}
}

// TestPlayEngineEnds checks how an engine scenario ends at its prompts:
// declining with n or 0 applies nothing and returns, q quits, the end of
// the input aborts with 130; a typed confirmation refuses a wrong line and
// reads the next; from the menu, 0 goes back and q quits.
func TestPlayEngineEnds(t *testing.T) {
	port := enginePrefix + "/ssh-port-debian13"
	for _, tc := range []struct {
		name    string
		start   string
		lines   []string
		code    int
		applied bool
	}{
		{"n refused at the typed confirmation, then 0", port, []string{"n", "0"}, exitOK, false},
		{"0 at the typed confirmation", port, []string{"0"}, exitOK, false},
		{"q at the typed confirmation", port, []string{"q"}, exitOK, false},
		{"the end of the input at the typed confirmation", port, nil, prompt.ExitInterrupted, false},
		{"a wrong line, then the hostname", port, []string{"WEB-01", hostName}, exitOK, true},
		{"n at the sudo grant's y/N", enginePrefix + "/sudoers-grant-debian13", []string{"n"}, exitOK, false},
		{"0 at the menu", enginePrefix, []string{"0"}, exitOK, false},
		{"q at the menu", enginePrefix, []string{"q"}, exitOK, false},
		{"a scenario from the menu, then q", enginePrefix, []string{"1", hostName, "q"}, exitOK, true},
		{"the end of the input at the menu", enginePrefix, nil, prompt.ExitInterrupted, false},
	} {
		code, out, errs, unread := playGallery(tc.lines, "-profile", "plain", tc.start)
		_, record, _ := strings.Cut(out, "# simulated system ")
		applied := strings.Contains(record, "Applied: ")
		if code != tc.code || errs != "" || unread != 0 || applied != tc.applied || applied == strings.Contains(record, "Nothing was applied.") {
			t.Errorf("%s: exit %d, stderr %q, %d lines unread, applied %t; want exit %d, applied %t; the record:\n%s",
				tc.name, code, errs, unread, applied, tc.code, tc.applied, record)
		}
	}
}

// TestPlayEngineUsage checks that a scenario that is not one, and -fail or
// -sequence with an engine scenario, stop play with exit status 2 before
// stdin is read.
func TestPlayEngineUsage(t *testing.T) {
	for _, args := range [][]string{
		{enginePrefix + "/nope"},
		{"-fail", "2", enginePrefix + "/ssh-port-debian13"},
		{"-sequence", "typed", enginePrefix},
		{"-sequence", "normal", enginePrefix + "/keyonly-debian13"},
	} {
		var out, errs bytes.Buffer
		read := false
		code := run(append([]string{"play"}, args...), func() *prompt.Reader { read = true; return noInput() }, offTerminal, &out, &errs)
		if code != exitUsage || out.Len() != 0 || errs.Len() == 0 || read {
			t.Errorf("play %v: exit %d, stdout %q, stderr %q, stdin read %t; want exit %d, a message, no output", args, code, out.String(), errs.String(), read, exitUsage)
		}
	}
}

// TestPlayEngineSmoke plays the 2230 change with its confirmation and
// clean-up in every profile, both themes and both glyph sets, at 60 and 80
// columns: exit status 0, and no line wider than the terminal but a
// header, which shows its whole name, and a prompt's line with the answer
// the terminal echoes.
func TestPlayEngineSmoke(t *testing.T) {
	start := enginePrefix + "/confirm-cleanup-debian13"
	for _, p := range profiles {
		for _, th := range []string{"dark", "light"} {
			for _, ascii := range []bool{false, true} {
				for _, w := range []int{60, 80} {
					args := []string{"-profile", p.name, "-theme", th, fmt.Sprintf("-ascii=%t", ascii), fmt.Sprintf("-width=%d", w), start}
					code, out, errs, unread := playGallery([]string{hostName}, args...)
					where := strings.Join(args, " ")
					if code != exitOK || errs != "" || unread != 0 {
						t.Fatalf("%s: exit %d, stderr %q, %d lines unread", where, code, errs, unread)
					}
					ctx := component.NewCtx(newCell(p, th == "dark", !ascii, w).env)
					headers := []string{ansi.Strip(header(ctx, "play "+start)), ansi.Strip(header(ctx, "simulated system"))}
					for _, l := range strings.Split(strings.TrimSuffix(ansi.Strip(out), "\n"), "\n") {
						if slices.Contains(headers, l) || strings.HasSuffix(l, " "+hostName) {
							continue
						}
						if ansi.StringWidth(l) > w {
							t.Errorf("%s: line %q is wider than %d", where, l, w)
						}
					}
					if !strings.Contains(out, "Drop port 22") {
						t.Errorf("%s: no clean-up shown", where)
					}
				}
			}
		}
	}
}

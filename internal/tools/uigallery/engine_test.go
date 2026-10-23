package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
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
	return playWith(t, name, func(s *session) func() error { return s.openEngine(name) }, lines...)
}

// playWith is playEngine for the action open returns.
func playWith(t *testing.T, name string, open func(*session) func() error, lines ...string) (*session, string, int) {
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
		return ss.play("play "+name, []string{engineNote}, open(ss), &errs)
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

// journal is the journal in revert's state file name on h.
func (h *fakeHost) journal(t *testing.T, name string) revert.Journal {
	t.Helper()
	var j revert.Journal
	e, ok := h.f.Entry(path.Join(revert.DefaultDir, name))
	if err := json.Unmarshal(e.Data, &j); !ok || err != nil {
		t.Fatalf("%s: %t, %v", name, ok, err)
	}
	return j
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
		{"ssh-port-debian13", 0, []string{"Add SSH port 2230", "Operation Successful", "reverts automatically in 3:01", "port 2230", "sudo hardn confirm", "rolled back at 12:03:01 UTC"},
			held(true), []string{"systemd-run", "ufw allow 2230/tcp", sshd.HardnDropIn, "systemctl reload ssh.service"}},
		{"ssh-port-ubuntu2604", 0, []string{"Reload sshd (ssh.socket)", "Operation Successful", "reverts automatically in 3:01"},
			held(true), []string{"systemd-run", "ufw allow 2230/tcp", sshd.HardnDropIn, "systemctl daemon-reload", "systemctl restart ssh.socket"}},
		{"ssh-port-alpine324", 0, []string{"Reload sshd (openrc sshd)", "Operation Successful", "doas hardn confirm", "rolled back at about 12:03:00 UTC"},
			held(false), []string{"/bin/sh -c", sshd.HardnDropIn, "rc-service sshd reload"}},
		{"keyonly-debian13", 0, []string{"Disallow password login", "Operation Successful", "open a new SSH session and run"},
			func(t *testing.T, h *fakeHost) {
				waiting(t, h)
				if d := h.dropIn(); !strings.Contains(d, "PasswordAuthentication no\nKbdInteractiveAuthentication no\n") {
					t.Errorf("00-hardn.conf:\n%s", d)
				}
			}, []string{"systemd-run", sshd.HardnDropIn, "systemctl reload ssh.service"}},
		{"ufw-enable-debian13", 0, []string{"ufw: allow 22/tcp; enable", "Operation Successful"},
			func(t *testing.T, h *fakeHost) {
				waiting(t, h)
				if h.inactive || !slices.Equal(h.allowed, []uint16{22}) {
					t.Errorf("ufw inactive %t, allows %v; want active, allowing 22", h.inactive, h.allowed)
				}
			}, []string{"systemd-run", "ufw allow 22/tcp", "ufw --force enable"}},
		{"sudoers-grant-debian13", 0, []string{"[y/N, 0 or q]: y", "Let alice use sudo without a password Done", "Operation Successful"},
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
		{"failed-reload-debian13", 1, []string{"Reload sshd (ssh.service) Failed COMMAND_FAILED", "sshd -T shows ports 22, 2230 Skipped", "Operation Failed",
			"Add SSH port 2230 Rolled back", "ufw: allow 2230/tcp Rolled back", "Job for ssh.service failed", "Rolled back from the journal"},
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
		{"confirm-cleanup-debian13", 0, []string{"reverts automatically in 3:01", "runs sudo hardn confirm", "confirmed (", "Drop SSH port 22",
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
				// The connections open at the apply, which cannot confirm
				// it: alice's and sshd's listener, in /proc's order, not
				// the confirming one.
				if j := h.journal(t, revert.ConfirmedName); j.Applied == nil || !slices.Equal(j.Applied.SSHD, []revert.ProcStart{{PID: applySSHDPID, Start: applySSHDStart}, {PID: listenerPID, Start: 1200}}) {
					t.Errorf("the apply's sshd processes: %+v", j.Applied)
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
			flat := flatText(out)
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

// TestPlayEngineEnds checks how an engine scenario ends at its prompts, a
// typed confirmation and a y/N one: declining (0 at either, n at the y/N)
// applies nothing and returns, q quits, the end of the input aborts with
// 130; a typed confirmation refuses a wrong line, n among them, and reads
// the next; from the menu, 0 goes back and q quits.
func TestPlayEngineEnds(t *testing.T) {
	port, grant := enginePrefix+"/ssh-port-debian13", enginePrefix+"/sudoers-grant-debian13"
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
		{"n at the sudo grant's y/N", grant, []string{"n"}, exitOK, false},
		{"0 at the sudo grant's y/N", grant, []string{"0"}, exitOK, false},
		{"q at the sudo grant's y/N", grant, []string{"q"}, exitOK, false},
		{"the end of the input at the sudo grant's y/N", grant, nil, prompt.ExitInterrupted, false},
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
					if !strings.Contains(out, "Drop SSH port 22") {
						t.Errorf("%s: no clean-up shown", where)
					}
				}
			}
		}
	}
}

// TestRecorderSteps checks how the record splits what the fake recorded by
// the engine's events: the timer armed before the first change whatever
// came before it (here no boot unit's change, as on a host that has it),
// each change's own commands under how it ended, a command that failed
// among them, a failure of the apply as a whole (Index -1, here at its
// mark applied step) with what ran since the last change, and the
// rollback's after the failure; reads and hardn's own state left out.
func TestRecorderSteps(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		// last runs the plan's second change, or what follows the first.
		last func(h *fakeHost, r *recorder, run func(...string))
		want string
	}{
		{"a change fails", func(h *fakeHost, r *recorder, run func(...string)) {
			r.event(engine.Event{Index: 1, State: engine.EventRunning})
			h.f.AddDir("/var/lib/hardn", 0o700, 0, 0)
			if err := h.f.WriteFileAtomic(ctx, "/var/lib/hardn/pending.json", []byte("{}"), 0o600, 0, 0); err != nil {
				t.Fatal(err)
			}
			run("systemctl", "reload", "ssh.service")
			r.event(engine.Event{Index: 1, State: engine.EventFailed, Step: engine.StepApply})
		}, "Failed: Reload sshd: systemctl reload ssh.service []"},
		{"the apply fails as a whole", func(h *fakeHost, r *recorder, run func(...string)) {
			run("systemctl", "reload", "ssh.service")
			r.event(engine.Event{Index: -1, State: engine.EventFailed, Step: engine.StepMarkApplied})
		}, "Failed: the apply, at its mark applied step: systemctl reload ssh.service []"},
	} {
		h := newHost()
		h.failReloads = 1
		r := &recorder{host: h, f: h.f, title: "a plan"}
		run := func(argv ...string) { _, _ = h.f.Run(ctx, argv[0], argv[1:]...) }
		r.begin()
		run("ss", "-Htln")
		run("systemd-run", "--unit=hardn-revert-x", "--on-active=180s", galleryHardn, "rollback", "x")
		r.event(engine.Event{Index: 0, State: engine.EventRunning})
		run("ufw", "allow", "2230/tcp")
		r.event(engine.Event{Index: 0, State: engine.EventDone})
		tc.last(h, r, run)
		run("ufw", "delete", "allow", "2230/tcp")
		r.event(engine.Event{Index: 0, State: engine.EventRolledBack})
		r.end()
		r.setPlan(&engine.Plan{Changes: []engine.Change{{Summary: "Allow 2230"}, {Summary: "Reload sshd"}}})
		var got []string
		for _, s := range r.steps() {
			got = append(got, s.Title+": "+strings.Join(s.Commands, "; ")+" ["+s.Target+"]")
		}
		want := []string{
			"Armed the revert timer: systemd-run --unit=hardn-revert-x --on-active=180s /usr/local/bin/hardn rollback x []",
			"Applied: Allow 2230: ufw allow 2230/tcp []",
			tc.want,
			"Rolled back from the journal: ufw delete allow 2230/tcp []",
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: the record:\n%s\nwant\n%s", tc.name, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
	if !arms([]hostfake.Call{{Op: "start-detached", Argv: []string{"/bin/sh", "-c", "sleep"}}}) || arms([]hostfake.Call{{Op: "run", Argv: []string{"ss", "-Htln"}}}) {
		t.Error("arms: OpenRC's sleeper is the timer, ss is not")
	}
}

// TestScenarioHosts checks that each scenario's simulated host is the
// platform its note names, as the engine establishes it (detect.Detect,
// engine.Options.Detect's default): every fact established, with no error,
// and the ones the steps branch on as listed. Ubuntu 26.04's sudo is
// sudo-rs, as the VM run found it (docs/safety/vmtest.md §4.9).
func TestScenarioHosts(t *testing.T) {
	const (
		debian13   = "debian 13 supported; systemd; ssh.service, generator no; sudo yes, sudo; doas no; proxmox no; container no"
		ubuntu2604 = "ubuntu 26.04 supported; systemd; ssh.socket, generator yes; sudo yes, sudo-rs; doas no; proxmox no; container no"
	)
	want := map[string]string{
		"ssh-port-debian13":   debian13,
		"ssh-port-ubuntu2604": ubuntu2604,
		"ssh-port-alpine324":  "alpine 3.24 supported; openrc; openrc sshd, generator unknown; sudo no, unknown; doas yes; proxmox no; container no",
		"keyonly-debian13":    debian13, "ufw-enable-debian13": debian13, "sudoers-grant-debian13": debian13,
		"refused-d9-ubuntu2604": ubuntu2604, "failed-reload-debian13": debian13, "confirm-cleanup-debian13": debian13,
		"best-effort-ubuntu2204": "ubuntu 22.04 best effort; systemd; ssh.service, generator no; sudo yes, sudo; doas no; proxmox no; container no",
	}
	for _, sc := range scenarios() {
		h := sc.host()
		f, err := detect.Detect(context.Background(), h.f)
		e := f.Elevation
		got := fmt.Sprintf("%s %s %s; %s; %s, generator %s; sudo %s, %s; doas %s; proxmox %s; container %s", f.Platform.Distro, f.Platform.Version,
			f.Platform.Support, f.Init.Kind, f.SSH.Mode, f.SSH.Generator, e.Sudo.Present, e.SudoFlavor, e.Doas.Present, f.Proxmox.Installed, f.Container.Detected)
		if err = errors.Join(err, f.OS.Err, f.Platform.Err, f.Init.Err, f.SSH.Err, f.Proxmox.Err, f.Container.Err, e.Err, e.Sudo.Err, e.Doas.Err); err != nil || got != want[sc.name] {
			t.Errorf("%s: detect finds %q, %v; want %q", sc.name, got, err, want[sc.name])
		}
		if u := h.unanswered(); len(u) > 0 {
			t.Errorf("%s: detect asked what the model does not answer: %q", sc.name, u)
		}
	}
}

// TestFakeHost checks the model's answers that no scenario's golden shows:
// a command the platform does not ship is not found, as on the host, and
// is not kept; one the model does not answer fails as hostfake's
// unscripted commands do, and is kept for the scenario's test; ufw's words
// while inactive; sshd -T's PermitRootLogin, the platform's.
func TestFakeHost(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		h    func() *fakeHost
		argv []string
		out  string
		err  error // errors.Is
		kept bool  // in unanswered
	}{
		{"systemctl on OpenRC", openRCHost, []string{"systemctl", "is-active", "ssh.service"}, "", host.ErrNotFound, false},
		{"sudo with doas", openRCHost, []string{"sudo", "--version"}, "", host.ErrNotFound, false},
		{"dpkg-query on Alpine", openRCHost, []string{"dpkg-query", "--show", "pve-manager"}, "", host.ErrNotFound, false},
		{"rc-service on systemd", newHost, []string{"rc-service", "sshd", "reload"}, "", host.ErrNotFound, false},
		{"not modelled", newHost, []string{"apt-get", "install", "-y", "fail2ban"}, "", hostfake.ErrUnscripted, true},
		{"sudo, Debian 13", newHost, []string{"sudo", "--version"}, "Sudo version 1.9.16p2\n", nil, false},
		{"sudo-rs, Ubuntu 26.04", socketHost, []string{"sudo", "--version"}, "sudo-rs 0.2.13\n", nil, false},
		{"ufw delete, inactive", inactiveUFW, []string{"ufw", "delete", "allow", "22/tcp"}, "Rules updated\nRules updated (v6)\n", nil, false},
		{"ufw delete, active", newHost, []string{"ufw", "delete", "allow", "22/tcp"}, "Rule deleted\nRule deleted (v6)\n", nil, false},
	} {
		h := tc.h()
		res, err := h.f.Run(ctx, tc.argv[0], tc.argv[1:]...)
		kept := slices.Contains(h.unanswered(), strings.Join(tc.argv, " "))
		if string(res.Stdout) != tc.out || !errors.Is(err, tc.err) || (err == nil) != (tc.err == nil) || kept != tc.kept {
			t.Errorf("%s: %q, %v, kept %t; want %q, %v, kept %t", tc.name, res.Stdout, err, kept, tc.out, tc.err, tc.kept)
		}
	}
	for _, tc := range []struct {
		h    func() *fakeHost
		want string
	}{{newHost, "permitrootlogin without-password\n"}, {socketHost, "permitrootlogin prohibit-password\n"}} {
		h := tc.h()
		if res, err := h.f.Run(ctx, "sshd", "-T", "-f", sshd.MainConfig); err != nil || !strings.Contains(string(res.Stdout), tc.want) {
			t.Errorf("sshd -T: %v\n%s\nwant %q", err, res.Stdout, tc.want)
		}
	}
}

// TestConfirmFromNewConnection plays the 2230 change with ufw's enable,
// then hardn confirm, on hosts the scenario does not show: a connection
// that was open when the change was applied, which the claim refuses
// (revert.ErrOldConnection), so nothing is confirmed or cleaned up; and a
// clean-up whose reload fails, rolled back under the gallery's hold, the
// confirmation standing and port 22 left, as the report says.
func TestConfirmFromNewConnection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		build   func() *fakeHost
		between func(*fakeHost)
		holds   int
		screen  []string // what hardn confirm's part of the transcript shows, in order
		check   func(*testing.T, *fakeHost)
	}{
		{"an old connection", func() *fakeHost {
			h := inactiveUFW()
			h.process(confirmSSHDPID, h.session, listenerPID, confirmSSHDStart) // open before the apply
			return h
		}, func(*fakeHost) {}, 0, []string{"runs sudo hardn confirm.", "connection 192.0.2.10:50123 was open when the change was applied"},
			func(t *testing.T, h *fakeHost) {
				if !h.journalled(revert.PendingName) || h.journalled(revert.ConfirmedName) || !strings.Contains(h.dropIn(), "Port 22\n") {
					t.Error("not left waiting for confirmation, with port 22 held")
				}
			}},
		{"the clean-up's reload fails", inactiveUFW, func(h *fakeHost) { h.failReloads = 1 }, 1,
			[]string{"confirmed (", "Reload sshd (ssh.service) Failed", "Operation Failed", "Rolled back", "Port 22, which change", "is still open", "To finish the clean-up"},
			func(t *testing.T, h *fakeHost) {
				if h.journalled(revert.PendingName) || !h.journalled(revert.ConfirmedName) || !strings.Contains(h.dropIn(), "Port 22\nPort 2230\n") {
					t.Errorf("want the change confirmed and port 22 left; 00-hardn.conf:\n%s", h.dropIn())
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc, _ := findScenario("confirm-cleanup-debian13")
			var h *fakeHost
			sc.host = func() *fakeHost { h = tc.build(); return h }
			sc.confirm = false
			s, out, code := playWith(t, sc.name, func(s *session) func() error {
				return func() error {
					if err := s.runScenario(sc); err != nil {
						return err
					}
					tc.between(h)
					return s.confirmFromNewConnection(h)
				}
			}, sc.answers...)
			_, confirmed, _ := strings.Cut(out, "hardn confirm, from a new connection")
			flat, at := flatText(confirmed), 0
			for _, w := range tc.screen {
				i := strings.Index(flat[at:], w)
				if i < 0 {
					t.Errorf("hardn confirm does not show %q after what came before it:\n%s", w, confirmed)
					break
				}
				at += i
			}
			if code != exitOK || s.holds != tc.holds {
				t.Errorf("exit %d, the gallery's hold taken %d times; want %d", code, s.holds, tc.holds)
			}
			tc.check(t, h)
		})
	}
}

// TestSudoersGrant checks the sudo grant's control where its scenario does
// not reach: no elevation tool established, sudoers that cannot be read,
// and a grant alice already has, which plans nothing.
func TestSudoersGrant(t *testing.T) {
	ctx := context.Background()
	granted, _, _ := playEngine(t, enginePrefix+"/sudoers-grant-debian13", "y")
	noSudoers := newHost()
	if err := noSudoers.f.Remove(ctx, "/etc/sudoers"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		h     *fakeHost
		facts func(*fakeHost) detect.Facts
		err   bool
	}{
		{"no elevation tool established", newHost(), func(*fakeHost) detect.Facts { return detect.Facts{} }, true},
		{"no /etc/sudoers", noSudoers, nil, true},
		{"alice has the grant", granted.engineRuns[0].host, nil, false},
	} {
		f := detect.Facts{}
		if tc.facts != nil {
			f = tc.facts(tc.h)
		} else if d, err := detect.Detect(ctx, tc.h.f); err == nil {
			f = d
		}
		changes, err := sudoersGrant().Plan(ctx, tc.h.f, engine.Input{Facts: f})
		if (err != nil) != tc.err || len(changes) != 0 {
			t.Errorf("%s: %d changes, %v; want none, error %t", tc.name, len(changes), err, tc.err)
		}
	}
}

// TestPlayEngineWriteFails checks that a failed write in an engine
// scenario, of its note, the flow, what follows the flow or hardn
// confirm's part, ends play with exit status 1 and a message, as a failed
// write of the record does (TestPlayWriteFails).
func TestPlayEngineWriteFails(t *testing.T) {
	port, confirmed := enginePrefix+"/ssh-port-debian13", enginePrefix+"/confirm-cleanup-debian13"
	for _, tc := range []struct{ start, at, errs string }{
		{port, "web-01.example.net runs Debian 13", "uigallery: play: broken pipe\n"},
		{port, "Operation Successful", "uigallery: play: flow: write: broken pipe\n"},
		{port, "reverts automatically", "uigallery: play: broken pipe\n"},
		{confirmed, "alice opens a new SSH connection", "uigallery: play: broken pipe\n"},
	} {
		var out, errs bytes.Buffer
		w := &breaks{w: &out, at: tc.at}
		ty := uitest.NewTypist(&out, hostName)
		code := run([]string{"play", "-profile", "plain", tc.start}, func() *prompt.Reader { return prompt.New(ty, false) }, offTerminal, w, &errs)
		if code != exitError || errs.String() != tc.errs || !w.broke {
			t.Errorf("%s, at %q: exit %d, stderr %q, the write failed %t; want exit %d, stderr %q", tc.start, tc.at, code, errs.String(), w.broke, exitError, tc.errs)
		}
	}
}

// flatText is s as one line of words: each line without a callout's bar
// ("# " in plain mode) and its white space collapsed, so that a sentence
// matches however it is wrapped, as prose or in a callout's body (P-42
// R5), and a step's row as its title, then its state.
func flatText(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(l, "# ")
	}
	return strings.Join(strings.Fields(strings.Join(lines, "\n")), " ")
}

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestUpdatesGolden pins each listing mock on the core set and under
// LANG=C: the screen on every host, and the refusals.
func TestUpdatesGolden(t *testing.T) { goldenListing(t, updListing) }

// TestPlayUpdatesGolden plays each flow end to end and pins the
// transcript: each direction planned, asked y/N and applied on the real
// engine; refusals before asking; the read-only item; plan only; the hosts'
// menu; the main menu's 6.
func TestPlayUpdatesGolden(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		lines []string
		cells []uitest.Cell
	}{
		{"updates-enable-installer", []string{"updates/debian-installer"}, []string{"1", "y", "0"}, append(uitest.Core(), asciiColour)},
		{"updates-enable-off", []string{"updates/debian-off"}, []string{"1", "y", "0"}, nil},
		{"updates-enable-lists", []string{"updates/lists-stale"}, []string{"1", "y", "0"}, nil},
		{"updates-enable-alpine", []string{"updates/alpine323"}, []string{"1", "y", "0"}, append(uitest.Core(), asciiColour)},
		{"updates-enable-legacy", []string{"updates/legacy-alpine"}, []string{"1", "y", "0"}, nil},
		{"updates-disable-debian", []string{"updates/by-hardn-debian"}, []string{"1", "y", "0"}, append(uitest.Core(), asciiColour)},
		{"updates-disable-alpine", []string{"updates/by-hardn-alpine"}, []string{"1", "y", "0"}, nil},
		{"updates-crond-shared", []string{"updates/crond-shared"}, []string{"1", "y", "0"}, nil},
		{"updates-decline", []string{"updates/debian-off"}, []string{"1", "n", "0"}, nil},
		{"updates-edited", []string{"updates/edited"}, []string{"1", "0"}, nil},
		{"updates-timer-off", []string{"updates/timer-off"}, []string{"1", "0"}, nil},
		{"updates-not-read", []string{"updates/not-read"}, []string{"1", "0"}, nil},
		{"updates-read-only", []string{"updates/debian13"}, []string{"1", "0"}, nil},
		{"updates-plan-only", []string{"-sequence", "dry-run", "updates/debian-installer"}, []string{"1", "0"}, nil},
		{"updates-hosts", []string{"updates"}, []string{"9", "0", "0"}, nil},
		{"updates-main-menu", nil, []string{"6", "0", "0"}, nil},
	}
	for _, tt := range tests {
		cells := tt.cells
		if cells == nil {
			cells = uitest.Core()
		}
		uitest.Golden(t, "play/"+tt.name, cells, func(env term.Env) string {
			args := append(lookArgs(t, env), tt.args...)
			code, out, errs, unread := playGallery(tt.lines, args...)
			if code != exitOK || errs != "" || unread != 0 {
				t.Errorf("%s, %v: exit %d, stderr %q, %d lines unread", tt.name, args, code, errs, unread)
			}
			return sessionIDs(t, out)
		})
	}
}

// TestUpdatesHosts holds each host's row to the real check, and its item
// to the rule: Enable while off, Disable while on by hardn, read-only
// while on not by hardn and on Proxmox VE; every call one the model
// answers.
func TestUpdatesHosts(t *testing.T) {
	type want struct{ detail, item string }
	const ro = "read-only"
	wants := map[string]want{
		"debian13":         {"unattended-upgrades", ro},
		"ubuntu2604":       {"unattended-upgrades", ro},
		"alpine323":        {"no apk-cron", updEnable},
		"debian-installer": {"no unattended-upgrades", updEnable},
		"debian-off":       {"upgrades off", updEnable},
		"lists-stale":      {"lists not refreshed", updEnable},
		"timer-off":        {"upgrade timer off", updEnable},
		"by-hardn-debian":  {"unattended-upgrades", updDisable},
		"by-hardn-alpine":  {"apk-cron", updDisable},
		"edited":           {"unattended-upgrades", updDisable},
		"crond-shared":     {"apk-cron", updDisable},
		"legacy-alpine":    {"no apk-cron", updEnable},
		"pve9":             {"no unattended-upgrades", ro},
		"not-read":         {"can't read", updEnable},
	}
	if len(wants) != len(updHosts()) {
		t.Fatalf("%d hosts, %d wanted", len(updHosts()), len(wants))
	}
	ctx := context.Background()
	for _, uh := range updHosts() {
		h := uh.host()
		r, err := readUpdates(ctx, h.sys())
		if err != nil {
			t.Fatalf("%s: %v", uh.name, err)
		}
		w := wants[uh.name]
		if len(r.status.Rows) != 1 || r.status.Rows[0].Detail != w.detail || r.check.Detail != w.detail {
			t.Errorf("%s: rows %+v, check %q; want %q", uh.name, r.status.Rows, r.check.Detail, w.detail)
		}
		it := r.item()
		got := it.Label
		if it.Disabled {
			got = ro
		}
		if got != w.item {
			t.Errorf("%s: item %q, want %q", uh.name, got, w.item)
		}
		if u := h.unanswered(); len(u) > 0 {
			t.Errorf("%s: unanswered %q", uh.name, u)
		}
	}
}

// TestUpdatesRefusals: each refusal of updRefusals refuses before asking,
// but on on a host hardn turned on, an empty plan.
func TestUpdatesRefusals(t *testing.T) {
	ctx := context.Background()
	for _, rf := range updRefusals() {
		words, err := updRefusalWords(ctx, rf)
		empty := rf.host == "by-hardn-debian"
		if err != nil || (words == "") != empty {
			t.Errorf("%v: %q, %v", rf, words, err)
		}
	}
}

// TestUpdatesOffChanged: off refuses each step changed since hardn made it
// (hardn's file, apk-cron's script, a record hardn can't read, a file of
// hardn's name holding other bytes), and where the package's files changed
// removes it in place of the purge, keeping them and saying so.
func TestUpdatesOffChanged(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name string
		host func() *fakeHost
		want string
	}{
		{"hardn's file", updEdited, hardnAptFile + " changed since"},
		{"apk's script", func() *fakeHost {
			h := updByHardnAlpine()
			h.f.AddFile(alpine.Script, []byte("#!/bin/sh\n"), 0o755, 0, 0)
			return h
		}, alpine.Script + " changed since"},
		{"the record", func() *fakeHost {
			h := updByHardnDebian()
			h.f.AddFile(updRecordPath, []byte("{\"schemaVersion\": 2}\n"), 0o600, 0, 0)
			return h
		}, "can't read its record"},
		{"no record, other bytes", func() *fakeHost {
			h := updDebianOff()
			h.f.AddFile(hardnAptFile, []byte("APT::Periodic::Unattended-Upgrade \"1\";\n"), 0o644, 0, 0)
			return h
		}, hardnAptFile + " changed since"},
	} {
		h := tt.host()
		r, err := readUpdates(ctx, h.sys())
		if err != nil {
			t.Fatal(err)
		}
		if words := (&updRunner{h: h}).refusalOf(ctx, r, true); !strings.Contains(words, tt.want) {
			t.Errorf("%s: %q, want %q", tt.name, words, tt.want)
		}
	}
	h := updByHardnDebian()
	h.f.AddFile(autoUpgrades, []byte(autoOff), 0o644, 0, 0)
	r, err := readUpdates(ctx, h.sys())
	if err != nil {
		t.Fatal(err)
	}
	cs, kept, err := planOff(ctx, h.system(), engine.Input{Facts: r.f})
	if err != nil || len(cs) != 3 || cs[1].Commands[0] != "apt-get remove -y "+uuPackage || len(kept) != 1 {
		t.Errorf("20auto-upgrades changed: %v, kept %v, %v", changeIDs(cs), kept, err)
	}
}

// TestUpdatesOwnFile: with no record, hardn's own file holding hardn's
// bytes is on by hardn, and off deletes it and nothing else.
func TestUpdatesOwnFile(t *testing.T) {
	ctx := context.Background()
	r, h, err := updReadNamed(ctx, "own-file")
	if err != nil {
		t.Fatal(err)
	}
	if it := r.item(); it.Label != updDisable || it.Disabled {
		t.Errorf("item %+v, want Disable", it)
	}
	cs, _, err := planOff(ctx, h.system(), engine.Input{Facts: r.f})
	if err != nil || len(cs) != 1 || cs[0].Target != hardnAptFile {
		t.Errorf("off: %v, %v; want the file alone", changeIDs(cs), err)
	}
}

// TestUpdatesPlanOnlyWritesNothing: under plan only every item opens and
// stops at its plan, nothing applied.
func TestUpdatesPlanOnlyWritesNothing(t *testing.T) {
	for _, host := range []string{"updates/debian-installer", "updates/debian-off", "updates/alpine323", "updates/legacy-alpine", "updates/by-hardn-debian", "updates/by-hardn-alpine"} {
		code, out, errs, unread := playGallery([]string{"1", "0"}, "-profile", "plain", "-sequence", "dry-run", host)
		_, record, _ := strings.Cut(out, "# simulated system ")
		if code != exitOK || errs != "" || unread != 0 || !strings.Contains(out, "No actual changes were made") || strings.Contains(record, "Applied: ") {
			t.Errorf("%s: exit %d, stderr %q, %d unread; the record:\n%s", host, code, errs, unread, record)
		}
	}
}

// TestUpdatesHeights: every host's screen is as many lines at 80x24 as at
// 80x40, Unicode and ASCII, the prompt on its last, no line over 80 cells.
func TestUpdatesHeights(t *testing.T) {
	ctx := context.Background()
	for _, uh := range updHosts() {
		r, _, err := updReadNamed(ctx, uh.name)
		if err != nil {
			t.Fatal(err)
		}
		var n []int
		for _, unicode := range []bool{true, false} {
			for _, rows := range []int{24, 40} {
				env := term.Fixed(80, colorprofile.TrueColor, unicode, true)
				env.Height = rows
				lines := strings.Split(ansi.Strip(r.screen().Render(component.NewCtx(env))), "\n")
				for _, l := range lines {
					if w := ansi.StringWidth(l); w > 80 {
						t.Errorf("%s: %d cells: %q", uh.name, w, l)
					}
				}
				if !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
					t.Errorf("%s: the last line %q, want the prompt", uh.name, lines[len(lines)-1])
				}
				n = append(n, len(lines))
			}
		}
		t.Logf("%s: %v", uh.name, n)
		if n[0] != n[1] || n[0] != n[2] || n[0] != n[3] {
			t.Errorf("%s: lines %v, want the same at 24 and 40 rows", uh.name, n)
		}
	}
}

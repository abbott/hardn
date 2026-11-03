package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	usersctl "github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// usrTestKey is a synthetic key the tests paste.
var usrTestKey = synthKey(5, "carol@laptop")

// TestUsersGolden pins each listing mock on the core set and under LANG=C.
func TestUsersGolden(t *testing.T) { goldenListing(t, usrListing) }

// TestPlayUsersGolden plays each flow end to end and pins the transcript.
func TestPlayUsersGolden(t *testing.T) {
	// A pasted key's echo is wider than the screen, so the transcripts
	// answer no to Add SSH key; TestUsersLockoutRisk and
	// TestUsersCreateAlpine paste one.
	create := []string{"2", "carol", "y", "n", "n", "y", "0"}
	tests := []struct {
		name  string
		args  []string
		lines []string
		cells []uitest.Cell
	}{
		{"users-remove-session", []string{"users/hardn"}, []string{"1", "1", "1", "0", "0", "0"}, append(uitest.Core(), asciiColour)},
		{"users-remove-cloud-init", []string{"users/debian13"}, []string{"1", "1", "1", "0", "0", "0"}, nil},
		{"users-remove-group", []string{"users/debian13"}, []string{"1", "2", "1", "0", "0", "0"}, nil},
		{"users-remove-legacy", []string{"users/legacy"}, []string{"1", "1", "1", "0", "0", "0"}, nil},
		{"users-remove-alice", []string{"users/second-admin"}, []string{"1", "2", "1", hostName, "0", "0", "0"}, append(uitest.Core(), asciiColour)},
		{"users-require-no-password", []string{"users/debian13"}, []string{"1", "1", "2", "0", "0", "0"}, nil},
		{"users-require-alice", []string{"users/second-admin"}, []string{"1", "2", "2", hostName, "0", "0", "0"}, nil},
		{"users-stop-asking", []string{"users/sudo-password"}, []string{"1", "1", "2", "y", "0", "0", "0"}, nil},
		{"users-grant-no-sudo", []string{"users/no-sudo"}, []string{"1", "1", "1", "0", "0", "0"}, nil},
		{"users-grant-doas", []string{"users/alpine-no-rule"}, []string{"1", "1", "1", "y", "0", "0", "0"}, nil},
		{"users-not-read", []string{"users/not-read"}, []string{"1", "1", "1", "0", "0", "0"}, nil},
		{"users-add-key", []string{"users/debian13"}, []string{"1", "2", "3", "1", "nope", "0", "0", "0", "0", "0"}, nil},
		{"users-remove-key", []string{"users/debian13"}, []string{"1", "1", "3", "2", "2", hostName, "0", "0", "0", "0"}, nil},
		{"users-last-key", []string{"users/one-key"}, []string{"1", "1", "3", "2", "0", "0", "0", "0"}, nil},
		{"users-create", []string{"users/debian13"}, []string{"2", "George", "george", "carol", "y", "y", "n", "n", "y", "0"}, append(uitest.Core(), asciiColour)},
		{"users-create-alpine", []string{"users/alpine323"}, create, nil},
		{"users-create-pve", []string{"users/pve9"}, []string{"1", "2", "carol", "y", "n", "n", "y", "0"}, nil},
		{"users-plan-only-create", []string{"-sequence", "dry-run", "users/debian13"}, []string{"2", "carol", "y", "n", "n", "0"}, nil},
		{"users-plan-only-remove", []string{"-sequence", "dry-run", "users/second-admin"}, []string{"1", "2", "1", "0", "0", "0"}, nil},
		{"users-hosts", []string{"users"}, []string{"3", "0", "0"}, nil},
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

// usrHostNamed is the host named name.
func usrHostNamed(t *testing.T, name string) usrHost {
	t.Helper()
	uh, ok := findUsrHost(name)
	if !ok {
		t.Fatalf("no users host %q", name)
	}
	return uh
}

// readUsrHost reads the host named name, fresh.
func readUsrHost(t *testing.T, name string) (usrReading, *fakeHost) {
	t.Helper()
	r, h, err := usrReadNamed(context.Background(), name)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return r, h
}

// playUsr plays the mock on h, plain, the user typing lines, a dry run with
// dryRun.
func playUsr(t *testing.T, uh usrHost, h *fakeHost, dryRun bool, lines ...string) (*session, string, int) {
	t.Helper()
	return playWith(t, usrPrefix+"/"+uh.name, func(s *session) func() error {
		if dryRun {
			s.sequence = "dry-run"
		}
		return func() error { return s.usersScreen(context.Background(), uh, h) }
	}, lines...)
}

// TestUsersHosts holds each host's rows to the real readers: the User row
// to users.Admin.Check's state, each user's admin rights to
// users.ReadElevation's GrantsOf, hardn's grant to sudoers.PolicyOf, and
// what each host shows to its note.
func TestUsersHosts(t *testing.T) {
	type want struct {
		admin  detect.Tri
		by     string
		nopass bool
		pw     usrPassword
		keys   int
		hardn  sudoers.Grant
	}
	cloud := sudoers.DropInDir + "/90-cloud-init-users"
	wants := map[string]map[string]want{
		"debian13":       {"george": {detect.Yes, cloud, true, pwNone, 2, sudoers.GrantNone}, "alice": {detect.Yes, "group sudo", false, pwSet, 0, sudoers.GrantNone}},
		"ubuntu2604":     {"george": {detect.Yes, cloud, true, pwNone, 2, sudoers.GrantNone}, "alice": {detect.Yes, "group sudo", false, pwSet, 0, sudoers.GrantNone}},
		"alpine323":      {"alpine": {detect.Yes, "group wheel", true, pwNone, 0, sudoers.GrantNone}, "george": {detect.Yes, "group wheel", true, pwNone, 2, sudoers.GrantNone}},
		"legacy":         {"george": {detect.Yes, sudoers.DropInDir + "/george", true, pwSet, 2, sudoers.GrantNone}},
		"hardn":          {"george": {detect.Yes, "hardn", true, pwNone, 2, sudoers.GrantNoPassword}},
		"second-admin":   {"george": {detect.Yes, cloud, true, pwNone, 2, sudoers.GrantNone}, "alice": {detect.Yes, "hardn", true, pwSet, 1, sudoers.GrantNoPassword}},
		"one-key":        {"george": {detect.Yes, cloud, true, pwNone, 1, sudoers.GrantNone}, "alice": {detect.Yes, "group sudo", false, pwSet, 0, sudoers.GrantNone}},
		"sudo-password":  {"george": {detect.Yes, "hardn", false, pwSet, 2, sudoers.GrantPassword}},
		"alpine-no-rule": {"george": {detect.No, "", false, pwNone, 2, sudoers.GrantNone}},
		"alpine-locked":  {"alpine": {detect.Yes, "group wheel", true, pwNone, 0, sudoers.GrantNone}, "george": {detect.Yes, "group wheel", true, pwNone, 2, sudoers.GrantNone}, "alice": {detect.Yes, "group wheel", true, pwLocked, 1, sudoers.GrantNone}},
		"yml-missing":    {"george": {detect.Yes, cloud, true, pwNone, 2, sudoers.GrantNone}, "alice": {detect.Yes, "group sudo", false, pwSet, 0, sudoers.GrantNone}},
		"not-read":       {"george": {detect.Unknown, "", false, pwNone, 2, sudoers.GrantNone}, "alice": {detect.Unknown, "", false, pwSet, 0, sudoers.GrantNone}},
		"pve9":           {},
		"no-sudo":        {"george": {detect.No, "", false, pwNone, 2, sudoers.GrantNone}},
	}
	if len(wants) != len(usrHosts()) {
		t.Fatalf("%d hosts, %d wanted", len(usrHosts()), len(wants))
	}
	ctx := context.Background()
	for _, uh := range usrHosts() {
		r, h := readUsrHost(t, uh.name)
		sys := h.sys()
		f, _ := detect.Detect(ctx, sys)
		st, err := usersctl.Admin{}.Check(ctx, sys, f)
		if row := r.status.Rows; len(row) != 1 || row[0].Label != "User" || (err == nil && row[0].Detail != st.Detail) {
			t.Errorf("%s: the User row %+v, the check %v %q", uh.name, row, st.State, st.Detail)
		}
		db, _ := accounts.Read(ctx, sys)
		elev := usersctl.ReadElevation(ctx, sys, f)
		if len(r.users) != len(wants[uh.name]) {
			t.Errorf("%s: %d users, want %d", uh.name, len(r.users), len(wants[uh.name]))
		}
		for _, u := range r.users {
			w, ok := wants[uh.name][u.Name]
			if !ok {
				t.Errorf("%s: user %s not wanted", uh.name, u.Name)
				continue
			}
			real := detect.No
			for _, g := range elev.GrantsOf(db, u.User) {
				if g.Grants == detect.Yes || (g.Grants == detect.Unknown && real == detect.No) {
					real = g.Grants
				}
			}
			if u.admin != real || u.admin != w.admin || u.by != w.by || u.nopass != w.nopass || u.pw != w.pw || len(u.keys) != w.keys || u.policy.Grant != w.hardn {
				t.Errorf("%s, %s: admin %v (the reader's %v), by %q, nopass %v, pw %v, %d keys, hardn %v; want %+v",
					uh.name, u.Name, u.admin, real, u.by, u.nopass, u.pw, len(u.keys), u.policy.Grant, w)
			}
		}
		if got := h.unanswered(); len(got) > 0 {
			t.Errorf("%s: commands the model does not answer: %q", uh.name, got)
		}
	}
}

// TestUsersRefusals: every lockout path and refusal of usrRefusals says
// why before asking, played: its words shown, no plan, no question.
func TestUsersRefusals(t *testing.T) {
	ctx := context.Background()
	for _, rf := range usrRefusals() {
		words, err := refusalOf(ctx, rf)
		if err != nil || words == "" {
			t.Errorf("%v: %q, %v", rf, words, err)
			continue
		}
		r, _ := readUsrHost(t, rf.host)
		i := 0
		for j, u := range r.users {
			if u.Name == rf.user {
				i = j + 1
			}
		}
		key := map[string]string{"remove": "1", "grant": "1", "require": "2"}[rf.item]
		lines := []string{"1", fmt.Sprint(i), key, "0", "0", "0"}
		if rf.item == "remove-key" {
			lines = []string{"1", fmt.Sprint(i), "3", "2", "0", "0", "0", "0"}
		}
		uh := usrHostNamed(t, rf.host)
		h := uh.host()
		n := len(h.f.Calls())
		_, out, code := playUsr(t, uh, h, false, lines...)
		text := flatText(out)
		if code != exitOK || !strings.Contains(text, flatText(words)) || strings.Contains(text, "[y/N") || strings.Contains(text, typeTheHostname) {
			t.Errorf("%v: exit %d, not refused before asking:\n%s", rf, code, out)
		}
		if cmds, files := changed(h.f.Calls()[n:]); len(cmds) > 0 || len(files) > 0 {
			t.Errorf("%v: changed %q, %q", rf, cmds, files)
		}
	}
}

// TestUsersStillGranted: where hardn's grant and another rule both make a
// user an admin, Remove refuses, naming the other rule, and never plans
// the grant away as though the user lost sudo.
func TestUsersStillGranted(t *testing.T) {
	ctx := context.Background()
	h := usrDebian13().hardnGrant("alice", sudoers.GrantPassword)
	r, err := readUsers(ctx, h.sys(), map[string]string{"SUDO_USER": "george"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := r.user("alice")
	if u.by != "hardn" || u.policy.Grant != sudoers.GrantPassword {
		t.Fatalf("alice: by %q, hardn %v", u.by, u.policy.Grant)
	}
	still := stillAdmin(ctx, h.sys(), r, u)
	words := r.removeRefusal(u, still)
	if still != "group sudo" || !strings.Contains(words, "the sudo group") {
		t.Errorf("still %q, refusal %q", still, words)
	}
}

// TestUsersLockoutRisk: the changes that can lock a user out are typed
// with the hostname, and confirmed or reverted: removing alice's grant,
// asking her password, removing a key of the session user's, and anyone's
// last key; the others are y/N.
func TestUsersLockoutRisk(t *testing.T) {
	for _, tc := range []struct {
		host  string
		lines []string
		typed bool
	}{
		{"second-admin", []string{"1", "2", "1", hostName, "0", "0", "0"}, true},
		{"second-admin", []string{"1", "2", "2", hostName, "0", "0", "0"}, true},
		{"debian13", []string{"1", "1", "3", "2", "1", hostName, "0", "0", "0", "0"}, true},
		{"second-admin", []string{"1", "2", "3", "2", "1", hostName, "0", "0", "0", "0"}, true},
		{"sudo-password", []string{"1", "1", "2", "y", "0", "0", "0"}, false},
		{"debian13", []string{"1", "2", "3", "1", usrTestKey, "y", "0", "0", "0", "0"}, false},
		{"alpine-no-rule", []string{"1", "1", "1", "y", "0", "0", "0"}, false},
	} {
		uh := usrHostNamed(t, tc.host)
		h := uh.host()
		_, out, code := playUsr(t, uh, h, false, tc.lines...)
		text := flatText(out)
		typed := strings.Contains(text, typeTheHostname)
		if code != exitOK || typed != tc.typed || !strings.Contains(text, "Operation Successful") {
			t.Errorf("%s %v: exit %d, typed %v, want %v:\n%s", tc.host, tc.lines, code, typed, tc.typed, out)
		}
		if typed && !strings.Contains(text, "hardn confirm") {
			t.Errorf("%s %v: a typed plan without confirm-or-revert", tc.host, tc.lines)
		}
	}
}

// TestUsersRemoveKey: a key removed is gone from the file, the others
// kept, and the review counts what is left.
func TestUsersRemoveKey(t *testing.T) {
	uh := usrHostNamed(t, "debian13")
	h := uh.host()
	r, _ := readUsrHost(t, "debian13")
	u, _ := r.user("george")
	playUsr(t, uh, h, false, "1", "1", "3", "2", "1", hostName, "0", "0", "0", "0")
	data, err := h.f.ReadFile(context.Background(), "/home/george/.ssh/authorized_keys")
	if err != nil || strings.Contains(string(data), u.keys[0].line) || !strings.Contains(string(data), u.keys[1].line) {
		t.Errorf("authorized_keys after removing the first key: %q, %v", data, err)
	}
	fi, _ := h.f.Stat(context.Background(), "/home/george/.ssh/authorized_keys")
	if fi == nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("authorized_keys mode %v", fi)
	}
}

// TestUsersCreateAlpine: a user created on Alpine, whose sshd has no PAM,
// gets "*", not useradd's "!", so that its key logs it in; on Debian
// useradd's own "!", which PAM lets a key past.
func TestUsersCreateAlpine(t *testing.T) {
	for host, want := range map[string]string{"alpine323": "carol:*:", "debian13": "carol:!:"} {
		uh := usrHostNamed(t, host)
		h := uh.host()
		playUsr(t, uh, h, false, "2", "carol", "y", "n", "y", usrTestKey, "y", "0")
		shadow, _ := h.f.ReadFile(context.Background(), accounts.ShadowFile)
		keys, err := h.f.ReadFile(context.Background(), "/home/carol/.ssh/authorized_keys")
		if !strings.Contains(string(shadow), want) || err != nil || strings.TrimSpace(string(keys)) != usrTestKey {
			t.Errorf("%s: shadow %q, keys %q, %v", host, shadow, keys, err)
		}
		r, _ := readUsers(context.Background(), h.sys(), uh.env)
		u, ok := r.user("carol")
		if !ok || u.admin != detect.Yes || u.by != "hardn" || u.pw == pwLocked || len(u.keys) != 1 {
			t.Errorf("%s: carol read back %+v", host, u)
		}
	}
}

// TestUsersPlanOnlyWritesNothing: under plan only every change opens as
// live and stops at its plan, nothing written.
func TestUsersPlanOnlyWritesNothing(t *testing.T) {
	flows := map[string][][]string{
		"debian13":       {{"2", "carol", "y", "n", "y", usrTestKey, "0"}, {"1", "2", "3", "1", usrTestKey, "0", "0", "0", "0"}, {"1", "1", "3", "2", "1", "0", "0", "0", "0"}},
		"second-admin":   {{"1", "2", "1", "0", "0", "0"}, {"1", "2", "2", "0", "0", "0"}},
		"sudo-password":  {{"1", "1", "2", "0", "0", "0"}},
		"alpine-no-rule": {{"1", "1", "1", "0", "0", "0"}},
		"alpine323":      {{"2", "carol", "y", "n", "y", usrTestKey, "0"}},
		"pve9":           {{"2", "carol", "n", "y", usrTestKey, "0"}},
	}
	for host, all := range flows {
		uh := usrHostNamed(t, host)
		for _, lines := range all {
			h := uh.host()
			n := len(h.f.Calls())
			_, out, code := playUsr(t, uh, h, true, lines...)
			if code != exitOK || !strings.Contains(flatText(out), "No actual changes were made") {
				t.Errorf("%s %v: exit %d, did not stop at the plan:\n%s", host, lines, code, out)
			}
			if cmds, files := changed(h.f.Calls()[n:]); len(cmds) > 0 || len(files) > 0 {
				t.Errorf("%s %v: plan only changed %q, %q", host, lines, cmds, files)
			}
		}
	}
}

// TestUsersKeys: the key check takes one OpenSSH public key and refuses
// anything else; the fingerprint is ssh-keygen's form.
func TestUsersKeys(t *testing.T) {
	k, err := parseKey(usrTestKey, false)
	if err != nil || !strings.HasPrefix(k.fp, "SHA256:") || len(k.fp) != 50 || k.comment != "carol@laptop" {
		t.Errorf("%q: %+v, %v", usrTestKey, k, err)
	}
	for _, bad := range []string{"", "ssh-ed25519", "ssh-ed25519 !!!", "ssh-rsa " + strings.Fields(usrTestKey)[1], "ssh-dss AAAAB3NzaC1kc3M=", `from="192.0.2.1" ` + usrTestKey} {
		if _, err := parseKey(bad, false); err == nil {
			t.Errorf("%q taken", bad)
		}
	}
	if _, err := parseKey(`from="192.0.2.1" `+usrTestKey, true); err != nil {
		t.Errorf("a line with options not read: %v", err)
	}
}

// TestUsersNames: the names useradd refuses, refused; a name the host has,
// refused.
func TestUsersNames(t *testing.T) {
	r, h := readUsrHost(t, "debian13")
	db, _ := accounts.Read(context.Background(), h.sys())
	for name, ok := range map[string]bool{"carol": true, "_svc": true, "web-01": true, "Carol": false, "1carol": false, "car ol": false,
		"george": false, "sudo": false, strings.Repeat("a", 33): false, "permit": false, "": false} {
		if got := r.checkName(db, name) == nil; got != ok {
			t.Errorf("%q: taken %v, want %v", name, got, ok)
		}
	}
}

// heights logs each screen's height, to set TestUsersHeights' table.
var heights = flag.Bool("users-heights", false, "log the User Management mock's heights")

// TestUsersHeights: each screen at 80x24 and 80x40, in Unicode and ASCII,
// is as many lines at either height, the prompt on its last. Those over 24
// rows are the page's §4.
func TestUsersHeights(t *testing.T) {
	want := map[string]int{"debian13": 26, "ubuntu2604": 26, "alpine323": 26, "legacy": 19, "hardn": 19, "second-admin": 26, "one-key": 26,
		"sudo-password": 19, "alpine-no-rule": 18, "alpine-locked": 33, "yml-missing": 26, "not-read": 24, "pve9": 16, "no-sudo": 18,
		"select": 10, "manage": 20, "keys": 19}
	draws := map[string]func(component.Ctx) string{}
	for _, uh := range usrHosts() {
		r, _ := readUsrHost(t, uh.name)
		draws[uh.name] = func(c component.Ctx) string { return r.mainMenu().Render(c) }
	}
	r, _ := readUsrHost(t, "debian13")
	u, _ := r.user("george")
	draws["select"] = func(c component.Ctx) string { return r.selectMenu().Render(c) }
	draws["manage"] = func(c component.Ctx) string { return r.manageMenu(u).Render(c) }
	draws["keys"] = func(c component.Ctx) string { return r.keysMenu(u).Render(c) }
	for name, draw := range draws {
		for _, rows := range []int{24, 40} {
			for _, unicode := range []bool{true, false} {
				env := term.Fixed(80, colorprofile.TrueColor, unicode, true)
				env.Height = rows
				lines := strings.Split(ansi.Strip(draw(component.NewCtx(env))), "\n")
				if *heights {
					t.Logf("%q: %d,", name, len(lines))
				}
				if len(lines) != want[name] || !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
					t.Errorf("%s at 80x%d, unicode %v: %d lines, the last %q; want %d, the prompt", name, rows, unicode, len(lines), lines[len(lines)-1], want[name])
				}
			}
		}
	}
}

// TestUsersUsage: the listing and play refuse what they do not take, with
// status 2 and nothing on stdout; the listing lists with no input; the main
// menu's 1 leads to debian13's.
func TestUsersUsage(t *testing.T) {
	for _, args := range [][]string{
		{usrPrefix, "nope"},
		{"play", "users/nope"},
		{"play", "-fail", "1", "users/debian13"},
		{"play", "-sequence", "failure", "users/debian13"},
	} {
		var out, errs bytes.Buffer
		if code := run(args, nil, offTerminal, &out, &errs); code != exitUsage || out.Len() > 0 || errs.Len() == 0 {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), errs.String())
		}
	}
	var out bytes.Buffer
	if code := run([]string{usrPrefix, "-list"}, nil, offTerminal, &out, &bytes.Buffer{}); code != exitOK || len(strings.Fields(out.String())) != len(usrMocks()) {
		t.Errorf("-list: exit %d, %q", code, out.String())
	}
	if usrLinks[mainMenuMock+"/1"] != usrMainHost {
		t.Errorf("the main menu's 1 leads to %q", usrLinks[mainMenuMock+"/1"])
	}
}

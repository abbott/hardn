package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/engine"
	usersctl "github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/screens/screenkit"
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
		{"users-create-pve", []string{"users/pve9"}, []string{"2", "carol", "y", "n", "n", "y", "0"}, nil},
		{"users-create-no-admin", []string{"users/debian13"}, []string{"2", "carol", "n", "n", "y", "0"}, nil},
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
		"root-login":     {"george": {detect.Yes, "hardn", true, pwNone, 2, sudoers.GrantNoPassword}},
		"debian-nopam":   {"george": {detect.Yes, cloud, true, pwLocked, 2, sudoers.GrantNone}, "alice": {detect.Yes, "group sudo", false, pwSet, 0, sudoers.GrantNone}},
		"alpine-stray":   {"alpine": {detect.Yes, "group wheel", true, pwNone, 0, sudoers.GrantNone}, "george": {detect.Yes, "group wheel", true, pwNone, 1, sudoers.GrantNone}},
		"group-home":     {"george": {detect.Yes, cloud, true, pwNone, 2, sudoers.GrantNone}, "alice": {detect.Yes, "group sudo", false, pwSet, 0, sudoers.GrantNone}},
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
		if len(r.Users) != len(wants[uh.name]) {
			t.Errorf("%s: %d users, want %d", uh.name, len(r.Users), len(wants[uh.name]))
		}
		for _, u := range r.Users {
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
			if by := usrBy(u.Decided); u.Admin != real || u.Admin != w.admin || by != w.by || u.NoPassword != w.nopass || usrPW(u) != w.pw || len(u.Keys) != w.keys || u.Policy.Grant != w.hardn {
				t.Errorf("%s, %s: admin %v (the reader's %v), by %q, nopass %v, pw %v, %d keys, hardn %v; want %+v",
					uh.name, u.Name, u.Admin, real, by, u.NoPassword, usrPW(u), len(u.Keys), u.Policy.Grant, w)
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
		for j, u := range r.Users {
			if u.Name == rf.user {
				i = j + 1
			}
		}
		key := map[string]string{"remove": "1", "grant": "1", "require": "2"}[rf.item]
		lines := []string{"1", fmt.Sprint(i), key, "0", "0", "0"}
		switch rf.item {
		case "remove-key":
			lines = []string{"1", fmt.Sprint(i), "3", "2", "0", "0", "0", "0"}
		case "add-key":
			lines = []string{"1", fmt.Sprint(i), "3", "1", "0", "0", "0", "0"}
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

// TestUsersStillGranted: where hardn's grant and other rules make a user
// an admin, Remove refuses, naming every rule still granting
// (users.StillAdmin); a rule asking a password the user lacks is not
// counted; a rule the reader marks Unknown refuses, naming it.
func TestUsersStillGranted(t *testing.T) {
	ctx := context.Background()
	still := func(h *fakeHost, name string) (string, usersctl.Still) {
		t.Helper()
		r, err := readUsers(ctx, h.sys(), map[string]string{"SUDO_USER": "george"})
		if err != nil {
			t.Fatal(err)
		}
		if u, _ := r.user(name); u.Policy.Grant == sudoers.GrantNone {
			t.Fatalf("%s: no grant of hardn's", name)
		}
		return r.refusal(ctx, h.sys(), usersctl.Admin{Action: usersctl.ActionRemove, User: name}), usersctl.StillAdmin(ctx, h.sys(), r.Reading, name)
	}
	names := func(st usersctl.Still) []string {
		var out []string
		for _, by := range st.By {
			out = append(out, usrBy(by))
		}
		return out
	}

	h := usrDebian13().hardnGrant("alice", sudoers.GrantPassword)
	h.f.AddFile(sudoers.DropInDir+"/50-alice", []byte("alice ALL=(ALL) NOPASSWD: ALL\n"), 0o440, 0, 0)
	words, st := still(h, "alice")
	if !slices.Equal(names(st), []string{sudoers.DropInDir + "/50-alice", "group sudo"}) || words != fmt.Sprintf(usrWordsStill, "alice", "/etc/sudoers.d/50-alice and the sudo group") {
		t.Errorf("alice: still %+v, refusal %q", st, words)
	}

	h = usrDebian13().hardnGrant("george", sudoers.GrantNoPassword)
	_ = h.f.Remove(ctx, sudoers.DropInDir+"/90-cloud-init-users")
	if words, st := still(h, "george"); len(st.By) > 0 || st.Unknown != "" || words != fmt.Sprintf(usrWordsSession, "george", "sudo") {
		t.Errorf("george, in sudo with no password: still %+v, refusal %q", st, words)
	}

	h = usrSecondAdmin()
	h.f.AddFile(sudoers.DropInDir+"/50-alice", []byte("alice web-01=(ALL) ALL\n"), 0o440, 0, 0)
	if words, st := still(h, "alice"); st.Unknown != sudoers.DropInDir+"/50-alice:1" || words != fmt.Sprintf(usrWordsStillUnknown, "alice", "/etc/sudoers.d/50-alice:1") {
		t.Errorf("alice, a rule not read: still %+v, refusal %q", st, words)
	}
}

// TestUsersRows: a grant asking no password is a cost for an account with
// none, a failure for one with a password; Manage User is offered only
// with a user to manage, its detail the host's tool.
func TestUsersRows(t *testing.T) {
	for _, tc := range []struct {
		host, user string
		state      component.ValueState
		note       string
	}{{"debian13", "george", component.ValueWarn, usrNoPassToAsk}, {"legacy", "george", component.ValueBad, "No password"}} {
		r, _ := readUsrHost(t, tc.host)
		u, _ := r.user(tc.user)
		if row := r.rows(u)[0]; row.State != tc.state || row.Note != tc.note {
			t.Errorf("%s, %s: %+v", tc.host, tc.user, row)
		}
	}
	for host, want := range map[string]string{"debian13": "Configure sudo, SSH keys", "alpine323": "Configure doas, SSH keys", "pve9": ""} {
		r, _ := readUsrHost(t, host)
		items := r.mainMenu().Menu.Items
		got := ""
		if items[0].Key == "1" {
			got = items[0].Description
		}
		if got != want || items[len(items)-1].Key != "2" {
			t.Errorf("%s: items %+v", host, items)
		}
	}
}

// TestUsersDoasFile: a doas grant is hardn's zz-hardn-<stem>.conf, which
// sorts after wheel.conf (decision 2 (a) (i)), so the plan has no finding
// of a file that overrides it.
func TestUsersDoasFile(t *testing.T) {
	ctx := context.Background()
	for _, host := range []string{"alpine323", "alpine-no-rule"} {
		r, h := readUsrHost(t, host)
		cs, err := r.admin(usersctl.Admin{Action: usersctl.ActionGrant, User: "alpine"}).Plan(ctx, h.sys(), engine.Input{Facts: r.f})
		if host == "alpine-no-rule" {
			cs, err = r.admin(usersctl.Admin{Action: usersctl.ActionGrant, User: "george"}).Plan(ctx, h.sys(), engine.Input{Facts: r.f})
		}
		if err != nil || len(cs) != 1 || !strings.HasPrefix(cs[0].Target, sudoers.DoasDir+"/zz-hardn-") {
			t.Fatalf("%s: %v, %+v", host, err, cs)
		}
		for _, fd := range cs[0].Findings {
			if strings.HasSuffix(fd.Source, "wheel.conf") {
				t.Errorf("%s: %+v", host, fd)
			}
		}
	}
}

// TestUsersKeyFiles: keys are counted only in the files sshd -T says it
// reads; with ~/.ssh/authorized_keys not among them every key change and
// Create's key refuse before asking; with sshd -T not read, too.
func TestUsersKeyFiles(t *testing.T) {
	ctx := context.Background()
	r, _ := readUsrHost(t, "alpine-stray")
	if !slices.Equal(r.KeyEntries, []string{".ssh/authorized_keys"}) || r.KeysRefusal() != nil {
		t.Errorf("alpine-stray: key files %q, %v", r.KeyEntries, r.KeysRefusal())
	}
	uh := usrHostNamed(t, "debian13")
	h := uh.host()
	h.f.AddFile("/etc/ssh/sshd_config.d/50-keys.conf", []byte("AuthorizedKeysFile /etc/ssh/keys/%u\n"), 0o644, 0, 0)
	r, err := readUsers(ctx, h.sys(), uh.env)
	u, _ := r.user("george")
	words := usrWords(r.KeysRefusal())
	if err != nil || words != fmt.Sprintf(usrWordsKeyFiles, "/etc/ssh/keys/%u") || len(u.Keys) != 0 ||
		r.refusal(ctx, h.sys(), usersctl.Admin{Action: usersctl.ActionRemoveKey, User: "george"}) != words ||
		r.refusal(ctx, h.sys(), usersctl.Admin{Action: usersctl.ActionAddKey, User: "george"}) != words {
		t.Errorf("AuthorizedKeysFile /etc/ssh/keys/%%u: %q, %d keys, %v", words, len(u.Keys), err)
	}
	n := len(h.f.Calls())
	_, out, _ := playUsr(t, uh, h, false, "2", "carol", "n", "y", "n", "n", "0")
	if !strings.Contains(flatText(out), flatText(words)) {
		t.Errorf("Create's key not refused:\n%s", out)
	}
	if cmds, files := changed(h.f.Calls()[n:]); len(cmds) > 0 || len(files) > 0 {
		t.Errorf("Create declined changed %q, %q", cmds, files)
	}

	h = usrHostNamed(t, "debian13").host()
	_ = h.f.Remove(ctx, "/etc/ssh/sshd_config")
	r, _ = readUsers(ctx, h.sys(), uh.env)
	if r.KeyErr == nil || usrWords(r.KeysRefusal()) != fmt.Sprintf(usrWordsKeyFilesRead, screenkit.FullStop(r.KeyErr.Error())) {
		t.Errorf("sshd -T not read: %v, %q", r.KeyErr, usrWords(r.KeysRefusal()))
	}
}

// TestUsersLockoutRisk: the changes that can lock a user out are typed
// with the hostname, and confirmed or reverted: removing alice's grant,
// asking alice's password, removing a key of the session user's, and anyone's
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
		{"root-login", []string{"1", "1", "3", "2", "1", hostName, "0", "0", "0", "0"}, true},
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
	if err != nil || strings.Contains(string(data), u.Keys[0].Line) || !strings.Contains(string(data), u.Keys[1].Line) {
		t.Errorf("authorized_keys after removing the first key: %q, %v", data, err)
	}
	fi, _ := h.f.Stat(context.Background(), "/home/george/.ssh/authorized_keys")
	if fi == nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("authorized_keys mode %v", fi)
	}
}

// TestUsersCreatePassword: a user created where sshd's PAM is not on
// (Alpine; Debian with UsePAM no) gets "*", not useradd's "!", so that its
// key logs it in; where PAM is on, useradd's own "!", which PAM lets a key
// past. The home is stated and created, its mode the host's; the fake's
// useradd makes none without --create-home.
func TestUsersCreatePassword(t *testing.T) {
	ctx := context.Background()
	for host, want := range map[string]struct {
		shadow string
		home   fs.FileMode
	}{"alpine323": {"carol:*:", 0o755}, "debian13": {"carol:!:", 0o700}, "debian-nopam": {"carol:*:", 0o700}} {
		uh := usrHostNamed(t, host)
		h := uh.host()
		playUsr(t, uh, h, false, "2", "carol", "y", "n", "y", usrTestKey, "y", "0")
		shadow, _ := h.f.ReadFile(ctx, accounts.ShadowFile)
		keys, err := h.f.ReadFile(ctx, "/home/carol/.ssh/authorized_keys")
		if !strings.Contains(string(shadow), want.shadow) || err != nil || strings.TrimSpace(string(keys)) != usrTestKey {
			t.Errorf("%s: shadow %q, keys %q, %v", host, shadow, keys, err)
		}
		ran := false
		for _, argv := range h.f.Runs() {
			ran = ran || argv[0] == "useradd" && strings.Contains(strings.Join(argv, " "), "--home-dir /home/carol --create-home")
		}
		if fi, err := h.f.Stat(ctx, "/home/carol"); !ran || err != nil || fi.Mode().Perm() != want.home {
			t.Errorf("%s: useradd stating its home %v, the home %v, %v", host, ran, fi, err)
		}
		r, _ := readUsers(ctx, h.sys(), uh.env)
		u, ok := r.user("carol")
		if !ok || u.Admin != detect.Yes || usrBy(u.Decided) != "hardn" || usrPW(u) == pwLocked || len(u.Keys) != 1 {
			t.Errorf("%s: carol read back %+v", host, u)
		}
	}
	h := usrDebian13()
	h.f.Handler([]string{"useradd", "--shell", "/bin/bash", "dave"}, nil)
	if _, err := h.f.Stat(ctx, "/home/dave"); err == nil {
		t.Errorf("useradd without --create-home made a home")
	}
}

// TestUsersCreateNoAdmin: Create answered n to admin rights writes no
// elevation rule and joins no group, and the user reads back with none.
func TestUsersCreateNoAdmin(t *testing.T) {
	uh := usrHostNamed(t, "debian13")
	h := uh.host()
	n := len(h.f.Calls())
	_, out, code := playUsr(t, uh, h, false, "2", "carol", "n", "n", "y", "0")
	if code != exitOK || !strings.Contains(flatText(out), "Operation Successful") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	cmds, files := changed(h.f.Calls()[n:])
	for _, f := range files {
		if strings.HasPrefix(f, sudoers.DropInDir+"/") || strings.HasPrefix(f, sudoers.DoasDir+"/") {
			t.Errorf("wrote %s", f)
		}
	}
	for _, c := range cmds {
		if strings.HasPrefix(c, "usermod") || strings.HasPrefix(c, "gpasswd") {
			t.Errorf("ran %s", c)
		}
	}
	r, _ := readUsers(context.Background(), h.sys(), uh.env)
	if u, ok := r.user("carol"); !ok || u.Admin != detect.No || u.Policy.Grant != sudoers.GrantNone {
		t.Errorf("carol read back %+v", u)
	}
}

// TestUsersAlpineLocked: alpine-locked's alice is built as usersOn builds
// every user, so the fake's account and home agree with alice's rows.
func TestUsersAlpineLocked(t *testing.T) {
	_, h := readUsrHost(t, "alpine-locked")
	ctx := context.Background()
	u, err := h.f.LookupUser(ctx, "alice")
	fi, serr := h.f.Stat(ctx, "/home/alice")
	if err != nil || u.UID != 1002 || serr != nil || fi.Mode().Perm() != 0o755 {
		t.Errorf("alice: %+v, %v; home %v, %v", u, err, fi, serr)
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

// TestUsersKeys: the key check, the control's, takes one OpenSSH public key
// and refuses anything else in the decided words; the fingerprint is
// ssh-keygen's form.
func TestUsersKeys(t *testing.T) {
	k, err := usersctl.ParseKey(usrTestKey)
	if err != nil || !strings.HasPrefix(k.Fingerprint, "SHA256:") || len(k.Fingerprint) != 50 || k.Comment != "carol@laptop" {
		t.Errorf("%q: %+v, %v", usrTestKey, k, err)
	}
	for _, bad := range []string{"", "ssh-ed25519", "ssh-ed25519 !!!", "ssh-rsa " + strings.Fields(usrTestKey)[1], "ssh-dss AAAAB3NzaC1kc3M=", `from="192.0.2.1" ` + usrTestKey} {
		if _, err := usersctl.ParseKey(bad); usrWords(err) != usrWordsKey {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

// TestUsersNames: the names useradd refuses, refused; a name the host has,
// refused, in the decided words.
func TestUsersNames(t *testing.T) {
	ctx := context.Background()
	r, h := readUsrHost(t, "debian13")
	for name, ok := range map[string]bool{"carol": true, "_svc": true, "web-01": true, "Carol": false, "1carol": false, "car ol": false,
		"george": false, "sudo": false, strings.Repeat("a", 33): false, "permit": false, "": false} {
		if got := r.CheckName(ctx, h.sys(), name) == nil; got != ok {
			t.Errorf("%q: taken %v, want %v", name, got, ok)
		}
	}
	if w := usrWords(r.CheckName(ctx, h.sys(), "Carol")); w != usrWordsName {
		t.Errorf("Carol: %q", w)
	}
	if w := usrWords(r.CheckName(ctx, h.sys(), "george")); w != fmt.Sprintf(usrWordsTaken, "george") {
		t.Errorf("george: %q", w)
	}
	if w := usrWords(r.CheckNew(ctx, h.sys(), usersctl.New{Name: "carol", Admin: true, Password: true})); w != fmt.Sprintf(usrWordsNewNoPW, "sudo") {
		t.Errorf("a password: %q", w)
	}
}

// TestUsersWords: each refusal of usrRefusals, and hardn's file edited,
// in the decided words (gallery-users.md §6).
func TestUsersWords(t *testing.T) {
	ctx := context.Background()
	cloud := sudoers.DropInDir + "/90-cloud-init-users"
	notRead := fmt.Sprintf(usrWordsNotRead, "sudo", "its files don't check.", "visudo -c")
	want := map[usrRefusal]string{
		{"remove", "hardn", "george"}:            fmt.Sprintf(usrWordsSession, "george", "sudo"),
		{"remove", "debian13", "george"}:         fmt.Sprintf(usrWordsNotHardn, "george", cloud),
		{"remove", "debian13", "alice"}:          fmt.Sprintf(usrWordsGroup, "alice", "sudo", "alice", "alice", "sudo"),
		{"remove", "legacy", "george"}:           fmt.Sprintf(usrWordsNotHardn, "george", sudoers.DropInDir+"/george"),
		{"remove", "alpine323", "george"}:        fmt.Sprintf(usrWordsGroup, "george", "wheel", "george", "george", "wheel"),
		{"remove", "not-read", "george"}:         notRead,
		{"require", "debian13", "george"}:        fmt.Sprintf(usrWordsNoPassword, "george", "sudo", "george"),
		{"require", "alpine323", "george"}:       fmt.Sprintf(usrWordsNoPassword, "george", "doas", "george"),
		{"remove-key", "one-key", "george"}:      fmt.Sprintf(usrWordsLastKey, "george", "george"),
		{"grant", "no-sudo", "george"}:           usrWordsNoTool,
		{"grant", "not-read", "alice"}:           notRead,
		{"remove", "root-login", "george"}:       fmt.Sprintf(usrWordsLastAdmin, "george"),
		{"remove-key", "alpine-stray", "george"}: fmt.Sprintf(usrWordsLastKey, "george", "george"),
		{"add-key", "group-home", "alice"}:       fmt.Sprintf(usrWordsStrict, "alice", "/home/alice is writable by its group (0775)"),
	}
	for _, rf := range usrRefusals() {
		if got, err := refusalOf(ctx, rf); err != nil || got != want[rf] {
			t.Errorf("%v: %q, %v\nwant %q", rf, got, err, want[rf])
		}
	}
	r, h := readUsrHost(t, "debian13")
	cs, err := r.admin(usersctl.Admin{Action: usersctl.ActionCreate, New: usersctl.New{Name: "carol"}}).Plan(ctx, h.sys(), engine.Input{Facts: r.f})
	if err != nil || len(cs) != 1 || len(cs[0].Findings) != 1 || cs[0].Findings[0].Detail+"." != fmt.Sprintf(usrWordsNoKeyNotice, "carol") {
		t.Errorf("no key: %+v, %v", cs, err)
	}
	h = usrHardn()
	n, _ := sudoers.FileNames(sudoers.ToolSudo, "george")
	h.f.AddFile(n.Grant, []byte(sudoers.Header+"george ALL=(ALL) ALL # mine\n"), 0o440, 0, 0)
	r, err = readUsers(ctx, h.sys(), map[string]string{"SUDO_USER": "george"})
	if got := r.refusal(ctx, h.sys(), usersctl.Admin{Action: usersctl.ActionRemove, User: "george"}); err != nil || got != fmt.Sprintf(usrWordsEdited, n.Grant) {
		t.Errorf("edited: %q, %v", got, err)
	}
}

// heights logs each screen's height, to set TestUsersHeights' table.
var heights = flag.Bool("users-heights", false, "log the User Management mock's heights")

// TestUsersHeights: each screen at 80x24 and 80x40, in Unicode and ASCII,
// is as many lines at either height, the prompt on its last. Those over 24
// rows are the page's §4.
func TestUsersHeights(t *testing.T) {
	want := map[string]int{"debian13": 26, "ubuntu2604": 26, "alpine323": 26, "legacy": 19, "hardn": 19, "second-admin": 26, "one-key": 26,
		"sudo-password": 19, "alpine-no-rule": 18, "alpine-locked": 33, "yml-missing": 26, "not-read": 24, "pve9": 15, "no-sudo": 18,
		"root-login": 19, "debian-nopam": 26, "alpine-stray": 26, "group-home": 26,
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

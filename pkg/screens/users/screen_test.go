package users_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	usrscreen "github.com/abbott/hardn/pkg/screens/users"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The flow's words a test looks for: the typed confirmation's refusal, plan
// only's end, an apply's.
const (
	cantConfirm = "Can't confirm with the hostname:"
	planned     = "No actual changes were made"
	applied     = "Operation Successful"
	typeIt      = "Type the hostname"
)

// The decided words (gallery-users.md §6), as the gallery's tests pinned
// them (TestUsersWords), with the control's words for the rest.
const (
	wordsNoPassword   = "%s has no password, so %s couldn't ask for one. Set one with passwd %s, then try again."
	wordsNotHardn     = "%s is an admin through %s, which hardn didn't write, so hardn can't remove it. Change it there."
	wordsGroup        = "%s is an admin through the %s group, which hardn leaves alone. Take %s out of it with gpasswd -d %s %s."
	wordsStill        = "%s would stay an admin through %s, which hardn didn't write. Change it there, then try again."
	wordsStillUnknown = "hardn can't tell whether %s would stay an admin: it can't read %s. Check it, then try again."
	wordsSession      = "You're running hardn through %s's %s. Removing it would leave no way to become root. Grant another user first."
	wordsLastAdmin    = "hardn can't tell who you're logged in as, and %s is the last admin. Grant another user first."
	wordsKeyFiles     = "sshd doesn't read ~/.ssh/authorized_keys here (AuthorizedKeysFile %s), so hardn can't change keys."
	wordsKeyFilesRead = "hardn can't read which files sshd takes keys from: %s"
	wordsStrict       = "sshd would ignore a key for %s: %s. With StrictModes it takes keys only where the home and ~/.ssh are the user's own and no one else can write to them."
	wordsLastKey      = "This is %s's last key, and you're logged in as %s. Add another key first."
	wordsDupKey       = "%s already has this key."
	wordsKeyGone      = "That key isn't in %s any more. Nothing changed."
	wordsElsewhere    = "%s's key is also in %s, which sshd reads too. Remove it there, then try again."
	wordsNotRead      = "hardn can't read %s's rules: %s Check them with %s, then try again."
	wordsNoTool       = "sudo isn't installed, so hardn can't grant admin rights. Install sudo, then try again."
	wordsName         = "Use lower-case letters, digits, - and _, starting with a letter or _, up to 32."
	wordsTaken        = "%s already exists."
	wordsKey          = "Paste one OpenSSH public key, such as ssh-ed25519 AAAA… name."
	wordsNotSet       = "%s can't use %s until a password is set: run passwd %s"
	wordsNoKeyNotice  = "Without a key, %s can't log in: there's no password."
	wordsEdited       = "%s holds a rule hardn didn't write. Change it there, then try again."
	wordsLocked       = "sshd refuses %s's keys: the account is locked (!) and sshd doesn't use PAM"
	wordsNoPassToAsk  = "No password to ask for"
)

// carolKey is a synthetic key the tests paste.
var carolKey = synthKey(5, "carol@laptop")

// readHost reads h as the screen does (users.Read).
func readHost(t *testing.T, h *usrHost) users.Reading {
	t.Helper()
	r, err := users.Read(context.Background(), h, h.facts, h.getenv)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// userOf is the user named name in r.
func userOf(t *testing.T, r users.Reading, name string) (users.User, int) {
	t.Helper()
	for i, u := range r.Users {
		if u.Name == name {
			return u, i + 1
		}
	}
	t.Fatalf("no user %s", name)
	return users.User{}, 0
}

// TestHosts: every host of the gallery's draws, live and under plan only,
// writing nothing and making no engine; each user's rows are the control's
// reading (as the gallery's TestUsersHosts held them to the real readers),
// the User row Check's.
func TestHosts(t *testing.T) {
	type want struct {
		admin  detect.Tri
		by     string
		nopass bool
		pw     string
		keys   int
		hardn  sudoers.Grant
	}
	cloud := sudoers.DropInDir + "/90-cloud-init-users"
	wants := map[string]map[string]want{
		"debian13":       {"george": {detect.Yes, cloud, true, "None", 2, sudoers.GrantNone}, "alice": {detect.Yes, "group sudo", false, "Set", 0, sudoers.GrantNone}},
		"ubuntu2604":     {"george": {detect.Yes, cloud, true, "None", 2, sudoers.GrantNone}, "alice": {detect.Yes, "group sudo", false, "Set", 0, sudoers.GrantNone}},
		"alpine323":      {"alpine": {detect.Yes, "group wheel", true, "None", 0, sudoers.GrantNone}, "george": {detect.Yes, "group wheel", true, "None", 2, sudoers.GrantNone}},
		"legacy":         {"george": {detect.Yes, sudoers.DropInDir + "/george", true, "Set", 2, sudoers.GrantNone}},
		"hardn":          {"george": {detect.Yes, "hardn", true, "None", 2, sudoers.GrantNoPassword}},
		"second-admin":   {"george": {detect.Yes, cloud, true, "None", 2, sudoers.GrantNone}, "alice": {detect.Yes, "hardn", true, "Set", 1, sudoers.GrantNoPassword}},
		"one-key":        {"george": {detect.Yes, cloud, true, "None", 1, sudoers.GrantNone}, "alice": {detect.Yes, "group sudo", false, "Set", 0, sudoers.GrantNone}},
		"sudo-password":  {"george": {detect.Yes, "hardn", false, "Set", 2, sudoers.GrantPassword}},
		"alpine-no-rule": {"george": {detect.No, "", false, "None", 2, sudoers.GrantNone}},
		"alpine-locked": {"alpine": {detect.Yes, "group wheel", true, "None", 0, sudoers.GrantNone}, "george": {detect.Yes, "group wheel", true, "None", 2, sudoers.GrantNone},
			"alice": {detect.Yes, "group wheel", true, "Locked, key login refused", 1, sudoers.GrantNone}},
		"yml-missing":  {"george": {detect.Yes, cloud, true, "None", 2, sudoers.GrantNone}, "alice": {detect.Yes, "group sudo", false, "Set", 0, sudoers.GrantNone}},
		"not-read":     {"george": {detect.Unknown, "", false, "None", 2, sudoers.GrantNone}, "alice": {detect.Unknown, "", false, "Set", 0, sudoers.GrantNone}},
		"pve9":         {},
		"root-login":   {"george": {detect.Yes, "hardn", true, "None", 2, sudoers.GrantNoPassword}},
		"debian-nopam": {"george": {detect.Yes, cloud, true, "Locked, key login refused", 2, sudoers.GrantNone}, "alice": {detect.Yes, "group sudo", false, "Set", 0, sudoers.GrantNone}},
		"alpine-stray": {"alpine": {detect.Yes, "group wheel", true, "None", 0, sudoers.GrantNone}, "george": {detect.Yes, "group wheel", true, "None", 1, sudoers.GrantNone}},
		"group-home":   {"george": {detect.Yes, cloud, true, "None", 2, sudoers.GrantNone}, "alice": {detect.Yes, "group sudo", false, "Set", 0, sudoers.GrantNone}},
		"no-sudo":      {"george": {detect.No, "", false, "None", 2, sudoers.GrantNone}},
	}
	if len(wants) != len(usrHosts) {
		t.Fatalf("%d hosts, %d wanted", len(usrHosts), len(wants))
	}
	ctx := context.Background()
	for _, name := range usrHosts {
		for _, dryRun := range []bool{false, true} {
			h := newHost(t, name)
			p := play(h, dryRun, "0")
			if p.err != nil || p.engines != 0 || len(p.runs) != 0 || len(writes(h, 0)) > 0 {
				t.Errorf("%s, plan only %v: Run = %v, %d engines, wrote %v:\n%s", name, dryRun, p.err, p.engines, writes(h, 0), p.out)
			}
			checkHost(t, name, h)
			if dryRun {
				continue
			}
			st, _ := users.Admin{}.Check(ctx, h, h.facts)
			if !strings.Contains(p.out, "User") || !strings.Contains(p.out, st.Detail) {
				t.Errorf("%s: the User row is not Check's %q:\n%s", name, st.Detail, p.out)
			}
			r := readHost(t, h)
			if len(r.Users) != len(wants[name]) {
				t.Errorf("%s: %d users, want %d", name, len(r.Users), len(wants[name]))
			}
			sections := sectionsOf(p.out)
			for _, u := range r.Users {
				w := wants[name][u.Name]
				i := slices.IndexFunc(sections, func(s string) bool { return strings.HasPrefix(s, u.Name+" -") })
				if i < 0 {
					t.Errorf("%s: no section %s", name, u.Name)
					continue
				}
				s := sections[i]
				if u.Admin != w.admin || u.NoPassword != w.nopass || len(u.Keys) != w.keys || u.Policy.Grant != w.hardn ||
					rowOf(s, "Granted by") != w.by || rowOf(s, "Password") != w.pw {
					t.Errorf("%s, %s: admin %v, nopass %v, %d keys, hardn %v, by %q, password %q; want %+v", name, u.Name, u.Admin, u.NoPassword,
						len(u.Keys), u.Policy.Grant, rowOf(s, "Granted by"), rowOf(s, "Password"), w)
				}
			}
		}
	}
}

// sectionHeader is a section header as plain mode draws it
// (component.SectionHeader): the title from column 2, a space, then a run
// of the rule's "-".
var sectionHeader = regexp.MustCompile(`(?m)^  (\S.*?) -+$`)

// sectionsOf is out cut before each section header: each piece from the
// header's title, its lead dropped, down to the next header.
func sectionsOf(out string) []string {
	at := sectionHeader.FindAllStringIndex(out, -1)
	var got []string
	for k, ix := range at {
		end := len(out)
		if k+1 < len(at) {
			end = at[k+1][0]
		}
		got = append(got, out[ix[0]+2:end])
	}
	return got
}

// rowOf is the value of the row key in s, its marker dropped; "" when it has
// none.
func rowOf(s, key string) string {
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		if i := strings.Index(t, "] "); strings.HasPrefix(t, "[") && i > 0 {
			t = strings.TrimSpace(t[i+2:])
		}
		t = strings.TrimSpace(strings.TrimPrefix(t, "- "))
		if strings.HasPrefix(t, key+"  ") {
			return strings.Join(strings.Fields(strings.TrimPrefix(t, key)), " ")
		}
	}
	return ""
}

// refusalCase is one refusal said before asking, played: the host, the user,
// the item, its words.
type refusalCase struct {
	item, host, user, words string
}

// refusals are the lockout paths and refusals the gallery drew
// (usrRefusals), in the decided words.
func refusals() []refusalCase {
	cloud := sudoers.DropInDir + "/90-cloud-init-users"
	notRead := fmt.Sprintf(wordsNotRead, "sudo", "its files don't check.", "visudo -c")
	return []refusalCase{
		{"remove", "hardn", "george", fmt.Sprintf(wordsSession, "george", "sudo")},
		{"remove", "debian13", "george", fmt.Sprintf(wordsNotHardn, "george", cloud)},
		{"remove", "debian13", "alice", fmt.Sprintf(wordsGroup, "alice", "sudo", "alice", "alice", "sudo")},
		{"remove", "legacy", "george", fmt.Sprintf(wordsNotHardn, "george", sudoers.DropInDir+"/george")},
		{"remove", "alpine323", "george", fmt.Sprintf(wordsGroup, "george", "wheel", "george", "george", "wheel")},
		{"remove", "not-read", "george", notRead},
		{"require", "debian13", "george", fmt.Sprintf(wordsNoPassword, "george", "sudo", "george")},
		{"require", "alpine323", "george", fmt.Sprintf(wordsNoPassword, "george", "doas", "george")},
		{"remove-key", "one-key", "george", fmt.Sprintf(wordsLastKey, "george", "george")},
		{"grant", "no-sudo", "george", wordsNoTool},
		{"grant", "not-read", "alice", notRead},
		{"remove", "root-login", "george", fmt.Sprintf(wordsLastAdmin, "george")},
		{"remove-key", "alpine-stray", "george", fmt.Sprintf(wordsLastKey, "george", "george")},
		{"add-key", "group-home", "alice", fmt.Sprintf(wordsStrict, "alice", "/home/alice is writable by its group (0775)")},
	}
}

// itemLines are the lines that choose rf's item for the user at index i, and
// go back to the start.
func itemLines(item string, i int) []string {
	switch item {
	case "remove-key":
		return []string{"1", fmt.Sprint(i), "3", "2", "0", "0", "0", "0"}
	case "add-key":
		return []string{"1", fmt.Sprint(i), "3", "1", "0", "0", "0", "0"}
	case "require":
		return []string{"1", fmt.Sprint(i), "2", "0", "0", "0"}
	}
	return []string{"1", fmt.Sprint(i), "1", "0", "0", "0"}
}

// TestRefusals: every lockout path and refusal of gallery-users.md §6 says
// why before asking, played: the control's words shown, nothing asked, no
// engine, nothing written.
func TestRefusals(t *testing.T) {
	for _, rf := range refusals() {
		h := newHost(t, rf.host)
		_, i := userOf(t, readHost(t, h), rf.user)
		n := len(h.Calls())
		p := play(h, false, itemLines(rf.item, i)...)
		if p.err != nil || !shows(p.out, rf.words) || asked(p.out) || p.engines != 0 || p.unread != 0 {
			t.Errorf("%v: Run = %v, %d engines, not refused before asking:\n%s", rf, p.err, p.engines, p.out)
		}
		if w := writes(h, n); len(w) > 0 {
			t.Errorf("%v: wrote %v", rf, w)
		}
	}
	h := newHost(t, "hardn")
	n, _ := sudoers.FileNames(sudoers.ToolSudo, "george")
	h.AddFile(n.Grant, []byte(sudoers.Header+"george ALL=(ALL) ALL # mine\n"), 0o440, 0, 0)
	if p := play(h, false, "1", "1", "1", "0", "0", "0"); !shows(p.out, fmt.Sprintf(wordsEdited, n.Grant)) || asked(p.out) {
		t.Errorf("hardn's file edited:\n%s", p.out)
	}
}

// TestStillGranted: where hardn's grant and other rules make a user an
// admin, Remove refuses, naming every rule still granting (users.StillAdmin);
// a rule asking a password the user lacks is not counted; a rule the reader
// marks Unknown refuses, naming it.
func TestStillGranted(t *testing.T) {
	for _, tc := range []struct {
		name  string
		host  func() *usrHost
		user  string
		words string
	}{
		{"two rules", func() *usrHost {
			h := newHost(t, "debian13").hardnGrant("alice", sudoers.GrantPassword)
			h.AddFile(sudoers.DropInDir+"/50-alice", []byte("alice ALL=(ALL) NOPASSWD: ALL\n"), 0o440, 0, 0)
			return h
		}, "alice", fmt.Sprintf(wordsStill, "alice", "/etc/sudoers.d/50-alice and the sudo group")},
		{"in sudo with no password", func() *usrHost {
			h := newHost(t, "debian13").hardnGrant("george", sudoers.GrantNoPassword)
			_ = h.Remove(context.Background(), sudoers.DropInDir+"/90-cloud-init-users")
			return h
		}, "george", fmt.Sprintf(wordsSession, "george", "sudo")},
		{"a rule not read", func() *usrHost {
			h := newHost(t, "second-admin")
			h.AddFile(sudoers.DropInDir+"/50-alice", []byte("alice web-01=(ALL) ALL\n"), 0o440, 0, 0)
			return h
		}, "alice", fmt.Sprintf(wordsStillUnknown, "alice", "/etc/sudoers.d/50-alice:1")},
	} {
		h := tc.host()
		_, i := userOf(t, readHost(t, h), tc.user)
		if p := play(h, false, "1", fmt.Sprint(i), "1", "0", "0", "0"); !shows(p.out, tc.words) || asked(p.out) {
			t.Errorf("%s: not refused in %q:\n%s", tc.name, tc.words, p.out)
		}
	}
}

// TestRows: a grant asking no password is a cost for an account with none,
// a failure for one with a password; Manage User is offered only with a
// user to manage, its detail the host's tool.
func TestRows(t *testing.T) {
	for _, tc := range []struct{ host, marker, note string }{{"debian13", "[ ] Sudo", wordsNoPassToAsk}, {"legacy", "[ ] Sudo", "No password"}} {
		p := play(newHost(t, tc.host), false, "0")
		if !strings.Contains(p.out, tc.marker) || rowOf(p.out, "Sudo") != "Enabled, "+tc.note {
			t.Errorf("%s: %q:\n%s", tc.host, rowOf(p.out, "Sudo"), p.out)
		}
	}
	for host, want := range map[string]string{"debian13": "1) Manage User          Configure sudo, SSH keys", "alpine323": "1) Manage User          Configure doas, SSH keys"} {
		if p := play(newHost(t, host), false, "0"); !strings.Contains(p.out, want) || !strings.Contains(p.out, "2) Create User") {
			t.Errorf("%s:\n%s", host, p.out)
		}
	}
	if p := play(newHost(t, "pve9"), false, "1", "0"); strings.Contains(p.out, "Manage User") || !strings.Contains(p.out, "No user besides root") || p.err != nil {
		t.Errorf("pve9: Run = %v:\n%s", p.err, p.out)
	}
}

// TestDoasFile: a doas grant is hardn's zz-hardn-<stem>.conf, which sorts
// after wheel.conf (decision 2 (a) (i)), so its plan names no file that
// overrides it; y/N. It asks george's password, which george has not got:
// the review's row and the plan's finding say so (the maintainer,
// 2026-11-11).
func TestDoasFile(t *testing.T) {
	h := newHost(t, "alpine-no-rule")
	p := play(h, false, "1", "1", "1", "y", "0", "0", "0")
	data, err := h.ReadFile(context.Background(), sudoers.DoasDir+"/zz-hardn-george.conf")
	if p.err != nil || err != nil || !strings.Contains(string(data), "permit persist george") || strings.Contains(string(data), "nopass") ||
		!shows(p.out, "Enabled, No password set") || !shows(p.out, fmt.Sprintf(wordsNotSet, "george", "doas", "george")) || strings.Contains(p.out, "wheel.conf") || strings.Contains(p.out, typeIt) {
		t.Errorf("Run = %v, %q, %v:\n%s", p.err, data, err, p.out)
	}
}

// TestKeyFiles: keys are counted only in the files sshd -T says it reads;
// with ~/.ssh/authorized_keys not among them every key change and Create's
// key refuse before asking; with sshd -T not read, too.
func TestKeyFiles(t *testing.T) {
	r := readHost(t, newHost(t, "alpine-stray"))
	if !slices.Equal(r.KeyEntries, []string{".ssh/authorized_keys"}) || r.KeysRefusal() != nil {
		t.Errorf("alpine-stray: key files %q, %v", r.KeyEntries, r.KeysRefusal())
	}
	h := newHost(t, "debian13")
	h.AddFile("/etc/ssh/sshd_config.d/50-keys.conf", []byte("AuthorizedKeysFile /etc/ssh/keys/%u\n"), 0o644, 0, 0)
	words := fmt.Sprintf(wordsKeyFiles, "/etc/ssh/keys/%u")
	for _, lines := range [][]string{{"1", "1", "3", "1", "0", "0", "0", "0"}, {"2", "carol", "n", "y", "n", "n", "0"}} {
		n := len(h.Calls())
		p := play(h, false, lines...)
		if !shows(p.out, words) || p.err != nil || len(writes(h, n)) > 0 {
			t.Errorf("AuthorizedKeysFile /etc/ssh/keys/%%u, %q: Run = %v:\n%s", lines, p.err, p.out)
		}
	}
	h = newHost(t, "debian13")
	_ = h.Remove(context.Background(), "/etc/ssh/sshd_config")
	r = readHost(t, h)
	if r.KeyErr == nil || screenkit.RefusalWords(r.KeysRefusal()) != fmt.Sprintf(wordsKeyFilesRead, screenkit.FullStop(r.KeyErr.Error())) {
		t.Errorf("sshd -T not read: %v", r.KeyErr)
	}
	if p := play(h, false, "1", "1", "3", "1", "0", "0", "0", "0"); !shows(p.out, "hardn can't read which files sshd takes keys from") || asked(p.out) {
		t.Errorf("sshd -T not read, Add:\n%s", p.out)
	}
}

// TestLockoutRisk: the changes that can lock a user out are typed with the
// hostname, and confirmed or reverted: removing alice's admin rights, asking
// alice's password, removing a key of the session user's, and any key with
// the session user not known; the others are y/N.
func TestLockoutRisk(t *testing.T) {
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
		{"debian13", []string{"1", "2", "3", "1", carolKey, "y", "0", "0", "0", "0"}, false},
		{"alpine-no-rule", []string{"1", "1", "1", "y", "0", "0", "0"}, false},
		{"root-login", []string{"1", "1", "3", "2", "1", hostName, "0", "0", "0", "0"}, true},
	} {
		h := newHost(t, tc.host)
		p := play(h, false, tc.lines...)
		typed := strings.Contains(p.out, typeIt)
		if p.err != nil || p.unread != 0 || typed != tc.typed || !shows(p.out, applied) || len(p.runs) != 1 {
			t.Errorf("%s %v: Run = %v, typed %v, want %v:\n%s", tc.host, tc.lines, p.err, typed, tc.typed, p.out)
		}
		if typed && !strings.Contains(p.out, "hardn confirm") {
			t.Errorf("%s %v: a typed plan without confirm-or-revert", tc.host, tc.lines)
		}
		checkHost(t, tc.host, h)
	}
}

// TestRemoveAdmin: alice's admin rights removed, typed: hardn's file gone,
// and the screen drawn again from a fresh reading, alice not an admin.
func TestRemoveAdmin(t *testing.T) {
	h := newHost(t, "second-admin")
	n, _ := sudoers.FileNames(sudoers.ToolSudo, "alice")
	p := play(h, false, "1", "2", "1", hostName, "0", "0", "0")
	if _, err := h.ReadFile(context.Background(), n.Grant); !errors.Is(err, fs.ErrNotExist) || p.err != nil {
		t.Errorf("%s after Remove: %v; Run = %v", n.Grant, err, p.err)
	}
	after := strings.Split(p.out, "Operation Successful")
	if len(after) != 2 || !strings.Contains(after[1], "1) Grant admin rights") {
		t.Errorf("not drawn again from a fresh reading:\n%s", p.out)
	}
}

// TestRemoveKey: a key removed is gone from the file, the others kept, the
// file 0600; the review counts what is left.
func TestRemoveKey(t *testing.T) {
	h := newHost(t, "debian13")
	u, _ := userOf(t, readHost(t, h), "george")
	p := play(h, false, "1", "1", "3", "2", "1", hostName, "0", "0", "0", "0")
	ctx := context.Background()
	data, err := h.ReadFile(ctx, "/home/george/.ssh/authorized_keys")
	if err != nil || strings.Contains(string(data), u.Keys[0].Line) || !strings.Contains(string(data), u.Keys[1].Line) {
		t.Errorf("authorized_keys after removing the first key: %q, %v", data, err)
	}
	if fi, _ := h.Stat(ctx, "/home/george/.ssh/authorized_keys"); fi == nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("authorized_keys mode %v", fi)
	}
	if rowOf(strings.Split(p.out, "Remove SSH key #")[1], "SSH Keys") != "1 key" {
		t.Errorf("the review:\n%s", p.out)
	}
}

// TestRemoveKeyElsewhere: a key also in authorized_keys2, which sshd reads
// too, is refused at "Remove which key?" in the control's words
// (USERS_KEY_ELSEWHERE), asked again; no engine, nothing written.
func TestRemoveKeyElsewhere(t *testing.T) {
	h := newHost(t, "debian13")
	two := "/home/george/.ssh/authorized_keys2"
	h.AddFile(two, []byte(synthKey(1, "george@laptop-1")+"\n"), 0o600, 1000, 1000)
	n := len(h.Calls())
	p := play(h, false, "1", "1", "3", "2", "1", "q")
	words := fmt.Sprintf(wordsElsewhere, "george", two)
	if !errors.Is(p.err, nav.ErrQuit) || !shows(p.out, words) || !strings.Contains(p.out, screen.UsersWhichKeyField) ||
		strings.Count(p.out, "Option [1-2, 0 or q]") != 2 || p.engines != 0 || asked(p.out) {
		t.Errorf("Run = %v, %d engines:\n%s", p.err, p.engines, p.out)
	}
	if w := writes(h, n); len(w) > 0 {
		t.Errorf("wrote %v", w)
	}
}

// TestAddKey: Add SSH key writes the key, y/N; a key that is not one is
// refused at its field in the control's words, asked again; a locked user's
// plan says sshd refuses its keys, the control's finding.
func TestAddKey(t *testing.T) {
	h := newHost(t, "debian13")
	p := play(h, false, "1", "2", "3", "1", "nope", carolKey, "y", "0", "0", "0", "0")
	data, err := h.ReadFile(context.Background(), "/home/alice/.ssh/authorized_keys")
	if p.err != nil || err != nil || strings.TrimSpace(string(data)) != carolKey || !shows(p.out, "Paste one OpenSSH public key, such as ssh-ed25519") {
		t.Errorf("Run = %v, %q, %v:\n%s", p.err, data, err, p.out)
	}
	// A key george has: warned at the field, asked again; refusing a new
	// one there too, the review never counts it, and nothing is planned.
	h = newHost(t, "debian13")
	george, _ := userOf(t, readHost(t, h), "george")
	n := len(h.Calls())
	p = play(h, false, "1", "1", "3", "1", george.Keys[0].Line, "q")
	if !errors.Is(p.err, nav.ErrQuit) || !shows(p.out, fmt.Sprintf(wordsDupKey, "george")) || strings.Count(p.out, screen.UsersKeyField) != 2 ||
		p.engines != 0 || asked(p.out) || len(writes(h, n)) > 0 {
		t.Errorf("a key george has: Run = %v, %d engines:\n%s", p.err, p.engines, p.out)
	}
	h = newHost(t, "alpine-locked")
	if p := play(h, true, "1", "3", "3", "1", carolKey, "0", "0", "0", "0"); !shows(p.out, fmt.Sprintf(wordsLocked, "alice")) || !shows(p.out, planned) {
		t.Errorf("alpine-locked: Run = %v:\n%s", p.err, p.out)
	}
}

// TestCreatePassword: a user created where sshd's PAM is not on (Alpine;
// Debian with UsePAM no) gets "*", not useradd's "!", so that its key logs
// it in; where PAM is on, useradd's own "!". The home is stated and
// created, its mode the host's.
func TestCreatePassword(t *testing.T) {
	ctx := context.Background()
	for host, want := range map[string]struct {
		shadow string
		home   fs.FileMode
	}{"alpine323": {"carol:*:", 0o755}, "debian13": {"carol:!:", 0o700}, "debian-nopam": {"carol:*:", 0o700}} {
		h := newHost(t, host)
		p := play(h, false, "2", "carol", "y", "n", "y", carolKey, "y", "0")
		shadow, _ := h.ReadFile(ctx, accounts.ShadowFile)
		keys, err := h.ReadFile(ctx, "/home/carol/.ssh/authorized_keys")
		if p.err != nil || !strings.Contains(string(shadow), want.shadow) || err != nil || strings.TrimSpace(string(keys)) != carolKey {
			t.Errorf("%s: Run = %v, shadow %q, keys %q, %v", host, p.err, shadow, keys, err)
		}
		if fi, err := h.Stat(ctx, "/home/carol"); !ran(h, 0, "useradd --home-dir /home/carol --create-home") || err != nil || fi.Mode().Perm() != want.home {
			t.Errorf("%s: the home %v, %v", host, fi, err)
		}
		r := readHost(t, h)
		if u, _ := userOf(t, r, "carol"); u.Admin != detect.Yes || u.Decided.Kind != users.RuleHardn || u.Locked || len(u.Keys) != 1 {
			t.Errorf("%s: carol read back %+v", host, u)
		}
		if !strings.Contains(strings.Split(p.out, applied)[1], "\n  carol -") {
			t.Errorf("%s: carol not drawn after:\n%s", host, p.out)
		}
	}
}

// TestCreateNoAdmin: Create answered n to admin rights writes no elevation
// rule and joins no group, and the user reads back with none; with no key
// the plan says the user can't log in. On Proxmox VE, Create works with no
// tool.
func TestCreateNoAdmin(t *testing.T) {
	for _, host := range []string{"debian13", "pve9"} {
		h := newHost(t, host)
		p := play(h, false, "2", "carol", "n", "n", "y", "0")
		if p.err != nil || !shows(p.out, applied) || !shows(p.out, strings.TrimSuffix(fmt.Sprintf(wordsNoKeyNotice, "carol"), ".")) {
			t.Fatalf("%s: Run = %v:\n%s", host, p.err, p.out)
		}
		for _, w := range writes(h, 0) {
			if strings.Contains(w, sudoers.DropInDir+"/") || strings.Contains(w, sudoers.DoasDir+"/") || strings.Contains(w, "usermod") || strings.Contains(w, "gpasswd") {
				t.Errorf("%s: %s", host, w)
			}
		}
		if u, _ := userOf(t, readHost(t, h), "carol"); u.Admin != detect.No || u.Policy.Grant != sudoers.GrantNone {
			t.Errorf("%s: carol read back %+v", host, u)
		}
	}
}

// TestCreateForm: each field refused at the field in the control's words,
// asked again: a name useradd refuses, one the host has, yes to admin rights with no tool, yes to a key where sshd
// reads no ~/.ssh/authorized_keys, a key that is not one; the password asked
// only after yes to admin rights. Nothing is planned until it is answered.
func TestCreateForm(t *testing.T) {
	for _, tc := range []struct {
		host  string
		lines []string
		words string
	}{
		{"debian13", []string{"2", "Carol", "carol", "q"}, wordsName},
		{"debian13", []string{"2", "george", "carol", "q"}, fmt.Sprintf(wordsTaken, "george")},
		{"no-sudo", []string{"2", "carol", "y", "n", "q"}, wordsNoTool},
		{"debian13", []string{"2", "carol", "n", "y", "nope", "q"}, "Paste one OpenSSH public key, such as ssh-ed25519"},
	} {
		h := newHost(t, tc.host)
		p := play(h, false, tc.lines...)
		if !errors.Is(p.err, nav.ErrQuit) || p.unread != 0 || !shows(p.out, tc.words) || p.engines != 0 || len(writes(h, 0)) > 0 {
			t.Errorf("%s %q: Run = %v, %d engines:\n%s", tc.host, tc.lines, p.err, p.engines, p.out)
		}
	}
	if p := play(newHost(t, "debian13"), false, "2", "carol", "n", "n", "q"); strings.Contains(p.out, "Require password") {
		t.Errorf("the password asked with no admin rights:\n%s", p.out)
	}
	if p := play(newHost(t, "no-sudo"), false, "2", "carol", "y", "n", "q"); strings.Contains(p.out, "Require password") {
		t.Errorf("admin rights taken at their field with no sudo:\n%s", p.out)
	}
}

// TestCreateDefaults: Create's defaults are hardn.yml's username and first
// key, never a user the rows are about (decision 7), each an empty line's
// answer; "Grant admin rights?" is no. With no hardn.yml, none: an empty
// name is refused.
func TestCreateDefaults(t *testing.T) {
	h := newHost(t, "yml-missing")
	cfg := loadYML(t, h, usrYML)
	p := playWith(h, true, func(o *usrscreen.Options) { o.Config = cfg }, "2", "", "", "", "", "0")
	if p.err != nil || !shows(p.out, "Default: deploy") || !shows(p.out, "Grant admin rights? [y/N") || shows(p.out, "Require password") || !shows(p.out, "Add SSH key? [Y/n") ||
		!shows(p.out, "Default: hardn.yml's key") || !shows(p.out, "useradd --home-dir /home/deploy") || !shows(p.out, "Add deploy's key") || !shows(p.out, planned) {
		t.Errorf("hardn.yml's defaults: Run = %v:\n%s", p.err, p.out)
	}
	if before := strings.Split(p.out, "Create User #")[0]; strings.Contains(before, "deploy") {
		t.Errorf("hardn.yml's user drawn:\n%s", before)
	}
	p = play(newHost(t, "debian13"), false, "2", "", "carol", "n", "n", "q")
	if !errors.Is(p.err, nav.ErrQuit) || !shows(p.out, wordsName) || shows(p.out, "Default:") || !shows(p.out, "Grant admin rights? [y/N") {
		t.Errorf("no hardn.yml: Run = %v:\n%s", p.err, p.out)
	}
}

// TestCreateSetsPassword: Create answered yes to admin rights asks whether
// to require a password, yes by default, no where hardn.yml's
// adminNoPassword is true (the maintainer, 2026-11-11: admin rights
// without a password are opt-in); after yes, whether to set it now, yes by
// default. Applied, hardn's grant asks the password, and passwd carol is
// run with the terminal, once, after the plan; carol reads back able to
// become root. Answered no to setting it, declined at the plan, and under
// plan only, passwd is not run. passwd failing is said, carol left as she
// is. No to requiring one writes the grant asking none, and asks nothing
// more.
func TestCreateSetsPassword(t *testing.T) {
	passwd := func(h *usrHost) (n int) {
		for _, c := range h.Calls() {
			if c.Op == "run-interactive" && slices.Equal(c.Argv, []string{"passwd", "carol"}) {
				n++
			}
		}
		return n
	}
	grant := func(h *usrHost) string {
		data, _ := h.ReadFile(context.Background(), sudoers.DropInDir+"/hardn-grant-carol")
		return string(data)
	}
	const notSet = "carol's password isn't set. Run passwd carol."
	for _, tc := range []struct {
		name    string
		lines   []string
		dryRun  bool
		fails   bool
		ran     int
		rule    string // carol's rule; "" for no grant
		pw      accounts.Password
		said    []string
		notSaid []string
	}{
		{name: "the defaults", lines: []string{"2", "carol", "y", "", "", "n", "y", "0"}, ran: 1, rule: "carol ALL=(ALL) ALL", pw: accounts.PasswordSet,
			said:    []string{"Require password for admin rights? [Y/n", "Set carol's password now? [Y/n", "Enabled, No password set", fmt.Sprintf(wordsNotSet, "carol", "sudo", "carol"), applied, "Setting carol's password"},
			notSaid: []string{notSet}},
		{name: "not now", lines: []string{"2", "carol", "y", "y", "n", "n", "y", "0"}, rule: "carol ALL=(ALL) ALL", pw: accounts.PasswordNone, said: []string{applied}, notSaid: []string{"Setting carol's password"}},
		{name: "declined", lines: []string{"2", "carol", "y", "y", "y", "n", "n", "0"}, pw: accounts.PasswordNotRead, notSaid: []string{applied, "Setting carol's password"}},
		{name: "plan only", lines: []string{"2", "carol", "y", "y", "n", "0"}, dryRun: true, pw: accounts.PasswordNotRead, said: []string{planned}, notSaid: []string{"Set carol's password now?"}},
		{name: "passwd fails", lines: []string{"2", "carol", "y", "y", "y", "n", "y", "0"}, fails: true, ran: 1, rule: "carol ALL=(ALL) ALL", pw: accounts.PasswordNone, said: []string{applied, notSet}},
		{name: "no password", lines: []string{"2", "carol", "y", "n", "n", "y", "0"}, rule: "carol ALL=(ALL) NOPASSWD: ALL", pw: accounts.PasswordNone,
			said: []string{"Enabled, No password to ask for", applied}, notSaid: []string{"Set carol's password now?", "until a password is set"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t, "debian13")
			h.passwdFails = tc.fails
			p := play(h, tc.dryRun, tc.lines...)
			if p.err != nil || p.unread != 0 {
				t.Fatalf("Run = %v, %d lines unread:\n%s", p.err, p.unread, p.out)
			}
			for _, w := range tc.said {
				if !shows(p.out, w) {
					t.Errorf("lacks %q:\n%s", w, p.out)
				}
			}
			for _, w := range tc.notSaid {
				if shows(p.out, w) {
					t.Errorf("says %q:\n%s", w, p.out)
				}
			}
			if got := passwd(h); got != tc.ran {
				t.Errorf("passwd carol run %d times, want %d", got, tc.ran)
			}
			if g := grant(h); (tc.rule == "") != (g == "") || !strings.Contains(g, tc.rule) {
				t.Errorf("grant %q, want %q", g, tc.rule)
			}
			us := readHost(t, h).Users
			i := slices.IndexFunc(us, func(u users.User) bool { return u.Name == "carol" })
			pw := accounts.PasswordNotRead
			if i >= 0 {
				pw = us[i].Password
			}
			if (tc.rule != "") != (i >= 0) || pw != tc.pw {
				t.Errorf("carol there %v, password %v, want %v", i >= 0, pw, tc.pw)
			}
			if tc.pw == accounts.PasswordSet && (us[i].Admin != detect.Yes || us[i].NoPassword) {
				t.Errorf("carol read back %+v, want an admin whose password is asked", us[i])
			}
		})
	}
	h := newHost(t, "yml-missing")
	cfg := loadYML(t, h, "username: deploy\nadminNoPassword: true\n")
	if p := playWith(h, true, func(o *usrscreen.Options) { o.Config = cfg }, "2", "", "y", "", "n", "0"); p.err != nil || !shows(p.out, "Require password for admin rights? [y/N") ||
		!shows(p.out, "Enabled, No password to ask for") || !shows(p.out, planned) {
		t.Errorf("adminNoPassword: true: Run = %v:\n%s", p.err, p.out)
	}
}

// TestPlanOnlyWritesNothing: under plan only every change opens as live and
// stops at its plan, nothing written, the typed ones needing no hostname.
func TestPlanOnlyWritesNothing(t *testing.T) {
	flows := map[string][][]string{
		"debian13":       {{"2", "carol", "y", "n", "y", carolKey, "0"}, {"1", "2", "3", "1", carolKey, "0", "0", "0", "0"}, {"1", "1", "3", "2", "1", "0", "0", "0", "0"}},
		"second-admin":   {{"1", "2", "1", "0", "0", "0"}, {"1", "2", "2", "0", "0", "0"}},
		"sudo-password":  {{"1", "1", "2", "0", "0", "0"}},
		"alpine-no-rule": {{"1", "1", "1", "0", "0", "0"}},
		"alpine323":      {{"2", "carol", "y", "n", "y", carolKey, "0"}},
		"pve9":           {{"2", "carol", "n", "y", carolKey, "0"}},
	}
	for host, all := range flows {
		for _, lines := range all {
			h := newHost(t, host)
			_ = h.Remove(context.Background(), header.HostnameFile)
			n := len(h.Calls())
			p := play(h, true, lines...)
			if p.err != nil || !shows(p.out, planned) || shows(p.out, cantConfirm) || p.unread != 0 {
				t.Errorf("%s %v: Run = %v, did not stop at the plan:\n%s", host, lines, p.err, p.out)
			}
			if w := writes(h, n); len(w) > 0 {
				t.Errorf("%s %v: plan only wrote %v", host, lines, w)
			}
		}
	}
}

// TestKeys: the key check, the control's, takes one OpenSSH public key and
// refuses anything else in the decided words; the fingerprint is
// ssh-keygen's form.
func TestKeys(t *testing.T) {
	k, err := users.ParseKey(carolKey)
	if err != nil || !strings.HasPrefix(k.Fingerprint, "SHA256:") || len(k.Fingerprint) != 50 || k.Comment != "carol@laptop" {
		t.Errorf("%q: %+v, %v", carolKey, k, err)
	}
	for _, bad := range []string{"", "ssh-ed25519", "ssh-ed25519 !!!", "ssh-rsa " + strings.Fields(carolKey)[1], "ssh-dss AAAAB3NzaC1kc3M=", `from="192.0.2.1" ` + carolKey} {
		if _, err := users.ParseKey(bad); screenkit.RefusalWords(err) != wordsKey {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

// TestNames: the names useradd refuses, refused; a name the host has,
// refused, in the decided words.
func TestNames(t *testing.T) {
	ctx := context.Background()
	h := newHost(t, "debian13")
	r := readHost(t, h)
	for name, ok := range map[string]bool{"carol": true, "_svc": true, "web-01": true, "Carol": false, "1carol": false, "car ol": false,
		"george": false, "sudo": false, strings.Repeat("a", 33): false, "permit": false, "": false} {
		if got := r.CheckName(ctx, h, name) == nil; got != ok {
			t.Errorf("%q: taken %v, want %v", name, got, ok)
		}
	}
	if w := screenkit.RefusalWords(r.CheckName(ctx, h, "george")); w != fmt.Sprintf(wordsTaken, "george") {
		t.Errorf("george: %q", w)
	}
}

// TestHostname: a lockout-risk change whose hostname can't be read says so
// before asking, live: nothing asked, no engine, nothing written; under plan
// only it stops at its plan. A wrong phrase is refused and asked again; 0
// declines, writing nothing.
func TestHostname(t *testing.T) {
	for _, data := range []string{"<absent>", "\n"} {
		h := newHost(t, "second-admin")
		if data == "<absent>" {
			_ = h.Remove(context.Background(), header.HostnameFile)
		} else {
			h.AddFile(header.HostnameFile, []byte(data), 0o444, 0, 0)
		}
		for _, lines := range [][]string{{"1", "2", "1", "0", "0", "0"}, {"1", "1", "3", "2", "0", "0", "0", "0"}} {
			n := len(h.Calls())
			p := play(h, false, lines...)
			if p.err != nil || !shows(p.out, cantConfirm) || p.engines != 0 || asked(p.out) || len(writes(h, n)) > 0 {
				t.Errorf("%q %q: Run = %v, %d engines:\n%s", data, lines, p.err, p.engines, p.out)
			}
		}
	}
	h := newHost(t, "second-admin")
	n, _ := sudoers.FileNames(sudoers.ToolSudo, "alice")
	p := play(h, false, "1", "2", "1", "web-02", "0", "0", "0", "0")
	if _, err := h.ReadFile(context.Background(), n.Grant); err != nil || p.err != nil || p.unread != 0 || shows(p.out, applied) || strings.Count(p.out, typeIt) != 2 {
		t.Errorf("a wrong phrase, then 0: Run = %v, %v:\n%s", p.err, err, p.out)
	}
	if w := writes(h, 0); slices.ContainsFunc(w, func(s string) bool { return strings.Contains(s, "sudoers") }) {
		t.Errorf("declined wrote %v", w)
	}
	// The hostname read before asking, gone by the question: said as it is,
	// alice's grant kept.
	h = newHost(t, "second-admin")
	p = playWith(h, false, func(o *usrscreen.Options) {
		build := o.NewEngine
		o.NewEngine = func(progress func(engine.Event)) *engine.Engine {
			_ = h.Remove(context.Background(), header.HostnameFile)
			return build(progress)
		}
	}, "1", "2", "1", "0", "0", "0")
	if _, err := h.ReadFile(context.Background(), n.Grant); err != nil || p.err != nil || !shows(p.out, cantConfirm) || shows(p.out, applied) {
		t.Errorf("the hostname gone by the question: Run = %v, %v:\n%s", p.err, err, p.out)
	}
}

// TestMoved: a change whose reading moved since the draw is planned from a
// fresh reading: alice's grant gone before the plan, Remove plans nothing;
// george's key gone, Remove SSH key refuses (USERS_KEY_GONE), the control's
// words, never FILE_NOT_FOUND; nothing is applied to the old picture.
func TestMoved(t *testing.T) {
	h := newHost(t, "second-admin")
	n, _ := sudoers.FileNames(sudoers.ToolSudo, "alice")
	p := playWith(h, false, func(o *usrscreen.Options) {
		build := o.NewEngine
		o.NewEngine = func(progress func(engine.Event)) *engine.Engine {
			_ = h.Remove(context.Background(), n.Grant)
			return build(progress)
		}
	}, "1", "2", "1", "0", "0", "0")
	if p.err != nil || shows(p.out, applied) || len(p.runs) != 1 || p.runs[0].Result != nil && !p.runs[0].Result.Nothing {
		t.Errorf("grant gone: Run = %v, runs %+v:\n%s", p.err, p.runs, p.out)
	}
	h = newHost(t, "debian13")
	keys := "/home/george/.ssh/authorized_keys"
	p = playWith(h, false, func(o *usrscreen.Options) {
		build := o.NewEngine
		o.NewEngine = func(progress func(engine.Event)) *engine.Engine {
			h.AddFile(keys, []byte(synthKey(2, "george@laptop-2")+"\n"), 0o600, 1000, 1000)
			return build(progress)
		}
	}, "1", "1", "3", "2", "1", "0", "0", "0", "0")
	data, _ := h.ReadFile(context.Background(), keys)
	// Gone between the pick and the engine's plan: the flow's report of a
	// refused plan, in the refusal's words and code.
	gone := fmt.Sprintf(wordsKeyGone, keys)
	if p.err != nil || shows(p.out, applied) || string(data) != synthKey(2, "george@laptop-2")+"\n" ||
		!shows(p.out, strings.TrimSuffix(gone, ".")+" (USERS_KEY_GONE)") ||
		shows(p.out, "FILE_NOT_FOUND") || shows(p.out, "is not in") {
		t.Errorf("key gone at the plan: Run = %v, %q:\n%s", p.err, data, p.out)
	}
	// The key leaves after the keys are drawn, before Remove SSH key: said
	// as a refusal before asking, then the keys drawn again from a fresh
	// reading, without it.
	h = newHost(t, "debian13")
	n0 := len(h.Calls())
	p = playWith(h, false, func(o *usrscreen.Options) {
		o.Prompter.In = prompt.New(&hooked{r: uitest.NewTypist(io.Discard, "1", "1", "3", "2", "0", "0", "0", "0"), at: 3, fn: func() {
			h.AddFile(keys, []byte(synthKey(2, "george@laptop-2")+"\n"), 0o600, 1000, 1000)
		}}, false)
	})
	_, after, _ := strings.Cut(flat(p.out), flat(gone))
	if p.err != nil || !shows(p.out, gone) || asked(p.out) || p.engines != 0 || strings.Contains(p.out, screen.UsersWhichKeyField) ||
		!strings.Contains(after, "george@laptop-2") || strings.Contains(after, "george@laptop-1") || len(writes(h, n0)) > 0 {
		t.Errorf("key gone after the draw: Run = %v, %d engines, wrote %v:\n%s", p.err, p.engines, writes(h, n0), p.out)
	}
}

// hooked is r, calling fn once before the line at index at is read: the
// host changed while the screen waits for that line.
type hooked struct {
	r     io.Reader
	at, n int
	fn    func()
}

func (h *hooked) Read(b []byte) (int, error) {
	if h.n == h.at && h.fn != nil {
		h.fn()
		h.fn = nil
	}
	n, err := h.r.Read(b)
	h.n += bytes.Count(b[:n], []byte("\n"))
	return n, err
}

// TestHeights: each host's screen at 80x24 and 80x40, in Unicode and ASCII,
// is as many lines at either height, the prompt on its last (the gallery's
// TestUsersHeights, its counts gallery-users.md §4's).
func TestHeights(t *testing.T) {
	want := map[string]int{"debian13": 26, "ubuntu2604": 26, "alpine323": 26, "legacy": 19, "hardn": 19, "second-admin": 26, "one-key": 26,
		"sudo-password": 19, "alpine-no-rule": 18, "alpine-locked": 33, "yml-missing": 26, "not-read": 24, "pve9": 15, "no-sudo": 18,
		"root-login": 19, "debian-nopam": 26, "alpine-stray": 26, "group-home": 26}
	// The user list, george's screen and george's keys, drawn second,
	// third and fourth.
	draws := map[string][]string{"select": {"1", "0", "0"}, "manage": {"1", "1", "0", "0", "0"}, "keys": {"1", "1", "3", "0", "0", "0", "0"}}
	sub := map[string]struct{ at, lines int }{"select": {1, 10}, "manage": {2, 20}, "keys": {3, 19}}
	for _, rows := range []int{24, 40} {
		for _, unicode := range []bool{true, false} {
			lang := map[bool]string{true: "LANG=en_US.UTF-8", false: "LANG=C"}[unicode]
			env := term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Height: rows, Environ: []string{"TERM=xterm-256color", lang}})
			screens := func(h *usrHost, lines ...string) []int {
				var out bytes.Buffer
				playWith(h, false, func(o *usrscreen.Options) {
					o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, lines...), false), Out: env.Writer(&out)}
				})
				return drawn(escapes.ReplaceAllString(out.String(), ""))
			}
			for _, name := range usrHosts {
				if got := screens(newHost(t, name), "0"); len(got) != 1 || got[0] != want[name] {
					t.Errorf("%s at 80x%d, unicode %v: %v lines, want %d", name, rows, unicode, got, want[name])
				}
			}
			for name, lines := range draws {
				if got, w := screens(newHost(t, "debian13"), lines...), sub[name]; len(got) <= w.at || got[w.at] != w.lines {
					t.Errorf("%s at 80x%d, unicode %v: %v lines, want %d", name, rows, unicode, got, w.lines)
				}
			}
		}
	}
}

// escapes are the terminal's SGR sequences.
var escapes = regexp.MustCompile("\x1b\\[[0-9;]*m")

// drawn are the heights of the screens out shows, in order: each from its
// title bar to its prompt, which ends " or q]:", the answer echoed after.
func drawn(out string) []int {
	var got []int
	start := 0
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if strings.Contains(l, " or q]:") && strings.Contains(l, "Option [") {
			for start < i && strings.TrimSpace(lines[start]) == "" {
				start++
			}
			got = append(got, i-start+1)
			start = i + 1
		}
	}
	return got
}

// TestApplyContext: each apply runs under the context ApplyContext makes,
// stopped once it returns; plan only makes none. A cancelled apply is rolled
// back, and the screen goes on.
func TestApplyContext(t *testing.T) {
	for _, tc := range []struct {
		dryRun bool
		made   int
	}{{false, 1}, {true, 0}} {
		h := newHost(t, "sudo-password")
		made, stopped := 0, 0
		p := playWith(h, tc.dryRun, func(o *usrscreen.Options) {
			o.ApplyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
				made++
				c, cancel := context.WithCancel(ctx)
				return c, func() { stopped++; cancel() }
			}
		}, "1", "1", "2", "y", "0", "0", "0")
		if p.err != nil || made != tc.made || stopped != made {
			t.Errorf("plan only %v: Run = %v, %d contexts made, %d stopped, want %d:\n%s", tc.dryRun, p.err, made, stopped, tc.made, p.out)
		}
	}
	h := newHost(t, "sudo-password")
	n, _ := sudoers.FileNames(sudoers.ToolSudo, "george")
	before, _ := h.ReadFile(context.Background(), n.Grant)
	p := playWith(h, false, func(o *usrscreen.Options) {
		o.ApplyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
			c, cancel := context.WithCancel(ctx)
			cancel()
			return c, cancel
		}
	}, "1", "1", "2", "y", "0", "0", "0")
	after, _ := h.ReadFile(context.Background(), n.Grant)
	if p.err != nil || len(p.runs) != 1 || p.runs[0].Err == nil || !bytes.Equal(before, after) || !strings.Contains(p.out[strings.LastIndex(p.out, "Stop asking"):], "# Manage User #") {
		t.Errorf("cancelled: Run = %v, runs %+v, %q:\n%s", p.err, p.runs, after, p.out)
	}
}

// TestNavigation: 0 returns, q quits, and the end of the input aborts, from
// the screen, each sub-screen, each question and the form, writing nothing.
func TestNavigation(t *testing.T) {
	for _, tt := range []struct {
		host  string
		lines []string
		want  error
	}{
		{"debian13", []string{"0"}, nil}, {"debian13", []string{"q"}, nav.ErrQuit}, {"debian13", []string{"1", "q"}, nav.ErrQuit},
		{"debian13", []string{"1", "1", "q"}, nav.ErrQuit}, {"debian13", []string{"1", "1", "3", "q"}, nav.ErrQuit},
		{"second-admin", []string{"1", "2", "1", "q"}, nav.ErrQuit}, {"sudo-password", []string{"1", "1", "2", "q"}, nav.ErrQuit},
		{"debian13", []string{"1", "1", "3", "2", "q"}, nav.ErrQuit}, {"debian13", []string{"2", "q"}, nav.ErrQuit}, {"debian13", []string{"2", "carol", "q"}, nav.ErrQuit},
		{"debian13", nil, prompt.ErrAborted}, {"debian13", []string{"1"}, prompt.ErrAborted}, {"debian13", []string{"1", "1"}, prompt.ErrAborted},
		{"debian13", []string{"1", "1", "3"}, prompt.ErrAborted}, {"second-admin", []string{"1", "2", "1"}, prompt.ErrAborted},
		{"debian13", []string{"1", "1", "3", "1"}, prompt.ErrAborted}, {"debian13", []string{"2", "carol"}, prompt.ErrAborted},
		{"debian13", []string{"9", "2", "0", "0"}, nil}, {"debian13", []string{"1", "1", "3", "2", "0", "0", "0", "0", "0"}, nil},
		{"pve9", []string{"1", "0"}, nil},
	} {
		h := newHost(t, tt.host)
		p := play(h, false, tt.lines...)
		if !errors.Is(p.err, tt.want) || (tt.want == nil) != (p.err == nil) || p.unread != 0 {
			t.Errorf("%s %q: Run = %v, want %v, %d unread:\n%s", tt.host, tt.lines, p.err, tt.want, p.unread, p.out)
		}
		if w := writes(h, 0); len(w) > 0 {
			t.Errorf("%s %q: wrote %v", tt.host, tt.lines, w)
		}
	}
}

// TestNotRead: accounts that can't be read never draw an empty screen: the
// control's words where the users would be, and Create says them before
// asking; a user no longer listed says so, Return alone.
func TestNotRead(t *testing.T) {
	h := newHost(t, "debian13")
	_ = h.Remove(context.Background(), "/etc/passwd")
	n := len(h.Calls())
	p := play(h, false, "2", "0")
	if p.err != nil || !shows(p.out, "/etc/passwd") || strings.Contains(p.out, "No user besides root") || asked(p.out) || len(writes(h, n)) > 0 {
		t.Errorf("accounts not read: Run = %v:\n%s", p.err, p.out)
	}
	// A reading after alice's screen is drawn finds alice gone: the screen
	// drawn again after item 1's refusal says so.
	h = newHost(t, "debian13")
	reads := 0
	p = playWith(h, false, func(o *usrscreen.Options) {
		o.Getenv = func(k string) string {
			if k == "SUDO_USER" {
				if reads++; reads == 3 {
					h.dropLine("/etc/passwd", "alice")
				}
			}
			return h.getenv(k)
		}
	}, "1", "2", "1", "0", "0", "0")
	last := p.out[strings.LastIndex(p.out, "Managing: "):]
	last = last[:strings.Index(last, "> Option")]
	if p.err != nil || !shows(last, "alice isn't a user hardn manages here.") || strings.Contains(last, "1) ") || asked(p.out) {
		t.Errorf("alice gone: Run = %v:\n%s", p.err, p.out)
	}
}

// TestRunNeeds: Run refuses options it cannot run with, writing nothing: no
// Prompter's In or Out, no System, Getenv or NewEngine. A nil Config is
// none.
func TestRunNeeds(t *testing.T) {
	h := newHost(t, "debian13")
	var out bytes.Buffer
	in := prompt.New(strings.NewReader("0\n"), false)
	newEngine := func(func(engine.Event)) *engine.Engine { return nil }
	for _, o := range []usrscreen.Options{
		{}, {System: h, Getenv: h.getenv, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in}, System: h, Getenv: h.getenv, NewEngine: newEngine},
		{Prompter: nav.Prompter{Out: &out}, System: h, Getenv: h.getenv, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, Getenv: h.getenv, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, System: h, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, System: h, Getenv: h.getenv},
	} {
		if err := usrscreen.Run(context.Background(), o); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Run(%+v) = %v, want fs.ErrInvalid", o, err)
		}
	}
	if len(h.Calls()) > 0 || out.Len() > 0 {
		t.Errorf("refused options read %d calls, wrote %q", len(h.Calls()), out.String())
	}
}

// TestAlpineLocked: alpine-locked's alice is built as usersOn builds every
// user, so the fake's account and home agree with alice's rows.
func TestAlpineLocked(t *testing.T) {
	h := newHost(t, "alpine-locked")
	ctx := context.Background()
	u, err := h.LookupUser(ctx, "alice")
	fi, serr := h.Stat(ctx, "/home/alice")
	if err != nil || u.UID != 1002 || serr != nil || fi.Mode().Perm() != 0o755 {
		t.Errorf("alice: %+v, %v; home %v, %v", u, err, fi, serr)
	}
}

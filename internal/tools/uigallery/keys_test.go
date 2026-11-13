package main

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/charmbracelet/x/ansi"
)

// The SSH Keys screen's mocks (keys.go): each a fake host's passwd and
// authorized_keys, read and drawn by the real screen (pkg/screens/sshkeys), and
// played on it. Nothing here reads this machine.

// TestKeysGolden pins each mock on the core set and under LANG=C.
func TestKeysGolden(t *testing.T) { goldenListing(t, keysListing) }

// keysMarks are a view's marked keys, "account type bits: marks", and its files
// that could not be read, "account: path".
func keysMarks(v screen.SSHKeys) []string {
	var out []string
	for _, a := range v.Accounts {
		for _, p := range a.Unread {
			out = append(out, a.Name+": "+p)
		}
		for _, l := range a.NotKeys {
			out = append(out, fmt.Sprintf("%s: %d lines of %s", a.Name, l.Lines, l.Path))
		}
		for _, k := range a.Keys {
			var marks []string
			if k.Weak {
				marks = append(marks, "weak")
			}
			if k.Refused {
				marks = append(marks, "refused")
			}
			if len(k.Shared) > 0 {
				marks = append(marks, "shared "+strings.Join(k.Shared, ","))
			}
			if len(marks) > 0 {
				out = append(out, a.Name+" "+k.Type+": "+strings.Join(marks, "; "))
			}
		}
	}
	return out
}

// TestKeysMocks holds each mock to what its note says: how many keys, and which
// are marked and why.
func TestKeysMocks(t *testing.T) {
	deploy := func(name string, others string) string { return name + " ED25519: shared " + others }
	tests := []struct {
		mock  string
		keys  int
		marks []string
	}{
		{"clean", 3, nil},
		{"weak", 6, []string{"root ED25519: shared george", "george ED25519: shared root", "george RSA: weak", "alice DSA: weak; refused"}},
		{"refused", 3, []string{"george RSA: refused", "alice ECDSA: refused"}},
		{"many", 23, []string{"root ED25519: shared george", deploy("root", "george,deploy"), "george ED25519: shared root", "george RSA: weak",
			deploy("george", "root,deploy"), deploy("deploy", "root,george")}},
		{"many-all", 23, nil},
		{"unreadable", 1, []string{"alice: /home/alice/.ssh/authorized_keys"}},
		{"comments", 3, nil},
		{"not-keys", 1, []string{"george: 2 lines of /home/george/.ssh/authorized_keys"}},
		{"not-read", 0, []string{}},
		{"login", 6, nil},
	}
	mocks := keysMocks()
	if len(tests) != len(mocks) {
		t.Fatalf("%d mocks tested, %d mocks", len(tests), len(mocks))
	}
	for i, tt := range tests {
		m := mocks[i]
		if m.name != tt.mock {
			t.Fatalf("mock %d is %s, not %s", i, m.name, tt.mock)
		}
		v := m.view(context.Background())
		n := 0
		for _, a := range v.Accounts {
			n += len(a.Keys)
		}
		if n != tt.keys {
			t.Errorf("%s: %d keys, want %d", tt.mock, n, tt.keys)
		}
		if got := keysMarks(v); tt.marks != nil && !slices.Equal(got, tt.marks) || tt.mock == "clean" && got != nil {
			t.Errorf("%s: marks %q", tt.mock, got)
		}
		if v.NotRead != m.notRead || v.NotRead != (v.Entry() == "Can't read") {
			t.Errorf("%s: not read %t, entry %q", tt.mock, v.NotRead, v.Entry())
		}
		if v.All != m.all || (n > screen.SSHKeysFold) != strings.HasPrefix(m.name, "many") {
			t.Errorf("%s: all %t, %d keys", tt.mock, v.All, n)
		}
	}
	for _, m := range keysListing.mocks() {
		if note := keysListing.note(m); !strings.HasPrefix(note, keysLabel) {
			t.Errorf("%s: %q", m.name, note)
		}
	}
}

// TestKeysRule: the adopted rule, through the mocks' hosts (pkg/screens/sshkeys
// has the screen's own test). An RSA key is weak under 2048 bits and DSA
// always; sshd refuses an RSA key under its RequiredRSASize and a type its
// PubkeyAcceptedAlgorithms leave out, which is not the same thing.
func TestKeysRule(t *testing.T) {
	for _, tt := range []struct {
		line          string
		rsaMin        int
		algos         []string
		weak, refused bool
	}{
		{rsaKey(1024, 1, "a"), 1024, keysAlgos, true, false},
		{rsaKey(2040, 1, "a"), 1024, keysAlgos, true, false},
		{rsaKey(2048, 1, "a"), 1024, keysAlgos, false, false},
		{rsaKey(2048, 1, "a"), 3072, keysAlgos, false, true},
		{rsaKey(1024, 1, "a"), 2048, keysAlgos, true, true},
		{rsaKey(4096, 1, "a"), 1024, []string{"ssh-ed25519"}, false, true},
		{rsaKey(4096, 1, "a"), 1024, []string{"ssh-rsa"}, false, false},
		{dsaKey(1, "a"), 1024, keysAlgos, true, true},
		{dsaKey(1, "a"), 1024, []string{"ssh-dss"}, true, false},
		{synthKey(1, "a"), 1024, keysAlgos, false, false},
		{ecdsaKey("nistp521", 1, "a"), 1024, keysAlgos, false, false},
		{skKey(1, "a"), 1024, []string{"ssh-ed25519"}, false, true},
	} {
		m := keysMock{accounts: []keysAccount{{name: "george", uid: 1000, lines: []string{tt.line}}}, rsaMin: tt.rsaMin, algos: tt.algos}
		v := m.view(context.Background())
		if len(v.Accounts) != 1 || len(v.Accounts[0].Keys) != 1 {
			t.Fatalf("%.40s: %+v", tt.line, v)
		}
		if k := v.Accounts[0].Keys[0]; k.Weak != tt.weak || k.Refused != tt.refused || k.Type == "" {
			t.Errorf("%.20s, RequiredRSASize %d, %v: %+v", tt.line, tt.rsaMin, tt.algos, k)
		}
	}
}

// TestKeysView: a key twice in one file is listed once; a line that is no key is
// no key, and counted; an account with no key has no entry; an RSA key says its bits and an
// ECDSA key its curve's.
func TestKeysView(t *testing.T) {
	k := synthKey(1, "george@laptop-1")
	m := keysMock{accounts: []keysAccount{
		{name: "george", uid: 1000, lines: []string{k, "# a comment", "no key at all", k, rsaKey(3072, 2, "r"), ecdsaKey("nistp521", 3, "e"), `from="192.0.2.1" ` + synthKey(4, "o")}},
		{name: "alice", uid: 1001},
	}}
	v := m.view(context.Background())
	if len(v.Accounts) != 1 || v.Accounts[0].Name != "george" || len(v.Accounts[0].NotKeys) != 1 || v.Accounts[0].NotKeys[0].Lines != 1 {
		t.Fatalf("accounts %+v", v.Accounts)
	}
	var got []string
	for _, k := range v.Accounts[0].Keys {
		got = append(got, k.Type+" "+k.Comment)
		if k.ID == "" || len(k.Shared) != 0 {
			t.Errorf("%+v", k)
		}
	}
	if !slices.Equal(got, []string{"ED25519 george@laptop-1", "RSA r", "ECDSA e", "ED25519 o"}) {
		t.Errorf("keys %q", got)
	}
	if b := []int{v.Accounts[0].Keys[1].Bits, v.Accounts[0].Keys[2].Bits, v.Accounts[0].Keys[0].Bits}; !slices.Equal(b, []int{3072, 521, 0}) {
		t.Errorf("bits %v", b)
	}
}

// TestKeysCommentsSafe: no control character, and no character that draws as
// nothing, reaches the screen from a comment: the view holds them spelled out.
func TestKeysCommentsSafe(t *testing.T) {
	m, _ := findKeysMock("comments")
	v := m.view(context.Background())
	keys := v.Accounts[0].Keys
	if got := keys[0].Comment; got != "^[[2J^[[32mroot@trusted<U+202E>" {
		t.Errorf("comment %q", got)
	}
	if keys[2].Comment != "" || len(keys[1].Comment) != 120 {
		t.Errorf("comments %q, %q", keys[1].Comment, keys[2].Comment)
	}
	var out, errs bytes.Buffer
	if code := run([]string{keysPrefix, "-profile", "plain", "comments"}, nil, offTerminal, &out, &errs); code != exitOK || errs.Len() > 0 {
		t.Fatalf("exit %d, stderr %q", code, errs.String())
	}
	if s := out.String(); strings.ContainsAny(s, "\x1b\u202e") || !strings.Contains(s, "No comment") {
		t.Errorf("drawn raw:\n%q", s)
	}
}

// TestKeysUsage: the listing refuses a mock that is not one, with status 2 and
// nothing on stdout, and play one it does not run the screen on; -list names
// the mocks.
func TestKeysUsage(t *testing.T) {
	for _, args := range [][]string{{keysPrefix, "nope"}, {keysPrefix, "clean", "-ascii"}, {"play", "keys/nope"}, {"play", "keys/login"},
		{"play", "-fail", "1", "keys/weak"}, {"play", "-sequence", "failure", "keys/weak"}} {
		var out, errs bytes.Buffer
		read := false
		code := run(args, func() *prompt.Reader { read = true; return noInput() }, offTerminal, &out, &errs)
		if code != exitUsage || out.Len() != 0 || errs.Len() == 0 || read {
			t.Errorf("%v: exit %d, stdout %q, stderr %q, stdin read %t", args, code, out.String(), errs.String(), read)
		}
	}
	var out, errs bytes.Buffer
	if code := run([]string{keysPrefix, "-list"}, nil, offTerminal, &out, &errs); code != exitOK || errs.Len() > 0 ||
		out.String() != "clean\nweak\nrefused\nmany\nmany-all\nunreadable\ncomments\nnot-keys\nnot-read\nlogin\n" {
		t.Errorf("-list: exit %d, %q, stderr %q", code, out.String(), errs.String())
	}
}

// TestPlayKeys: play runs hardn's SSH Keys screen on a mock's host: folded, 1
// lists every key and 1 again folds them; under plan only the same; the mocks'
// menu leads to each; and SSH Login's 6, on a host whose sshd -T is modelled,
// opens the screen and 0 returns to SSH Login.
func TestPlayKeys(t *testing.T) {
	for _, seq := range [][]string{nil, {"-sequence", "dry-run"}} {
		code, out, errs, unread := playGallery([]string{"1", "1", "0"}, append([]string{"-profile", "plain"}, append(seq, "keys/many")...)...)
		if code != exitOK || errs != "" || unread != 0 {
			t.Fatalf("%v keys/many: exit %d, stderr %q, %d lines unread", seq, code, errs, unread)
		}
		if n := strings.Count(out, "   1) All keys             23 keys\n"); n != 2 || strings.Count(out, "   1) Keys to review       Fold the rest\n") != 1 || strings.Count(out, "carol@host-5") != 1 {
			t.Errorf("%v keys/many:\n%s", seq, out)
		}
	}
	code, out, errs, unread := playGallery([]string{"2", "0", "0"}, "-profile", "plain", "keys")
	if code != exitOK || errs != "" || unread != 0 || !strings.Contains(out, "   2) weak\n") || !strings.Contains(out, "alice@2009 Weak, refused by sshd\n") || strings.Contains(out, ") login") {
		t.Errorf("keys: exit %d, stderr %q, %d lines unread:\n%s", code, errs, unread, out)
	}
	code, out, errs, unread = playGallery([]string{"6", "0", "0"}, "-profile", "plain", "ssh/rsa-small")
	if code != exitOK || errs != "" || unread != 0 || strings.Count(out, "   6) SSH keys             2 keys, 1 to review\n") != 2 ||
		!strings.Contains(out, "george@laptop-1 Weak, refused by sshd\n") {
		t.Errorf("ssh/rsa-small: exit %d, stderr %q, %d lines unread:\n%s", code, errs, unread, out)
	}
}

// TestPlayKeysIsTheListing: play draws each mock as the listing does, both by
// the real screen's reading and mapping: the listing's rows are in play's
// transcript (many-all's after item 1).
func TestPlayKeysIsTheListing(t *testing.T) {
	for _, m := range keysPlayed() {
		lines := []string{"0"}
		if m.all {
			lines = []string{"1", "0"}
		}
		code, out, errs, unread := playGallery(lines, "-profile", "plain", keysPrefix+"/"+m.name)
		if code != exitOK || errs != "" || unread != 0 {
			t.Fatalf("%s: exit %d, stderr %q, %d lines unread", m.name, code, errs, unread)
		}
		drawn := m.view(context.Background()).SubMenu().Render(component.NewCtx(plainOff()))
		cut := "\n   0) Return"
		if strings.Contains(drawn, "\n  Select an option") {
			cut = "\n  Select an option"
		}
		if rows, _, _ := strings.Cut(ansi.Strip(drawn), cut); !strings.Contains(ansi.Strip(out), rows) {
			t.Errorf("%s: play does not hold the listing's screen:\n%s\nplay:\n%s", m.name, rows, out)
		}
	}
}

package sshkeys_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/authkeys"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/screens/sshkeys"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// Nothing here reads this machine or runs a command: every host is hostfake's,
// its passwd and key files written here, and sshd's settings the test's own
// word. Synthetic keys, built here, no one's.

var ctx = context.Background()

// blob is the SSH strings fields after typ (RFC 4251 §5), as base64.
func blob(typ string, fields ...[]byte) string {
	var b []byte
	for _, f := range append([][]byte{[]byte(typ)}, fields...) {
		b = append(binary.BigEndian.AppendUint32(b, uint32(len(f))), f...)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// num is a positive number of n bits, every byte seed's but the first.
func num(n int, seed byte) []byte {
	return append([]byte{0, 0x80 | seed}, bytes.Repeat([]byte{seed}, n/8-1)...)
}

func ed(seed byte, comment string) string {
	return strings.TrimSpace("ssh-ed25519 " + blob("ssh-ed25519", bytes.Repeat([]byte{seed}, 32)) + " " + comment)
}

func rsa(n int, seed byte, comment string) string {
	return "ssh-rsa " + blob("ssh-rsa", []byte{1, 0, 1}, num(n, seed)) + " " + comment
}

func ecdsa(curve string, seed byte, comment string) string {
	n := map[string]int{"nistp256": 65, "nistp384": 97, "nistp521": 133}[curve]
	return "ecdsa-sha2-" + curve + " " + blob("ecdsa-sha2-"+curve, []byte(curve), append([]byte{4}, bytes.Repeat([]byte{seed}, n-1)...)) + " " + comment
}

func sk(seed byte, comment string) string {
	const typ = "sk-ssh-ed25519@openssh.com"
	return typ + " " + blob(typ, bytes.Repeat([]byte{seed}, 32), []byte("ssh:")) + " " + comment
}

func dsa(seed byte, comment string) string {
	return authkeys.DSA + " " + blob(authkeys.DSA, num(1024, seed), num(160, seed), num(1024, seed), num(1024, seed)) + " " + comment
}

// algos are PubkeyAcceptedAlgorithms as a test's sshd has them unless it says
// otherwise: every type but DSA.
var algos = []string{"ssh-ed25519", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521",
	"sk-ssh-ed25519@openssh.com", "sk-ecdsa-sha2-nistp256@openssh.com", "rsa-sha2-512", "rsa-sha2-256"}

// stock are a Debian sshd's settings; fixed gives them as read.
var stock = ssh.KeySettings{Files: []string{".ssh/authorized_keys", ".ssh/authorized_keys2"}, Algorithms: algos}

func fixed(s ssh.KeySettings) sshkeys.Settings {
	return func(context.Context) (ssh.KeySettings, error) { return s, nil }
}

// acct is one account of a test's host: its name, and its files by path, a
// path ending in "/" a directory in the file's place.
type acct struct {
	name  string
	files map[string]string
}

// keysOf is name's ~/.ssh/authorized_keys holding lines.
func keysOf(name string, lines ...string) acct {
	return acct{name, map[string]string{home(name) + "/.ssh/authorized_keys": strings.Join(lines, "\n") + "\n"}}
}

func home(name string) string {
	if name == "root" {
		return "/root"
	}
	return "/home/" + name
}

// hostOf is a host with as in passwd, in order, and their files.
func hostOf(as ...acct) *hostfake.System {
	h := hostfake.New(host.Options{})
	var passwd strings.Builder
	for i, a := range as {
		uid := 999 + i
		if a.name == "root" {
			uid = 0
		}
		fmt.Fprintf(&passwd, "%s:x:%d:%d::%s:/bin/bash\n", a.name, uid, uid, home(a.name))
		for p, data := range a.files {
			if dir, ok := strings.CutSuffix(p, "/"); ok {
				h.AddDir(dir, 0o700, uid, uid)
				continue
			}
			h.AddFile(p, []byte(data), 0o600, uid, uid)
		}
	}
	h.AddFile(accounts.PasswdFile, []byte(passwd.String()), 0o644, 0, 0)
	return h
}

// view is the screen's view of h under s.
func view(h host.System, s ssh.KeySettings) screen.SSHKeys {
	return sshkeys.View(sshkeys.Read(ctx, h, fixed(s)))
}

// marks are a view's keys, "account type bits comment: marks", and its files
// that are not read or hold lines that are no key.
func marks(v screen.SSHKeys) []string {
	var out []string
	for _, a := range v.Accounts {
		for _, p := range a.Unread {
			out = append(out, a.Name+": unread "+p)
		}
		for _, l := range a.NotKeys {
			out = append(out, fmt.Sprintf("%s: %d not keys in %s", a.Name, l.Lines, l.Path))
		}
		for _, k := range a.Keys {
			s := fmt.Sprintf("%s %s %d %s:", a.Name, k.Type, k.Bits, k.Comment)
			if k.Weak {
				s += " weak"
			}
			if k.Refused {
				s += " refused"
			}
			if len(k.Shared) > 0 {
				s += " shared " + strings.Join(k.Shared, ",")
			}
			out = append(out, s)
		}
	}
	return out
}

// played is one run of the screen: what it wrote, what Run returned, how many
// lines were left unread.
type played struct {
	out    string
	err    error
	unread int
}

// play runs the screen on h under settings, plain at 80 columns, the user
// typing lines.
func play(h host.System, settings sshkeys.Settings, lines ...string) played {
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 80, Height: 24})
	ty := uitest.NewTypist(&out, lines...)
	err := sshkeys.Run(ctx, sshkeys.Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		System:   h, Settings: settings,
	})
	return played{out.String(), err, ty.Unread()}
}

// has fails t for each of want that out does not hold.
func has(t *testing.T, name, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("%s: no %q in:\n%s", name, w, out)
		}
	}
}

// readOnly fails t for a call on h that is not a file read.
func readOnly(t *testing.T, h *hostfake.System) {
	t.Helper()
	for _, c := range h.Calls() {
		if c.Op != "read" && c.Op != "scan-lines" && c.Op != "resolve-symlink" {
			t.Errorf("a call that is not a read: %s", c)
		}
	}
}

// TestClean: keys with nothing to review, in passwd's order; an account with
// no key is not listed; the screen says None, has Return alone, and reads only.
func TestClean(t *testing.T) {
	h := hostOf(keysOf("root", ed(1, "root@console")), keysOf("daemon"), acct{name: "bob"},
		keysOf("george", ed(2, "george@laptop-1"), rsa(4096, 3, "george@desk-1"), sk(4, "george@yubikey"), ecdsa("nistp521", 5, "george@desk-2")))
	want := []string{"root ED25519 0 root@console:", "george ED25519 0 george@laptop-1:", "george RSA 4096 george@desk-1:",
		"george ED25519-SK 0 george@yubikey:", "george ECDSA 521 george@desk-2:"}
	if got := marks(view(h, stock)); !slices.Equal(got, want) {
		t.Errorf("view %q", got)
	}
	p := play(h, fixed(stock), "0")
	if p.err != nil || p.unread != 0 {
		t.Errorf("Run = %v, %d unread", p.err, p.unread)
	}
	has(t, "clean", p.out, "# SSH Keys ###", "  Summary ---", "   -  Keys                 5 in 2 accounts\n", "To review            None\n",
		"  root ---", "  george ---", "   -  RSA 4096             george@desk-1\n", "   0) Return", "[0 or q]:")
	for _, w := range []string{"Weak", "Shared", "Refused", "Not read", "Not a key", "Can't read", "1)", "daemon", "bob"} {
		if strings.Contains(p.out, w) {
			t.Errorf("%q in:\n%s", w, p.out)
		}
	}
	readOnly(t, h)
}

// TestRule: the adopted rule. An RSA key is weak under 2048 bits and DSA
// always, no other type ever; sshd refuses an RSA key under its
// RequiredRSASize, 1024 where it prints none, and a type its
// PubkeyAcceptedAlgorithms leave out, none where it prints no such line.
func TestRule(t *testing.T) {
	for _, tt := range []struct {
		line          string
		rsaMin        int
		algos         []string
		weak, refused bool
	}{
		{rsa(1024, 1, "a"), 0, algos, true, false},
		{rsa(2040, 1, "a"), 1024, algos, true, false},
		{rsa(2048, 1, "a"), 1024, algos, false, false},
		{rsa(2048, 1, "a"), 3072, algos, false, true},
		{rsa(1024, 1, "a"), 2048, algos, true, true},
		{rsa(768, 1, "a"), 0, algos, true, true},
		{rsa(768, 1, "a"), 512, algos, true, false},
		{rsa(4096, 1, "a"), 0, []string{"ssh-ed25519"}, false, true},
		{rsa(4096, 1, "a"), 0, []string{"ssh-rsa"}, false, false},
		{rsa(4096, 1, "a"), 0, nil, false, false},
		{dsa(1, "a"), 0, algos, true, true},
		{dsa(1, "a"), 0, []string{"ssh-dss"}, true, false},
		{dsa(1, "a"), 0, nil, true, false},
		{`from="192.0.2.1" ` + dsa(1, "a"), 0, algos, true, true},
		{ed(1, "a"), 0, algos, false, false},
		{ecdsa("nistp256", 1, "a"), 0, algos, false, false},
		{ecdsa("nistp256", 1, "a"), 0, []string{"ssh-ed25519", "ecdsa-sha2-nistp384"}, false, true},
		{sk(1, "a"), 0, []string{"ssh-ed25519"}, false, true},
	} {
		v := view(hostOf(keysOf("george", tt.line)), ssh.KeySettings{Files: stock.Files, Algorithms: tt.algos, RSAMin: tt.rsaMin})
		if len(v.Accounts) != 1 || len(v.Accounts[0].Keys) != 1 {
			t.Fatalf("%.30s: %+v", tt.line, v)
		}
		if k := v.Accounts[0].Keys[0]; k.Weak != tt.weak || k.Refused != tt.refused || k.Type == "" || k.Comment != "a" {
			t.Errorf("%.30s, RequiredRSASize %d, %v: %+v", tt.line, tt.rsaMin, tt.algos, k)
		}
	}
}

// TestMarked: the weak host. A key two accounts hold is marked on each copy and
// counted once, whatever its comment; a weak key and one sshd refuses are
// marked; the screen draws each with its words.
func TestMarked(t *testing.T) {
	h := hostOf(keysOf("root", ed(1, "george@laptop-1")),
		keysOf("george", ed(1, "my laptop"), rsa(1024, 2, "george@old-laptop"), rsa(2048, 3, "george@desk-1")),
		keysOf("alice", dsa(4, "alice@2009"), ecdsa("nistp384", 5, "alice@laptop-2")),
		keysOf("deploy", ed(1, ""), ed(6, "deploy@ci-1")))
	want := []string{"root ED25519 0 george@laptop-1: shared george,deploy", "george ED25519 0 my laptop: shared root,deploy",
		"george RSA 1024 george@old-laptop: weak", "george RSA 2048 george@desk-1:", "alice DSA 0 alice@2009: weak refused",
		"alice ECDSA 384 alice@laptop-2:", "deploy ED25519 0 : shared root,george", "deploy ED25519 0 deploy@ci-1:"}
	v := view(h, stock)
	if got := marks(v); !slices.Equal(got, want) {
		t.Errorf("view %q", got)
	}
	if e := v.Entry(); e != "8 keys, 5 to review" {
		t.Errorf("entry %q", e)
	}
	p := play(h, fixed(stock), "0")
	has(t, "marked", p.out, "   -  Keys                 8 in 4 accounts\n", "  [ ] Weak                 2 keys\n", "  [ ] Shared               1 key\n",
		"   -  Refused by sshd      1 key\n", "  [ ] ED25519              my laptop Shared with 3 accounts\n",
		"  [ ] RSA 1024             george@old-laptop Weak\n", "  [ ] DSA                  alice@2009 Weak, refused by sshd\n",
		"  [ ] ED25519              No comment Shared with 3 accounts\n")
	refused := view(hostOf(keysOf("george", ed(1, "g"), rsa(2048, 2, "r")), keysOf("alice", ecdsa("nistp256", 3, "e"))),
		ssh.KeySettings{Files: stock.Files, Algorithms: []string{"ssh-ed25519", "rsa-sha2-512"}, RSAMin: 3072})
	if got := marks(refused); !slices.Equal(got, []string{"george ED25519 0 g:", "george RSA 2048 r: refused", "alice ECDSA 256 e: refused"}) {
		t.Errorf("refused %q", got)
	}
}

// TestFiles: the files are AuthorizedKeysFile's, tokens expanded, for every
// account; a key in two of an account's files, or twice in one, is listed
// once; "none" names no file; a file hardn does not name is not read.
func TestFiles(t *testing.T) {
	k := ed(1, "george@laptop-1")
	h := hostOf(acct{"george", map[string]string{
		"/home/george/.ssh/authorized_keys":  k + "\n" + k + "\n# a comment\n\n",
		"/home/george/.ssh/authorized_keys2": k + "\n" + ed(2, "second") + "\n",
		"/etc/ssh/keys/george":               ed(3, "central") + "\n",
	}})
	if got := marks(view(h, stock)); !slices.Equal(got, []string{"george ED25519 0 george@laptop-1:", "george ED25519 0 second:"}) {
		t.Errorf("stock %q", got)
	}
	central := ssh.KeySettings{Files: []string{"/etc/ssh/keys/%u", "none", "%h/.ssh/authorized_keys2"}, Algorithms: algos}
	if got := marks(view(h, central)); !slices.Equal(got, []string{"george ED25519 0 central:", "george ED25519 0 george@laptop-1:", "george ED25519 0 second:"}) {
		t.Errorf("central %q", got)
	}
	if v := view(h, ssh.KeySettings{Files: []string{"none"}}); len(v.Accounts) != 0 || v.NotRead || v.Entry() != "0 keys" {
		t.Errorf("none: %+v", v)
	}
	p := play(h, fixed(ssh.KeySettings{Files: []string{"none"}}), "0")
	has(t, "none", p.out, "   -  Keys                 None\n")
}

// TestUnreadable: a file that can't be read is a row of its account with its
// path, counted, the account's other keys still listed; an account whose home
// is not a path has no file read.
func TestUnreadable(t *testing.T) {
	h := hostOf(keysOf("george", ed(1, "george@laptop-1")),
		acct{"alice", map[string]string{"/home/alice/.ssh/authorized_keys/": "", "/home/alice/.ssh/authorized_keys2": ed(2, "alice@laptop-2") + "\n"}})
	v := view(h, stock)
	want := []string{"george ED25519 0 george@laptop-1:", "alice: unread /home/alice/.ssh/authorized_keys", "alice ED25519 0 alice@laptop-2:"}
	if got := marks(v); !slices.Equal(got, want) || v.Entry() != "2 keys, 1 file not read" {
		t.Errorf("view %q, entry %q", got, v.Entry())
	}
	p := play(h, fixed(stock), "0")
	has(t, "unreadable", p.out, "  [ ] Not read             1 file\n", "  [ ] Can't read           /home/alice/.ssh/authorized_keys\n", "   -  ED25519              alice@laptop-2\n")
	if strings.Contains(p.out, "To review") {
		t.Errorf("To review in:\n%s", p.out)
	}
	nohome := hostfake.New(host.Options{})
	nohome.AddFile(accounts.PasswdFile, []byte("svc:x:500:500:::/bin/sh\n"), 0o644, 0, 0)
	if got := marks(view(nohome, stock)); !slices.Equal(got, []string{"svc: unread .ssh/authorized_keys", "svc: unread .ssh/authorized_keys2"}) {
		t.Errorf("no home %q", got)
	}
	readOnly(t, nohome)
	for _, c := range nohome.Calls() {
		if c.Path != accounts.PasswdFile {
			t.Errorf("read %s", c.Path)
		}
	}
}

// TestNotKeys: lines that are no key, a file that is no text among them, are
// counted for their file and shown, the file's keys still listed.
func TestNotKeys(t *testing.T) {
	h := hostOf(acct{"george", map[string]string{
		"/home/george/.ssh/authorized_keys":  ed(1, "george@laptop-1") + "\nnot a key\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5 cut@short\n# fine\n",
		"/home/george/.ssh/authorized_keys2": "\x7fELF\x02\x01\x01\x00\n",
	}})
	v := view(h, stock)
	want := []string{"george: 2 not keys in /home/george/.ssh/authorized_keys", "george: 1 not keys in /home/george/.ssh/authorized_keys2", "george ED25519 0 george@laptop-1:"}
	if got := marks(v); !slices.Equal(got, want) || v.Entry() != "1 key" {
		t.Errorf("view %q, entry %q", got, v.Entry())
	}
	p := play(h, fixed(stock), "0")
	has(t, "not keys", p.out, "  [ ] Not a key            3 lines\n", "  [ ] Not a key\n      /home/george/.ssh/authorized_keys 2 lines\n",
		"  [ ] Not a key\n      /home/george/.ssh/authorized_keys2 1 line\n")
}

// TestNotRead: with sshd's settings not read (no sshd, a configuration it
// refuses), or passwd missing or not in its form, the keys are not known: the
// screen says so, lists none and assumes no file.
func TestNotRead(t *testing.T) {
	good := hostOf(keysOf("george", ed(1, "george@laptop-1")))
	noSSHD := func(context.Context) (ssh.KeySettings, error) {
		return ssh.KeySettings{}, fmt.Errorf("host: run sshd: %w", host.ErrNotFound)
	}
	noPasswd := hostfake.New(host.Options{})
	noPasswd.AddFile("/home/george/.ssh/authorized_keys", []byte(ed(1, "g")+"\n"), 0o600, 1000, 1000)
	badPasswd := hostOf(keysOf("george", ed(1, "g")))
	badPasswd.AddFile(accounts.PasswdFile, []byte("george:x:1000:1000::/home/george:/bin/bash\nnot a line\n"), 0o644, 0, 0)
	for _, tt := range []struct {
		name     string
		h        *hostfake.System
		settings sshkeys.Settings
		is       error
	}{
		{"no sshd", good, noSSHD, host.ErrNotFound},
		{"no passwd", noPasswd, fixed(stock), fs.ErrNotExist},
		{"a passwd line not in its form", badPasswd, fixed(stock), accounts.ErrMalformed},
	} {
		r := sshkeys.Read(ctx, tt.h, tt.settings)
		v := sshkeys.View(r)
		if !errors.Is(r.Err, tt.is) || !v.NotRead || len(v.Accounts) != 0 || v.Entry() != "Can't read" {
			t.Errorf("%s: %v, %+v", tt.name, r.Err, v)
		}
		p := play(tt.h, tt.settings, "0")
		if p.err != nil || p.unread != 0 {
			t.Errorf("%s: Run = %v, %d unread", tt.name, p.err, p.unread)
		}
		has(t, tt.name, p.out, "  [?] Keys                 Can't read\n", "   0) Return")
		if strings.Contains(p.out, "george") || strings.Contains(p.out, "None") {
			t.Errorf("%s:\n%s", tt.name, p.out)
		}
	}
	if len(good.Calls()) != 0 {
		t.Errorf("with no settings, read %v", good.Calls())
	}
}

// TestUntrusted: a comment, an account's name and a path are the host's text:
// an escape sequence, a bidi override and a byte that is no UTF-8 are spelled
// out before the screen draws them, and a long comment is cut; a key's options
// are not shown.
func TestUntrusted(t *testing.T) {
	h := hostfake.New(host.Options{})
	h.AddFile(accounts.PasswdFile, []byte("ge\x1b[2Jorge:x:1000:1000::/home/ge\x1b]0;x\x07orge:/bin/bash\n"), 0o644, 0, 0)
	h.AddFile("/home/ge\x1b]0;x\x07orge/.ssh/authorized_keys", []byte(strings.Join([]string{
		ed(1, "\x1b[2J\x1b[32mroot@trusted\u202e"),
		ed(2, strings.Repeat("long-comment-", 9)+"end"),
		ed(3, ""),
		`command="\x1b[31mrm -rf /",from="192.0.2.1" ` + ed(4, "caf\xe9"),
		"\x1b[31mnot a key",
	}, "\n")), 0o600, 1000, 1000)
	h.AddDir("/home/ge\x1b]0;x\x07orge/.ssh/authorized_keys2", 0o700, 1000, 1000)
	v := view(h, stock)
	if len(v.Accounts) != 1 || len(v.Accounts[0].Keys) != 4 {
		t.Fatalf("view %+v", v)
	}
	a := v.Accounts[0]
	if a.Name != "ge^[[2Jorge" || a.Unread[0] != "/home/ge^[]0;x^Gorge/.ssh/authorized_keys2" || a.NotKeys[0].Path != "/home/ge^[]0;x^Gorge/.ssh/authorized_keys" {
		t.Errorf("account %+v", a)
	}
	if got := []string{a.Keys[0].Comment, a.Keys[2].Comment, a.Keys[3].Comment}; !slices.Equal(got, []string{"^[[2J^[[32mroot@trusted<U+202E>", "", "caf<0xe9>"}) {
		t.Errorf("comments %q", got)
	}
	p := play(h, fixed(stock), "0")
	if strings.ContainsAny(p.out, "\x1b\u202e\x07") || !utf8.ValidString(p.out) || strings.Contains(p.out, "rm -rf") || strings.Contains(p.out, "192.0.2.1") {
		t.Errorf("drawn raw:\n%q", p.out)
	}
	has(t, "untrusted", p.out, "No comment", "^[[2J^[[32mroot@trusted<U+202E>", "ge^[[2Jorge")
	for _, l := range strings.Split(p.out, "\n") {
		if len([]rune(l)) > 80 {
			t.Errorf("a line over 80 columns: %q", l)
		}
	}
}

// many is a host with more keys than the screen lists whole: george's weak key
// and the key he shares with root, and alice's and bob's with nothing to review.
func many() *hostfake.System {
	var alice, bob []string
	for i := range 6 {
		alice = append(alice, ed(byte(0x70+i), fmt.Sprintf("alice@host-%d", i+1)))
	}
	for i := range 4 {
		bob = append(bob, ed(byte(0x60+i), fmt.Sprintf("bob@host-%d", i+1)))
	}
	return hostOf(keysOf("root", ed(1, "george@laptop-1")), keysOf("george", ed(1, "george@laptop-1"), rsa(1024, 2, "george@old-laptop"), sk(3, "george@yubikey")),
		keysOf("alice", alice...), keysOf("bob", bob...))
}

// TestFolded: over ten keys the screen opens folded, on the keys to review; 1
// lists every key, 1 again folds them, each draw a fresh reading; q quits.
func TestFolded(t *testing.T) {
	p := play(many(), fixed(stock), "1", "1", "0")
	if p.err != nil || p.unread != 0 {
		t.Fatalf("Run = %v, %d unread", p.err, p.unread)
	}
	draws := strings.Split(p.out, "# SSH Keys ###")[1:]
	if len(draws) != 3 {
		t.Fatalf("%d draws:\n%s", len(draws), p.out)
	}
	for i, d := range draws {
		folded := i != 1
		has(t, fmt.Sprintf("draw %d", i), d, "   -  Keys                 14 in 4 accounts\n", "  [ ] RSA 1024             george@old-laptop Weak\n", "[0-1 or q]:")
		for w, want := range map[string]bool{
			"   -  1 more key\n": folded, "  Nothing to review ---": folded, "   -  alice                6 keys\n": folded, "   1) All keys": folded,
			"alice@host-6": !folded, "bob@host-1": !folded, "george@yubikey": !folded, "   1) Keys to review": !folded, "Fold the rest": !folded,
		} {
			if strings.Contains(d, w) != want {
				t.Errorf("draw %d: %q there %t, want %t:\n%s", i, w, !want, want, d)
			}
		}
	}
	if p := play(many(), fixed(stock), "q", "0"); !errors.Is(p.err, nav.ErrQuit) || p.unread != 1 {
		t.Errorf("q: Run = %v, %d unread", p.err, p.unread)
	}
	if p := play(many(), fixed(stock)); !errors.Is(p.err, prompt.ErrAborted) {
		t.Errorf("the end of the input: Run = %v", p.err)
	}
	if p := play(hostOf(keysOf("george", ed(1, "g"))), fixed(stock), "1", "0"); p.err != nil || p.unread != 0 || strings.Count(p.out, "# SSH Keys ###") != 1 {
		t.Errorf("1 with no item: Run = %v, %d unread:\n%s", p.err, p.unread, p.out)
	}
}

// TestFreshReading: each draw reads the host again, settings and files.
func TestFreshReading(t *testing.T) {
	h := many()
	n := 0
	settings := func(context.Context) (ssh.KeySettings, error) {
		if n++; n == 2 {
			h.AddFile("/home/bob/.ssh/authorized_keys", []byte(dsa(9, "bob@2009")+"\n"), 0o600, 1002, 1002)
		}
		return stock, nil
	}
	p := play(h, settings, "1", "0")
	if p.err != nil || n != 2 {
		t.Fatalf("Run = %v, %d readings", p.err, n)
	}
	has(t, "second draw", p.out, "   -  Keys                 14 in 4 accounts\n", "   -  Keys                 11 in 4 accounts\n", "  [ ] DSA                  bob@2009 Weak, refused by sshd\n")
	readOnly(t, h)
}

// TestInvalid: Run refuses, writing nothing, without a Prompter's In and Out,
// a System or Settings.
func TestInvalid(t *testing.T) {
	var out bytes.Buffer
	in := prompt.New(uitest.NewTypist(&out, "0"), false)
	h := hostOf()
	for name, o := range map[string]sshkeys.Options{
		"no In":       {Prompter: nav.Prompter{Out: &out}, System: h, Settings: fixed(stock)},
		"no Out":      {Prompter: nav.Prompter{In: in}, System: h, Settings: fixed(stock)},
		"no System":   {Prompter: nav.Prompter{In: in, Out: &out}, Settings: fixed(stock)},
		"no Settings": {Prompter: nav.Prompter{In: in, Out: &out}, System: h},
	} {
		if err := sshkeys.Run(ctx, o); !errors.Is(err, fs.ErrInvalid) || out.Len() != 0 {
			t.Errorf("%s: Run = %v, wrote %q", name, err, out.String())
		}
	}
}

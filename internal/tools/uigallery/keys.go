package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"path"
	"strings"

	sshctl "github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/authkeys"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/screens/sshkeys"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The SSH Keys screen's mocks (the roadmap's "SSH key review";
// docs/engine/gallery-keys.md, adopted 2026-11-13; screen-keys.md): read only.
// "uigallery keys" lists them and "play keys" runs the real screen
// (pkg/screens/sshkeys) on one. Each mock is a host's passwd and its accounts'
// authorized_keys, read by the screen's own reading (sshkeys.Read) from a fake
// host that holds those files alone, and drawn through its mapping
// (sshkeys.View). What sshd says of the keys it takes (AuthorizedKeysFile,
// RequiredRSASize, PubkeyAcceptedAlgorithms) is the mock's own word: no sshd -T
// is modelled here; "play ssh/<host>", item 6, runs the screen over one that is.

// keysPrefix names the listing and its goldens.
const keysPrefix = "keys"

// keysLabel is the source label every mock's note opens with.
const keysLabel = "Proposed: no Figma frame."

// keysAlgos are PubkeyAcceptedAlgorithms as a mock's sshd has them unless it
// says otherwise: every type authkeys.ParseLine takes, and no DSA.
var keysAlgos = []string{"ssh-ed25519", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521",
	"sk-ssh-ed25519@openssh.com", "sk-ecdsa-sha2-nistp256@openssh.com", "rsa-sha2-512", "rsa-sha2-256"}

// keysAccount is one account of a mock and its ~/.ssh/authorized_keys.
type keysAccount struct {
	name string
	uid  int
	// lines are the file's; unreadable puts a directory in the file's place,
	// which the fake host can't read as a file.
	lines      []string
	unreadable bool
}

// passwd is the account's line of /etc/passwd.
func (a keysAccount) passwd() string {
	u := a.user()
	return fmt.Sprintf("%s:x:%d:%d::%s:/bin/bash\n", u.Name, u.UID, u.GID, u.Home)
}

// user is the account as passwd has it: root in /root, the others under /home.
func (a keysAccount) user() accounts.User {
	home := "/home/" + a.name
	if a.uid == 0 {
		home = "/root"
	}
	return accounts.User{Name: a.name, UID: a.uid, GID: a.uid, Home: home}
}

// keysMock is one host's keys and what its sshd asks of a key.
type keysMock struct {
	name, note string
	accounts   []keysAccount
	// rsaMin is sshd's RequiredRSASize, 0 for its default; algos its
	// PubkeyAcceptedAlgorithms, nil for keysAlgos.
	rsaMin int
	algos  []string
	// notRead: sshd's settings are not read (sshd -T failed).
	notRead bool
	// all: the screen after its item All keys.
	all bool
	// login: draw SSH Login with the item that opens the screen, not the screen.
	login bool
}

// ecdsaKey is a synthetic ecdsa-sha2-<curve> public key line, its point's bytes
// seed's: no one's key, and no point on the curve, which authkeys does not check.
func ecdsaKey(curve string, seed byte, comment string) string {
	typ := "ecdsa-sha2-" + curve
	n := map[string]int{"nistp256": 65, "nistp384": 97, "nistp521": 133}[curve]
	blob := binary.BigEndian.AppendUint32(nil, uint32(len(typ)))
	blob = append(blob, typ...)
	blob = binary.BigEndian.AppendUint32(blob, uint32(len(curve)))
	blob = append(blob, curve...)
	blob = binary.BigEndian.AppendUint32(blob, uint32(n))
	blob = append(blob, 4)
	for range n - 1 {
		blob = append(blob, seed)
	}
	return typ + " " + base64.StdEncoding.EncodeToString(blob) + " " + comment
}

// skKey is a synthetic sk-ssh-ed25519@openssh.com public key line, a FIDO key's
// form (OpenSSH PROTOCOL.u2f): the 32 bytes seed's, the application "ssh:".
func skKey(seed byte, comment string) string {
	const typ = "sk-ssh-ed25519@openssh.com"
	blob := binary.BigEndian.AppendUint32(nil, uint32(len(typ)))
	blob = append(blob, typ...)
	blob = binary.BigEndian.AppendUint32(blob, 32)
	for range 32 {
		blob = append(blob, seed)
	}
	blob = binary.BigEndian.AppendUint32(blob, 4)
	blob = append(blob, "ssh:"...)
	return typ + " " + base64.StdEncoding.EncodeToString(blob) + " " + comment
}

// dsaKey is a synthetic ssh-dss public key line (RFC 4253 §6.6: p, q, g, y),
// each number's bytes seed's: no one's key.
func dsaKey(seed byte, comment string) string {
	blob := binary.BigEndian.AppendUint32(nil, uint32(len(authkeys.DSA)))
	blob = append(blob, authkeys.DSA...)
	for _, n := range []int{128, 20, 128, 128} {
		blob = binary.BigEndian.AppendUint32(blob, uint32(n+1))
		blob = append(blob, 0, 0x80|seed)
		for range n - 1 {
			blob = append(blob, seed)
		}
	}
	return authkeys.DSA + " " + base64.StdEncoding.EncodeToString(blob) + " " + comment
}

// keysManyAccounts are the many-keys host's: 23 keys in six accounts, the deploy
// key held by three of them, george's old RSA key weak.
func keysManyAccounts() []keysAccount {
	deploy := synthKey(0x60, "deploy@ci-1")
	var alice, carol, bob []string
	for i := range 6 {
		alice = append(alice, synthKey(byte(0x70+i), "alice@host-"+string(rune('1'+i))))
	}
	for i := range 5 {
		carol = append(carol, synthKey(byte(0x80+i), "carol@host-"+string(rune('1'+i))))
	}
	for i := range 4 {
		bob = append(bob, ecdsaKey("nistp256", byte(0x90+i), "bob@host-"+string(rune('1'+i))))
	}
	return []keysAccount{
		{name: "root", uid: 0, lines: []string{synthKey(0x61, "george@laptop-1"), deploy}},
		{name: "george", uid: 1000, lines: []string{synthKey(0x61, "george@laptop-1"), rsaKey(1024, 0x62, "george@old-laptop"), skKey(0x63, "george@yubikey"), deploy}},
		{name: "alice", uid: 1001, lines: alice},
		{name: "bob", uid: 1002, lines: bob},
		{name: "carol", uid: 1003, lines: carol},
		{name: "deploy", uid: 1004, lines: []string{deploy, rsaKey(4096, 0x64, "deploy@ci-2")}},
	}
}

// keysMocks are the mocks, in the listing's order.
func keysMocks() []keysMock {
	laptop := synthKey(0x41, "george@laptop-1")
	weak := []keysAccount{
		{name: "root", uid: 0, lines: []string{laptop}},
		{name: "george", uid: 1000, lines: []string{laptop, rsaKey(1024, 0x42, "george@old-laptop"), rsaKey(2048, 0x43, "george@desk-1")}},
		{name: "alice", uid: 1001, lines: []string{dsaKey(0x45, "alice@2009"), ecdsaKey("nistp384", 0x44, "alice@laptop-2")}},
	}
	return []keysMock{
		{name: "clean", note: "Three keys in two accounts, none weak, none shared.",
			accounts: []keysAccount{
				{name: "root", uid: 0, lines: []string{synthKey(0x31, "root@console")}},
				{name: "george", uid: 1000, lines: []string{synthKey(0x32, "george@laptop-1"), rsaKey(4096, 0x33, "george@desk-1")}},
			}},
		{name: "weak", accounts: weak,
			note: "george's laptop key is root's too; his RSA 1024 key and alice's DSA key are weak, and sshd refuses DSA. His RSA 2048 key is not marked: the rule drawn is under 2048."},
		{name: "refused", rsaMin: 3072, algos: []string{"ssh-ed25519", "rsa-sha2-512", "rsa-sha2-256"},
			note: "sshd asks RequiredRSASize 3072 and takes no ECDSA key: george's RSA 2048 key and alice's ECDSA key can't log in. Neither is weak.",
			accounts: []keysAccount{
				{name: "george", uid: 1000, lines: []string{synthKey(0x51, "george@laptop-1"), rsaKey(2048, 0x52, "george@desk-1")}},
				{name: "alice", uid: 1001, lines: []string{ecdsaKey("nistp256", 0x53, "alice@laptop-2")}},
			}},
		{name: "many", accounts: keysManyAccounts(),
			note: "23 keys in six accounts, more than 10: the screen opens folded, on the keys to review. The deploy key is held by three accounts."},
		{name: "many-all", accounts: keysManyAccounts(), all: true,
			note: "The same host after 1, All keys: every key, an account's keys to review first."},
		{name: "unreadable", note: "alice's authorized_keys can't be read (here a directory is in its place), so her keys are not known.",
			accounts: []keysAccount{
				{name: "george", uid: 1000, lines: []string{synthKey(0x54, "george@laptop-1")}},
				{name: "alice", uid: 1001, unreadable: true},
			}},
		{name: "comments", note: "Comments are untrusted: one holds an escape sequence and a right-to-left override, one is 120 letters long, one is missing.",
			accounts: []keysAccount{
				{name: "george", uid: 1000, lines: []string{
					synthKey(0x55, "\x1b[2J\x1b[32mroot@trusted‮"),
					synthKey(0x56, strings.Repeat("long-comment-", 9)+"end"),
					strings.TrimSpace(synthKey(0x57, "")),
				}},
			}},
		{name: "not-keys", note: "george's authorized_keys has two lines that are no key (a note, and a key cut short): they are counted and the file named. His key is still listed.",
			accounts: []keysAccount{
				{name: "george", uid: 1000, lines: []string{"george's keys, added 2026", synthKey(0x58, "george@laptop-1"), "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 george@desk-1"}},
			}},
		{name: "not-read", notRead: true, note: "sshd -T can't be read, so the files sshd takes keys from are not known: no key is listed and none is assumed.",
			accounts: []keysAccount{
				{name: "george", uid: 1000, lines: []string{synthKey(0x59, "george@laptop-1")}},
			}},
		{name: "login", accounts: weak, login: true,
			note: "SSH Login on the weak host, with the item that opens the screen: 6, SSH keys."},
	}
}

// host is the mock's host: /etc/passwd with its accounts, and each one's
// ~/.ssh/authorized_keys, nothing else.
func (m keysMock) host() *hostfake.System {
	f := hostfake.New(host.Options{})
	var passwd strings.Builder
	for _, a := range m.accounts {
		u := a.user()
		passwd.WriteString(a.passwd())
		p := authkeys.Path(authkeys.Planned, u)
		f.AddDir(path.Dir(p), 0o700, u.UID, u.GID)
		if a.unreadable {
			f.AddDir(p, 0o700, u.UID, u.GID)
		} else {
			f.AddFile(p, []byte(strings.Join(a.lines, "\n")+"\n"), 0o600, u.UID, u.GID)
		}
	}
	f.AddFile(accounts.PasswdFile, []byte(passwd.String()), 0o644, 0, 0)
	return f
}

// settings are what the mock's sshd says of the keys it takes: the one file
// hardn writes, the mock's RequiredRSASize and algorithms; an error where the
// mock's sshd -T is not read.
func (m keysMock) settings(context.Context) (sshctl.KeySettings, error) {
	if m.notRead {
		return sshctl.KeySettings{}, errors.New("sshd -T: not read")
	}
	algos := m.algos
	if algos == nil {
		algos = keysAlgos
	}
	return sshctl.KeySettings{Files: []string{authkeys.Planned}, Algorithms: algos, RSAMin: m.rsaMin}, nil
}

// view is the mock's screen, by the real screen's reading and mapping
// (sshkeys.Read, sshkeys.View).
func (m keysMock) view(ctx context.Context) screen.SSHKeys {
	v := sshkeys.View(sshkeys.Read(ctx, m.host(), m.settings))
	v.All = m.all
	return v
}

// keysLogin is SSH Login as a hardened host draws it, with the item that opens
// the SSH Keys screen on v: the rows are a mock's own, no host read.
func keysLogin(v screen.SSHKeys) screen.SSH {
	return screen.SSH{
		Status: component.StatusList{Rows: []component.StatusRow{
			{Label: "SSH Login", State: component.StatusConfigured, Detail: "root disallowed"},
			{Label: "SSH Auth", State: component.StatusConfigured, Detail: "key-only auth"},
			{Label: "SSH Port", State: component.StatusConfigured, Detail: "non-default (2230)"},
		}},
		Rows: []component.KeyValue{
			{Key: screen.SSHRootKey, Value: "Disallowed"},
			{Key: screen.SSHPasswordKey, Value: "Off"},
			{Key: screen.SSHPortKey, Value: "2230"},
			{Key: screen.SSHUsersKey, Value: "Anyone"},
		},
		Root: screen.SSHAllowRootKeyOnly, PasswordsOff: true, Ports: "2230",
		Keys: v.Entry(),
	}
}

// keysListMocks are the listing's mocks: each mock's screen.
func keysListMocks() []listMock {
	var out []listMock
	for _, m := range keysMocks() {
		out = append(out, listMock{name: m.name, note: m.note, draw: func(ctx context.Context, c component.Ctx) (string, error) {
			v := m.view(ctx)
			if m.login {
				return keysLogin(v).SubMenu().Render(c), nil
			}
			return v.SubMenu().Render(c), nil
		}})
	}
	return out
}

// keysUsage is the listing's usage line.
const keysUsage = "usage: go run ./internal/tools/uigallery keys [flags] [mock | all]..."

// keysListing is the SSH Keys listing.
var keysListing = listing{prefix: keysPrefix, usage: keysUsage, label: keysLabel, mocks: keysListMocks}

// keysPlayed are the mocks play runs the screen on: all but login, which is SSH
// Login as drawn and no host ("play ssh/<host>" is SSH Login, its 6 the screen).
func keysPlayed() []keysMock {
	var out []keysMock
	for _, m := range keysMocks() {
		if !m.login {
			out = append(out, m)
		}
	}
	return out
}

// openKeys returns the action that opens what name names: the mocks' menu for
// keysPrefix alone, the screen on one mock for "keys/<mock>".
func (s *session) openKeys(name string) func() error {
	if m, ok := findKeysMock(strings.TrimPrefix(name, keysPrefix+"/")); ok && !m.login && name != keysPrefix {
		return func() error { return s.keysScreen(context.Background(), m) }
	}
	return func() error {
		menu := hostsMenu("SSH Keys hosts", func(add func(label, about string, action func() error)) {
			for _, m := range keysPlayed() {
				add(m.name, "", func() error { return s.keysScreen(context.Background(), m) })
			}
		})
		return s.p.Run(func() nav.Screen { return menu.vm.Screen(s.p.Ctx, menu.menu) })
	}
}

// keysScreen runs hardn's SSH Keys screen on m's host until the user goes back
// from it, its note above it, opening on every key for a mock that shows them
// (many-all is many: item 1 lists them). sshd's settings are the mock's own word.
// Nothing is written or run; the plan-only sequence draws the same screen, as
// Options has no mode.
func (s *session) keysScreen(ctx context.Context, m keysMock) error {
	if err := s.writeNote(keysPrefix+"/"+m.name, m.note); err != nil {
		return err
	}
	return sshkeys.Run(ctx, sshkeys.Options{Prompter: s.p, System: m.host(), Settings: m.settings})
}

// keysStart returns the mocks' menu, or the mock, that args name ("keys",
// "keys/<mock>"), and whether they name one; an error for a mock that is not one.
func keysStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != keysPrefix && !strings.HasPrefix(args[0], keysPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], keysPrefix+"/"); args[0] != keysPrefix {
		if m, ok := findKeysMock(name); !ok || m.login {
			var names []string
			for _, m := range keysPlayed() {
				names = append(names, keysPrefix+"/"+m.name)
			}
			return "", false, fmt.Errorf("no keys mock %q; the mocks are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}

// keysNote is play's note for the SSH Keys screen.
const keysNote = "Play mode: hardn's SSH Keys screen (pkg/screens/sshkeys), proposed with no Figma frame, on a fake host's passwd and authorized_keys; what sshd says of the keys is the mock's own word. " +
	"It only shows: no plan, no y/N, no journal. Nothing on this machine changes or is read."

// findKeysMock returns the mock named name.
func findKeysMock(name string) (keysMock, bool) {
	for _, m := range keysMocks() {
		if m.name == name {
			return m, true
		}
	}
	return keysMock{}, false
}

package users_test

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/authkeys"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
)

// sshd -T with the files Debian 13 and Ubuntu 26.04 take keys from
// (s5/00-inspect logs, 2026-11-03: authorizedkeysfile .ssh/authorized_keys
// .ssh/authorized_keys2, strictmodes yes): with PAM, and with UsePAM no.
const (
	keyFiles      = "authorizedkeysfile .ssh/authorized_keys .ssh/authorized_keys2\nstrictmodes yes\n"
	sshdKeys      = sshdPAM + keyFiles
	sshdKeysNoPAM = sshdBase + "usepam no\n" + sshdRest + keyFiles
)

// testKey is a synthetic ED25519 public key, its 32 bytes seed: the
// OpenSSH wire form (RFC 8709 §4) of a key that is no one's.
func testKey(seed byte, comment string) string {
	blob := binary.BigEndian.AppendUint32(nil, 11)
	blob = append(blob, "ssh-ed25519"...)
	blob = binary.BigEndian.AppendUint32(blob, 32)
	for range 32 {
		blob = append(blob, seed)
	}
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob) + " " + comment
}

// fpOf is a key's fingerprint.
func fpOf(t *testing.T, line string) string {
	t.Helper()
	k, err := authkeys.ParsePublic(line)
	if err != nil {
		t.Fatal(err)
	}
	return k.Fingerprint
}

var (
	key1 = testKey(1, "george@laptop-1")
	key2 = testKey(2, "george@laptop-2")
	key3 = testKey(3, "alice@laptop")
	key4 = testKey(4, "carol@laptop")
)

// home is a user's home and keys: the files under it, relative, each a
// list of lines; mode is the home's.
type home struct {
	uid   int
	mode  fs.FileMode
	files map[string][]string
}

// keyed is debian13Login with sshd -T answering sshd, the homes of george
// (uid 1000) and alice (1001) 0700 and their own, with ~/.ssh 0700 where
// they have a file, and accounts' commands simulated (simAccounts).
func keyed(sshd string, homes map[string]home) site {
	s := debian13Login()
	s.sshd = sshd
	s.seed = func(h *hostfake.System) {
		h.AddDir("/home", 0o755, 0, 0)
		for name, hm := range homes {
			if hm.mode == 0 {
				hm.mode = 0o700
			}
			h.AddDir("/home/"+name, hm.mode, hm.uid, hm.uid)
			for rel, lines := range hm.files {
				h.AddDir("/home/"+name+"/.ssh", 0o700, hm.uid, hm.uid)
				h.AddFile("/home/"+name+"/"+rel, []byte(strings.Join(lines, "\n")+"\n"), 0o600, hm.uid, hm.uid)
			}
		}
	}
	return s
}

// both are george's two keys and alice's none, as on the Debian VM.
func both() map[string]home {
	return map[string]home{"george": {uid: 1000, files: map[string][]string{".ssh/authorized_keys": {key1, key2}}}, "alice": {uid: 1001}}
}

// simAccounts answers useradd, userdel --remove and install -d as shadow's
// and coreutils' tools do them (useradd(8), USERGROUPS_ENAB yes as on the
// three VMs: the user's own group; "!" without --password; the home with
// --create-home), the new user uid 1002.
func simAccounts(h *hostfake.System) {
	inner := h.Handler
	add := func(name, line string) {
		e, _ := h.Entry(name)
		h.AddFile(name, append(e.Data, line...), e.Mode.Perm(), e.UID, e.GID)
	}
	drop := func(name, user string) {
		e, _ := h.Entry(name)
		var keep []string
		for _, l := range strings.SplitAfter(string(e.Data), "\n") {
			if !strings.HasPrefix(l, user+":") {
				keep = append(keep, l)
			}
		}
		h.AddFile(name, []byte(strings.Join(keep, "")), e.Mode.Perm(), e.UID, e.GID)
	}
	h.Handler = func(argv []string, stdin []byte) hostfake.Response {
		name := argv[len(argv)-1]
		switch argv[0] {
		case "useradd":
			pw, dir := "!", ""
			for i := 1; i < len(argv)-1; i++ {
				switch argv[i] {
				case "--password":
					pw = argv[i+1]
				case "--home-dir":
					dir = argv[i+1]
				}
			}
			add("/etc/passwd", name+":x:1002:1002::"+dir+":/bin/bash\n")
			add("/etc/group", name+":x:1002:\n")
			add(accounts.ShadowFile, name+":"+pw+":20000:0:99999:7:::\n")
			h.AddDir(dir, 0o700, 1002, 1002)
			return hostfake.Response{}
		case "userdel":
			for _, f := range []string{"/etc/passwd", "/etc/group", accounts.ShadowFile} {
				drop(f, name)
			}
			_ = h.RemoveAll(context.Background(), "/home/"+name)
			return hostfake.Response{}
		case "install": // install -d -m 700 -o USER -g GROUP DIR, by name or number
			ids := map[string]int{"carol": 1002}
			uid, ok := ids[argv[5]]
			if !ok {
				uid, _ = strconv.Atoi(argv[5])
			}
			h.AddDir(name, 0o700, uid, uid)
			return hostfake.Response{}
		}
		return inner(argv, stdin)
	}
}

// refused checks p's one refusal: its code, the registry's, and its words.
func refused(t *testing.T, p *engine.Plan, code, words string) {
	t.Helper()
	rf := refusalOf(t, p)
	if rf.Code != code || output.CodeOf(rf) != code || output.CodeOf(p.Err()) != code {
		t.Errorf("code %s, registry's %s, want %s", rf.Code, output.CodeOf(rf), code)
	}
	if got := screenkit.RefusalWords(rf); got != words && !(strings.HasSuffix(words, "…") && strings.HasPrefix(got, strings.TrimSuffix(words, "…"))) {
		t.Errorf("words\n %q\nwant\n %q", got, words)
	}
}

// TestKeyRefusals: each key refusal, in the decided words (§6) where they
// are decided, with its code, the registry's, and nothing written.
func TestKeyRefusals(t *testing.T) {
	strayHomes := map[string]home{"george": {uid: 1000, files: map[string][]string{".ssh/authorized_keys": {key1}, ".ssh/authorized_keys2": {key1}}}}
	elsewhere := map[string]home{"george": {uid: 1000, files: map[string][]string{".ssh/authorized_keys": {key1, key2}, ".ssh/authorized_keys2": {key1}}}}
	oneKey := map[string]home{"george": {uid: 1000, files: map[string][]string{".ssh/authorized_keys": {key1}}}}
	add := users.Admin{Action: users.ActionAddKey, User: "george", Key: key4, Getenv: env(sudoGeorge)}
	otherFile := map[string]home{"george": {uid: 1000, files: map[string][]string{".ssh/authorized_keys": {key2}, ".ssh/authorized_keys2": {key1}}}}
	for _, tc := range []struct {
		name        string
		site        site
		a           users.Admin
		code, words string
	}{
		{"key files not read", keyed("", both()), add, "USERS_KEY_FILES_NOT_READ", "hardn can't read which files sshd takes keys from: …"},
		{"not the file hardn writes", keyed(sshdPAM+"authorizedkeysfile /etc/ssh/keys/%u\n", both()), add,
			"AUTHKEYS_NOT_PLANNED", "sshd doesn't read ~/.ssh/authorized_keys here (AuthorizedKeysFile /etc/ssh/keys/%u), so hardn can't change keys."},
		{"StrictModes", keyed(sshdKeys, map[string]home{"alice": {uid: 1001, mode: 0o775}}), users.Admin{Action: users.ActionAddKey, User: "alice", Key: key3},
			"AUTHKEYS_STRICT_MODES", "sshd would ignore a key for alice: /home/alice is writable by its group (0775). With StrictModes it takes keys only where the home and ~/.ssh are the user's own and no one else can write to them."},
		{"not a key", keyed(sshdKeys, both()), users.Admin{Action: users.ActionAddKey, User: "george", Key: "ssh-ed25519 AAAA george"},
			"AUTHKEYS_NOT_KEY", "Paste one OpenSSH public key, such as ssh-ed25519 AAAA… name."},
		{"not listed", keyed(sshdKeys, both()), users.Admin{Action: users.ActionAddKey, User: "carol", Key: key4},
			"USERS_NOT_LISTED", "carol isn't a user hardn manages here. Choose one of the users listed."},
		{"the session user's last key", keyed(sshdKeys, oneKey), users.Admin{Action: users.ActionRemoveKey, User: "george", Fingerprint: fpOf(t, key1), Getenv: env(sudoGeorge)},
			"USERS_LAST_KEY", "This is george's last key, and you're logged in as george. Add another key first."},
		{"the last key, counted across files", keyed(sshdKeys, strayHomes), users.Admin{Action: users.ActionRemoveKey, User: "george", Fingerprint: fpOf(t, key1), Getenv: env(sudoGeorge)},
			"USERS_LAST_KEY", "This is george's last key, and you're logged in as george. Add another key first."},
		{"the last key, a line sshd refuses not counted", keyed(sshdKeys, map[string]home{"george": {uid: 1000, files: map[string][]string{".ssh/authorized_keys": {key1, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 george@old"}}}}),
			users.Admin{Action: users.ActionRemoveKey, User: "george", Fingerprint: fpOf(t, key1), Getenv: env(sudoGeorge)},
			"USERS_LAST_KEY", "This is george's last key, and you're logged in as george. Add another key first."},
		{"also in authorized_keys2", keyed(sshdKeys, elsewhere), users.Admin{Action: users.ActionRemoveKey, User: "george", Fingerprint: fpOf(t, key1), Getenv: env(sudoGeorge)},
			"USERS_KEY_ELSEWHERE", "george's key is also in /home/george/.ssh/authorized_keys2, which sshd reads too. Remove it there, then try again."},
		{"a key the user has", keyed(sshdKeys, elsewhere), users.Admin{Action: users.ActionAddKey, User: "george", Key: key2, Getenv: env(sudoGeorge)},
			"USERS_KEY_THERE", "george already has this key."},
		{"a key the user has in authorized_keys2", keyed(sshdKeys, otherFile), users.Admin{Action: users.ActionAddKey, User: "george", Key: key1, Getenv: env(sudoGeorge)},
			"USERS_KEY_THERE", "george already has this key."},
		{"a key gone since the draw", keyed(sshdKeys, elsewhere), users.Admin{Action: users.ActionRemoveKey, User: "george", Fingerprint: fpOf(t, key4), Getenv: env(sudoGeorge)},
			"USERS_KEY_GONE", "That key isn't in /home/george/.ssh/authorized_keys any more. Nothing changed."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, p, _, _ := planOn(t, tc.site, tc.a)
			refused(t, p, tc.code, tc.words)
		})
	}
	// The other way: george's key counted once, alice's in her own home.
	if _, p, _, _ := planOn(t, keyed(sshdKeys, elsewhere), users.Admin{Action: users.ActionRemoveKey, User: "george", Fingerprint: fpOf(t, key2), Getenv: env(sudoGeorge)}); p.Err() != nil || p.Changes[len(p.Changes)-1].ID != users.ChangeKeys {
		t.Errorf("a key in authorized_keys alone: %v", p.Err())
	}
	if _, p, _, _ := planOn(t, keyed(sshdKeys, map[string]home{"alice": {uid: 1001}}), users.Admin{Action: users.ActionAddKey, User: "alice", Key: key3}); p.Err() != nil || len(p.Changes) != 1 {
		t.Errorf("alice's home 0700: %v", p.Err())
	}
}

// TestKeyRisk: removing a key is a lockout risk for the session user's,
// anyone's with the session not known, and anyone's last; removing another
// user's, one of two, and adding a key are plain. Applied, the line is
// gone and every other line kept.
func TestKeyRisk(t *testing.T) {
	ctx := context.Background()
	two := map[string]home{"george": {uid: 1000, files: map[string][]string{".ssh/authorized_keys": {key1, key2}}},
		"alice": {uid: 1001, files: map[string][]string{".ssh/authorized_keys": {"# alice's", key3, key4}}}}
	oneAlice := map[string]home{"george": {uid: 1000, files: map[string][]string{".ssh/authorized_keys": {key1, key2}}},
		"alice": {uid: 1001, files: map[string][]string{".ssh/authorized_keys": {key3}}}}
	for _, tc := range []struct {
		name  string
		homes map[string]home
		a     users.Admin
		want  engine.RiskClass
	}{
		{"the session user's", two, users.Admin{Action: users.ActionRemoveKey, User: "george", Fingerprint: fpOf(t, key1), Getenv: env(sudoGeorge)}, engine.RiskLockout},
		{"another user's, one of two", two, users.Admin{Action: users.ActionRemoveKey, User: "alice", Fingerprint: fpOf(t, key3), Getenv: env(sudoGeorge)}, engine.RiskPlain},
		{"session not known", two, users.Admin{Action: users.ActionRemoveKey, User: "alice", Fingerprint: fpOf(t, key3), Getenv: env(rootLogin)}, engine.RiskLockout},
		{"another user's last", oneAlice, users.Admin{Action: users.ActionRemoveKey, User: "alice", Fingerprint: fpOf(t, key3), Getenv: env(sudoGeorge)}, engine.RiskLockout},
		{"added", two, users.Admin{Action: users.ActionAddKey, User: "george", Key: testKey(9, "george@phone"), Getenv: env(sudoGeorge)}, engine.RiskPlain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, p, _, _ := planOn(t, keyed(sshdKeys, tc.homes), tc.a)
			if p.Err() != nil || p.Risk != tc.want || p.Changes[len(p.Changes)-1].ID != users.ChangeKeys { // a lockout risk's first is the boot unit
				t.Errorf("%v, risk %v, want %v", p.Err(), p.Risk, tc.want)
			}
		})
	}
	e, p, _, h := planOn(t, keyed(sshdKeys, two), users.Admin{Action: users.ActionRemoveKey, User: "alice", Fingerprint: fpOf(t, key3), Getenv: env(sudoGeorge)})
	if _, err := e.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	if en, _ := h.Entry("/home/alice/.ssh/authorized_keys"); string(en.Data) != "# alice's\n"+key4+"\n" || en.Mode.Perm() != 0o600 || en.UID != 1001 || en.GID != 1001 {
		t.Errorf("after: %q %v %d:%d", en.Data, en.Mode, en.UID, en.GID)
	}
}

// TestRestrictedKey: a key whose options may stop a login (authkeys
// Restricted) does not count toward the last key. george, the session
// user, with key1 plain and key2 command="/bin/false": removing key1
// refuses as his last key; removing key2 plans, a lockout risk as his.
// alice's (another user's) key3 plain and key4 restricted: removing key3 is
// a lockout risk, key4 plain.
func TestRestrictedKey(t *testing.T) {
	homes := map[string]home{"george": {uid: 1000, files: map[string][]string{".ssh/authorized_keys": {key1, `command="/bin/false" ` + key2}}},
		"alice": {uid: 1001, files: map[string][]string{".ssh/authorized_keys": {key3, `from="192.0.2.0/24" ` + key4}}}}
	_, p, _, _ := planOn(t, keyed(sshdKeys, homes), users.Admin{Action: users.ActionRemoveKey, User: "george", Fingerprint: fpOf(t, key1), Getenv: env(sudoGeorge)})
	refused(t, p, "USERS_LAST_KEY", "This is george's last key, and you're logged in as george. Add another key first.")
	for _, tc := range []struct {
		user, key string
		want      engine.RiskClass
	}{{"george", key2, engine.RiskLockout}, {"alice", key3, engine.RiskLockout}, {"alice", key4, engine.RiskPlain}} {
		_, p, _, _ := planOn(t, keyed(sshdKeys, homes), users.Admin{Action: users.ActionRemoveKey, User: tc.user, Fingerprint: fpOf(t, tc.key), Getenv: env(sudoGeorge)})
		if p.Err() != nil || p.Risk != tc.want || p.Changes[len(p.Changes)-1].ID != users.ChangeKeys {
			t.Errorf("%s's %s: %v, risk %v, want %v", tc.user, tc.key, p.Err(), p.Risk, tc.want)
		}
	}
	r, err := users.Read(context.Background(), func() host.System { s, _ := live(t, keyed(sshdKeys, homes)); return s }(), debian13Login().plat.facts, env(sudoGeorge))
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range r.Users {
		if u.Name == "george" && (len(u.Keys) != 2 || u.Keys[0].Restricted() || !u.Keys[1].Restricted()) {
			t.Errorf("george's keys %+v", u.Keys)
		}
	}
}

// TestAddKey: alice with no ~/.ssh: install makes it, 0700 and hers, then
// the file 0600, hers; with the write failing, the rollback removes ~/.ssh.
// The key there already is an empty plan.
func TestAddKey(t *testing.T) {
	ctx := context.Background()
	alice := users.Admin{Action: users.ActionAddKey, User: "alice", Key: key3, Getenv: env(sudoGeorge)}
	e, p, _, h := planOn(t, keyed(sshdKeys, both()), alice)
	simAccounts(h)
	if p.Err() != nil || len(p.Changes) != 1 || !slices.Equal(p.Changes[0].Commands, []string{"install -d -m 700 -o 1001 -g 1001 /home/alice/.ssh"}) || p.Changes[0].Summary != "Add a key for alice" {
		t.Fatalf("%v, %+v", p.Err(), p.Changes)
	}
	if _, err := e.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	f, _ := h.Entry("/home/alice/.ssh/authorized_keys")
	d, _ := h.Entry("/home/alice/.ssh")
	if string(f.Data) != key3+"\n" || f.Mode.Perm() != 0o600 || f.UID != 1001 || f.GID != 1001 || d.Mode.Perm() != 0o700 || d.UID != 1001 {
		t.Errorf("written %q %v %d:%d, dir %v %d", f.Data, f.Mode, f.UID, f.GID, d.Mode, d.UID)
	}
	s := keyed(sshdKeys, both())
	s.fail = map[string]error{"write /home/alice/.ssh/authorized_keys": fs.ErrPermission}
	e, p, _, h = planOn(t, s, alice)
	simAccounts(h)
	if _, err := e.Apply(ctx, p); !errors.Is(err, engine.ErrRolledBack) {
		t.Fatalf("apply: %v", err)
	}
	if _, ok := h.Entry("/home/alice/.ssh"); ok {
		t.Error("rolled back: /home/alice/.ssh still there")
	}
	_, p, _, _ = planOn(t, keyed(sshdKeys, both()), users.Admin{Action: users.ActionAddKey, User: "george", Key: key2})
	refused(t, p, "USERS_KEY_THERE", "george already has this key.")
}

// TestLockedFinding: george's "!" where sshd doesn't use PAM: adding a key
// is planned, with the finding that sshd refuses its key login; with PAM,
// none.
func TestLockedFinding(t *testing.T) {
	for sshd, want := range map[string]bool{sshdKeysNoPAM: true, sshdKeys: false} {
		_, p, _, _ := planOn(t, keyed(sshd, both()), users.Admin{Action: users.ActionAddKey, User: "george", Key: testKey(9, "george@phone")})
		if p.Err() != nil || len(p.Changes) != 1 {
			t.Fatal(p.Err())
		}
		got := slices.ContainsFunc(p.Changes[0].Findings, func(f engine.Finding) bool {
			return f.Code == users.CodeLocked && f.Detail == "sshd refuses george's keys: the account is locked (!) and sshd doesn't use PAM"
		})
		if got != want {
			t.Errorf("UsePAM no %v: findings %+v", want, p.Changes[0].Findings)
		}
	}
}

// TestCreateRefusals: Create's refusals, and CheckNew's the same.
func TestCreateRefusals(t *testing.T) {
	there := keyed(sshdKeys, map[string]home{"carol": {uid: 0}})
	alpineNoTool := alpine324()
	alpineNoTool.facts.Elevation.Doas = no
	for _, tc := range []struct {
		name        string
		site        site
		n           users.New
		code, words string
	}{
		{"upper case", keyed(sshdKeys, nil), users.New{Name: "Carol"}, "USERS_NAME_FORM", "Use lower-case letters, digits, - and _, starting with a letter or _, up to 32."},
		{"too long", keyed(sshdKeys, nil), users.New{Name: strings.Repeat("c", 33)}, "USERS_NAME_FORM", "Use lower-case letters, digits, - and _, starting with a letter or _, up to 32."},
		{"a user's", keyed(sshdKeys, nil), users.New{Name: "alice"}, "USERS_NAME_TAKEN", "alice already exists."},
		{"a group's", keyed(sshdKeys, nil), users.New{Name: "sudo"}, "USERS_NAME_TAKEN", "sudo already exists."},
		{"its home there", there, users.New{Name: "carol"}, "USERS_HOME_THERE", "/home/carol is already there, and undoing Create would delete it. Choose another name."},
		{"admin, no tool", site{plat: pve9(), users: []string{georgeLine}}, users.New{Name: "carol", Admin: true}, "USERS_NO_TOOL", "sudo isn't installed, so hardn can't grant admin rights. Install sudo, then try again."},
		// Alpine (the LXC of 2026-11-07): doas named, Alpine's default (sudoers.md);
		// useradd, shadow's, looked for before the tool; both missing, with
		// admin rights asked, one refusal naming both packages.
		{"alpine, admin, no tool", site{plat: alpineNoTool}, users.New{Name: "carol", Admin: true}, "USERS_NO_TOOL", "doas isn't installed, so hardn can't grant admin rights. Install doas, then try again."},
		{"alpine, no useradd", site{plat: alpine324(), noUseradd: true}, users.New{Name: "carol"}, "USERS_NO_USERADD", "useradd isn't installed, so hardn can't create users. Run apk add shadow, then try again."},
		{"alpine, no useradd, admin", site{plat: alpine324(), noUseradd: true}, users.New{Name: "carol", Admin: true}, "USERS_NO_USERADD", "useradd isn't installed, so hardn can't create users. Run apk add shadow, then try again."},
		{"alpine, no useradd, no tool", site{plat: alpineNoTool, noUseradd: true}, users.New{Name: "carol", Admin: true}, "USERS_NO_USERADD", "useradd and doas aren't installed, so hardn can't create an admin. Run apk add shadow doas, then try again."},
		{"alpine, no useradd, no tool, no admin", site{plat: alpineNoTool, noUseradd: true}, users.New{Name: "carol"}, "USERS_NO_USERADD", "useradd isn't installed, so hardn can't create users. Run apk add shadow, then try again."},
		{"not a key", keyed(sshdKeys, nil), users.New{Name: "carol", Keys: []string{"ssh-rsa AAAA"}}, "AUTHKEYS_NOT_KEY", "Key 1 isn't an OpenSSH public key, such as ssh-ed25519 AAAA… name."},
		{"a bad second key", keyed(sshdKeys, nil), users.New{Name: "carol", Keys: []string{key4, "ssh-rsa AAAA"}}, "AUTHKEYS_NOT_KEY", "Key 2 isn't an OpenSSH public key, such as ssh-ed25519 AAAA… name."},
		{"an empty key", keyed(sshdKeys, nil), users.New{Name: "carol", Keys: []string{"", key4}}, "AUTHKEYS_NOT_KEY", "Key 1 isn't an OpenSSH public key, such as ssh-ed25519 AAAA… name."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, p, sys, _ := planOn(t, tc.site, users.Admin{Action: users.ActionCreate, New: tc.n})
			refused(t, p, tc.code, tc.words)
			r, err := users.Read(context.Background(), sys, tc.site.plat.facts, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.CheckNew(context.Background(), sys, tc.n); output.CodeOf(err) != tc.code {
				t.Errorf("CheckNew: %v", err)
			}
		})
	}
	// Without admin rights Create needs no tool, as on Proxmox VE.
	if _, p, _, _ := planOn(t, site{plat: pve9(), users: []string{georgeLine}}, users.Admin{Action: users.ActionCreate, New: users.New{Name: "carol"}}); p.Err() != nil || len(p.Changes) != 1 {
		t.Errorf("Proxmox VE, no admin rights: %v", p.Err())
	}
	// useradd is looked for on Alpine alone; a lookup that fails is its
	// error, not a refusal.
	if _, p, _, _ := planOn(t, site{plat: debian13(), noUseradd: true}, users.Admin{Action: users.ActionCreate, New: users.New{Name: "carol"}}); p.Err() != nil || len(p.Changes) != 1 {
		t.Errorf("Debian, no useradd: %v", p.Err())
	}
	_, p, _, _ := planOn(t, site{plat: alpine324(), fail: map[string]error{"stat /usr/sbin/useradd": fs.ErrPermission}}, users.Admin{Action: users.ActionCreate, New: users.New{Name: "carol"}})
	var rf *steps.Refusal
	if err := p.Err(); !errors.Is(err, fs.ErrPermission) || errors.As(err, &rf) {
		t.Errorf("Alpine, useradd's lookup failing: %v", err)
	}
}

// TestCreate: the account, then the grant, then the key, in one plain plan;
// --password '*' only where sshd's PAM is not on; applied, the user, its
// grant and its key 0600, its own; with the key's write failing, the
// rollback removes the grant and the user, its home with it. No key: the
// notice. The grant asks carol's password, with the finding that none is
// set yet; with NoPassword, the opt-in, it asks none, with no finding.
func TestCreate(t *testing.T) {
	ctx := context.Background()
	n := users.New{Name: "carol", Admin: true, Keys: []string{key4}}
	for _, tc := range []struct {
		name, sshd string
		n          users.New
		star       bool
	}{
		{"UsePAM yes", sshdKeys, n, false},
		{"UsePAM no", sshdKeysNoPAM, n, true},
		// sshd's PAM not established: sshd -T not read (no key, as no
		// key is planned then), or no usepam line (a build without PAM).
		{"sshd -T not read", "", users.New{Name: "carol", Admin: true}, true},
		{"no usepam line", sshdBase + sshdRest + keyFiles, n, true},
	} {
		_, p, _, _ := planOn(t, keyed(tc.sshd, both()), users.Admin{Action: users.ActionCreate, New: tc.n})
		var ids []string
		for _, c := range p.Changes {
			ids = append(ids, c.ID)
		}
		want := []string{users.ChangeCreate, users.ChangeGrant, users.ChangeKeys}
		if len(tc.n.Keys) == 0 {
			want = want[:2]
		}
		if p.Err() != nil || p.Risk != engine.RiskPlain || len(ids) < len(want) || !slices.Equal(ids[len(ids)-len(want):], want) {
			t.Fatalf("%s: %v, risk %v, %q", tc.name, p.Err(), p.Risk, ids)
		}
		cmd := p.Changes[len(ids)-len(want)].Commands[0]
		if pre := "useradd --home-dir /home/carol --create-home --shell /bin/bash"; !strings.HasPrefix(cmd, pre) || strings.Contains(cmd, `--password "*"`) != tc.star {
			t.Errorf("%s: %s", tc.name, cmd)
		}
	}
	e, p, _, h := planOn(t, keyed(sshdKeys, both()), users.Admin{Action: users.ActionCreate, New: n})
	simAccounts(h)
	if _, err := e.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	k, _ := h.Entry("/home/carol/.ssh/authorized_keys")
	g, _ := h.Entry(grantDir + "hardn-grant-carol")
	if string(k.Data) != key4+"\n" || k.Mode.Perm() != 0o600 || k.UID != 1002 || k.GID != 1002 || string(g.Data) != "# Managed by hardn\ncarol ALL=(ALL) ALL\n" {
		t.Errorf("key %q %v %d:%d, grant %q", k.Data, k.Mode, k.UID, k.GID, g.Data)
	}
	for _, c := range p.Changes {
		want := 0
		if c.ID == users.ChangeGrant {
			want = 1
		}
		if len(c.Findings) != want || (want == 1 && (c.Findings[0].Code != users.CodePasswordNotSet || c.Findings[0].Detail != "carol can't use sudo until a password is set: run passwd carol")) {
			t.Errorf("%s: findings %+v", c.ID, c.Findings)
		}
	}
	opt := n
	opt.NoPassword = true
	e, p, _, h = planOn(t, keyed(sshdKeys, both()), users.Admin{Action: users.ActionCreate, New: opt})
	simAccounts(h)
	if _, err := e.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	if g, _ := h.Entry(grantDir + "hardn-grant-carol"); p.Risk != engine.RiskPlain || string(g.Data) != "# Managed by hardn\ncarol ALL=(ALL) NOPASSWD: ALL\n" {
		t.Errorf("NoPassword: risk %v, grant %q", p.Risk, g.Data)
	}
	for _, c := range p.Changes {
		if len(c.Findings) > 0 {
			t.Errorf("NoPassword: %s: findings %+v", c.ID, c.Findings)
		}
	}

	s := keyed(sshdKeys, both())
	s.fail = map[string]error{"write /home/carol/.ssh/authorized_keys": fs.ErrPermission}
	e, p, _, h = planOn(t, s, users.Admin{Action: users.ActionCreate, New: n})
	simAccounts(h)
	if _, err := e.Apply(ctx, p); !errors.Is(err, fs.ErrPermission) || !errors.Is(err, engine.ErrRolledBack) {
		t.Fatalf("apply: %v", err)
	}
	passwd, _ := h.Entry("/etc/passwd")
	_, grant := h.Entry(grantDir + "hardn-grant-carol")
	_, dir := h.Entry("/home/carol")
	if strings.Contains(string(passwd.Data), "carol:") || grant || dir {
		t.Errorf("rolled back: passwd %q, grant %v, home %v", passwd.Data, grant, dir)
	}

	_, p, _, _ = planOn(t, keyed(sshdKeys, both()), users.Admin{Action: users.ActionCreate, New: users.New{Name: "carol"}})
	if p.Err() != nil || len(p.Changes) != 1 || len(p.Changes[0].Findings) != 1 || p.Changes[0].Findings[0].Code != users.CodeNoKey ||
		p.Changes[0].Findings[0].Detail != "Without a key, carol can't log in: there's no password" {
		t.Errorf("no key: %v, %+v", p.Err(), p.Changes)
	}
}

// TestCreateKeys: Create with several keys, hardn.yml's sshKeys as -c
// passes them: one change adding every key, in order, each 0600 and the
// user's; the same key twice added once; one key keeps its words; a key
// another file sshd reads holds already (AuthorizedKeysFile also
// /etc/ssh/keys/%u, carol's holding key4) left out, the change adding the
// rest. A bad second key refuses the plan, nothing run (TestCreateRefusals
// has its words).
func TestCreateKeys(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		keys    []string
		held    string // /etc/ssh/keys/carol's key, which sshd also reads; "" for none
		file    string
		summary string
	}{
		{"two keys", []string{key4, key3}, "", key4 + "\n" + key3 + "\n", "Add carol's keys"},
		{"one key twice", []string{key4, testKey(4, "carol@phone")}, "", key4 + "\n", "Add carol's key"},
		{"one held in another file sshd reads", []string{key4, key3}, key4, key3 + "\n", "Add carol's key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := keyed(sshdKeys, both())
			if tc.held != "" {
				s = keyed(sshdPAM+"authorizedkeysfile .ssh/authorized_keys .ssh/authorized_keys2 /etc/ssh/keys/%u\nstrictmodes yes\n", both())
				seed := s.seed
				s.seed = func(h *hostfake.System) {
					seed(h)
					h.AddDir("/etc/ssh/keys", 0o755, 0, 0)
					h.AddFile("/etc/ssh/keys/carol", []byte(tc.held+"\n"), 0o644, 0, 0)
				}
			}
			e, p, _, h := planOn(t, s, users.Admin{Action: users.ActionCreate, New: users.New{Name: "carol", Admin: true, Keys: tc.keys}})
			var keys []engine.Change
			for _, c := range p.Changes {
				if c.ID == users.ChangeKeys {
					keys = append(keys, c)
				}
				if len(c.Findings) > 0 && (c.ID != users.ChangeGrant || c.Findings[0].Code != users.CodePasswordNotSet) {
					t.Errorf("%s: findings %+v", c.ID, c.Findings)
				}
			}
			if p.Err() != nil || len(keys) != 1 || keys[0].Summary != tc.summary {
				t.Fatalf("%v: key changes %+v, want one, %q", p.Err(), keys, tc.summary)
			}
			simAccounts(h)
			if _, err := e.Apply(ctx, p); err != nil {
				t.Fatal(err)
			}
			k, _ := h.Entry("/home/carol/.ssh/authorized_keys")
			if string(k.Data) != tc.file || k.Mode.Perm() != 0o600 || k.UID != 1002 || k.GID != 1002 {
				t.Errorf("keys %q %v %d:%d, want %q 0600 1002:1002", k.Data, k.Mode, k.UID, k.GID, tc.file)
			}
		})
	}
	_, p, _, h := planOn(t, keyed(sshdKeys, both()), users.Admin{Action: users.ActionCreate, New: users.New{Name: "carol", Admin: true, Keys: []string{key4, "ssh-rsa AAAA"}}})
	if p.Err() == nil || ran(h, "useradd") || ran(h, "install") {
		t.Errorf("a bad second key: %v, useradd ran %v", p.Err(), ran(h, "useradd"))
	}
}

// ran reports whether h ran a command named name.
func ran(h *hostfake.System, name string) bool {
	return slices.ContainsFunc(h.Calls(), func(c hostfake.Call) bool { return c.Op == "run" && len(c.Argv) > 0 && c.Argv[0] == name })
}

// armed is a System that calls fire once, on the engine's first journal
// write: after Apply's own plan, before the first change runs.
type armed struct {
	host.System
	fire func()
}

func (a *armed) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	if a.fire != nil && strings.HasPrefix(name, "/var/lib/hardn/") {
		a.fire()
		a.fire = nil
	}
	return a.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

// TestCreateUndo: Create's undo, userdel --remove, runs only for what
// useradd made. The name taken, or /home/carol made, after the engine's
// plan at apply and before the change runs: Create refuses, running
// neither useradd nor userdel; useradd failing: no userdel; a later step
// failing: userdel --remove.
func TestCreateUndo(t *testing.T) {
	ctx := context.Background()
	n := users.New{Name: "carol", Admin: true, Keys: []string{key4}}
	for _, tc := range []struct {
		name    string
		between func(h *hostfake.System)
		fail    map[string]error
		code    string
		useradd bool
		userdel bool
	}{
		{"the name taken", func(h *hostfake.System) {
			e, _ := h.Entry("/etc/group")
			h.AddFile("/etc/group", append(e.Data, "carol:x:1003:\n"...), e.Mode.Perm(), e.UID, e.GID)
		}, nil, "USERS_NAME_TAKEN", false, false},
		{"the home made", func(h *hostfake.System) { h.AddDir("/home/carol", 0o755, 0, 0) }, nil, "USERS_HOME_THERE", false, false},
		{"useradd failing", func(h *hostfake.System) {
			inner := h.Handler
			h.Handler = func(argv []string, stdin []byte) hostfake.Response {
				if argv[0] == "useradd" {
					return hostfake.Response{Stderr: "useradd: cannot lock /etc/passwd\n", ExitCode: 1}
				}
				return inner(argv, stdin)
			}
		}, nil, "", true, false},
		{"the key's write failing", func(*hostfake.System) {}, map[string]error{"write /home/carol/.ssh/authorized_keys": fs.ErrPermission}, "", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := keyed(sshdKeys, both())
			s.fail = tc.fail
			sys, h := live(t, s)
			simAccounts(h)
			e := engineOn(&armed{System: sys, fire: func() { tc.between(h) }}, s.plat.facts)
			p, err := e.Plan(ctx, []engine.Control{users.Admin{Action: users.ActionCreate, New: n}}, nil)
			if err != nil || p.Err() != nil {
				t.Fatal(err, p.Err())
			}
			_, err = e.Apply(ctx, p)
			if !errors.Is(err, engine.ErrRolledBack) || (tc.code != "" && output.CodeOf(err) != tc.code) {
				t.Fatalf("apply: %v, want %s", err, tc.code)
			}
			if ran(h, "useradd") != tc.useradd || ran(h, "userdel") != tc.userdel {
				t.Errorf("useradd ran %v, userdel ran %v; want %v, %v", ran(h, "useradd"), ran(h, "userdel"), tc.useradd, tc.userdel)
			}
		})
	}
}

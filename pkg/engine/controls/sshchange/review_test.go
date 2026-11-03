package sshchange_test

import (
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
)

// keyedAlice is alice with a key too, so Key-only plans.
func keyedAlice(t *testing.T, conf string) *vm {
	a := alice
	a.keys = []string{key(3, "alice@laptop-3")}
	return newVM(t, debian13(), true, george, a).file(sshd.DropInDir+"/50-cloud-init.conf", conf)
}

// TestKeyOnlyDirectives: Key-only sets KbdInteractiveAuthentication no
// beside PasswordAuthentication no where it is yes and sshd uses PAM, and
// PasswordAuthentication alone where sshd has no PAM.
func TestKeyOnlyDirectives(t *testing.T) {
	for _, tt := range []struct {
		name string
		pam  string
		kbd  bool
	}{
		{"UsePAM yes", "UsePAM yes", true},
		{"UsePAM no", "UsePAM no", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := keyedAlice(t, "PasswordAuthentication yes\nKbdInteractiveAuthentication yes\n")
			v.file(sshd.MainConfig, strings.Replace(debianMain, "UsePAM yes", tt.pam, 1))
			_, p := planOn(t, v, keyOnly)
			if err := p.Err(); err != nil || p.Empty() {
				t.Fatalf("plan: %v, %d changes", err, len(p.Changes))
			}
			var diffs strings.Builder
			for _, c := range p.Changes {
				diffs.WriteString(c.Diff)
			}
			d := diffs.String()
			if !strings.Contains(d, "+PasswordAuthentication no") || strings.Contains(d, "+KbdInteractiveAuthentication no") != tt.kbd {
				t.Errorf("diff:\n%s\nwant PasswordAuthentication no, KbdInteractiveAuthentication no %v", d, tt.kbd)
			}
		})
	}
}

// TestRefusalOrder: hosts that trip two refusals, the one the mock puts
// first winning (gallery-ssh.md §5 decision 2); and the review's other
// refusals: a uid-0 admin alone, an admin let in from some addresses
// alone from a console.
func TestRefusalOrder(t *testing.T) {
	toor := account{name: "toor", uid: 0, pw: "!", keys: []string{key(4, "toor@laptop-4")}}
	for _, tt := range []struct {
		name string
		host func(*testing.T) *vm
		c           engine.Control
		code, words string
	}{
		{"root's own login before a Match block letting root in", func(t *testing.T) *vm {
			v := debianVM(t).file(sshd.DropInDir+"/20-site.conf", "Match Address 192.0.2.0/24\n    PermitRootLogin yes\n")
			v.log = ""
			v.logIn("publickey", "root", "192.0.2.10")
			return v
		}, disallow, "SSH_ROOT_SESSION", ""},
		{"a password session before a Match block letting passwords in", func(t *testing.T) *vm {
			v := passwordLogin(t).file(sshd.DropInDir+"/20-site.conf", "Match Address 198.51.100.0/24\n    PasswordAuthentication yes\n")
			v.log = ""
			v.logIn("password", "alice", "192.0.2.10")
			return v
		}, keyOnly, "SSH_PASSWORD_SESSION", ""},
		{"an admin with a password and no key before no admin able", func(t *testing.T) *vm {
			return rsaSmall(t, newVM(t, debian13(), true, george, alice)).file(sshd.DropInDir+"/50-cloud-init.conf", "PasswordAuthentication yes\n")
		}, keyOnly, "SSH_ADMIN_PASSWORD_ONLY", ""},
		// toor, uid 0 and in sudo, is no admin besides root: users.Read
		// lists uid MinUID up, and LoginPaths skips uid 0 besides.
		{"the only admin besides root has uid 0", func(t *testing.T) *vm { return newVM(t, debian13(), false, toor).console() },
			disallow, "SSH_NO_ADMIN_LOGIN", "No admin could log in over SSH afterwards: there's no admin besides root. Fix that first."},
		{"from a console, AllowUsers george@192.0.2.0/24", func(t *testing.T) *vm {
			return debianVM(t).file(sshd.DropInDir+"/10-site.conf", "AllowUsers george@192.0.2.0/24\n").console()
		}, disallow, "SSH_NO_ADMIN_LOGIN", "No admin could log in over SSH afterwards: AllowUsers lets george in from 192.0.2.0/24 alone; alice isn't in AllowUsers. Fix that first."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, p := planOn(t, tt.host(t), tt.c)
			rf := refusalOf(t, p)
			words := screenkit.RefusalWords(rf)
			if rf.Code != tt.code || output.CodeOf(p.Items[0].Err) != tt.code || tt.words != "" && words != tt.words {
				t.Errorf("refused %s (registry's %s): %q\nwant %s %q", rf.Code, output.CodeOf(p.Items[0].Err), words, tt.code, tt.words)
			}
		})
	}
}

// TestRootValue: without-password, sshd's older name, read as
// prohibit-password; so Allow root key-only plans nothing where sshd -T
// prints it.
func TestRootValue(t *testing.T) {
	for in, want := range map[string]string{"without-password": "prohibit-password", "prohibit-password": "prohibit-password", "no": "no", "yes": "yes", "forced-commands-only": "forced-commands-only"} {
		var r sshchange.Reading
		r.Effective.PermitRootLogin.Value = in
		if got := r.RootValue(); got != want {
			t.Errorf("RootValue of %q = %q, want %q", in, got, want)
		}
	}
	v := debianVM(t).file(sshd.HardnDropIn, "PermitRootLogin without-password\n")
	if r := read(t, v, sudoGeorge); r.RootValue() != "prohibit-password" {
		t.Fatalf("sshd -T permitrootlogin without-password: RootValue %q", r.RootValue())
	}
	if _, p := planOn(t, v, allowRoot); p.Err() != nil || !p.Empty() {
		t.Errorf("Allow root key-only over without-password: %v, %d changes, want an empty plan", p.Err(), len(p.Changes))
	}
}

// TestNoPAM: KbdInteractiveAuthentication yes with UsePAM no lets no
// password in (PasswordsOn false, an admin's keyboard-interactive off);
// root's locked password counts only without PAM (RootLocked).
func TestNoPAM(t *testing.T) {
	kbd := func(t *testing.T, pam string) *vm {
		return debianVM(t).file(sshd.MainConfig, strings.Replace(strings.Replace(debianMain, "UsePAM yes", pam, 1), "KbdInteractiveAuthentication no", "KbdInteractiveAuthentication yes", 1))
	}
	for _, tt := range []struct {
		pam       string
		passwords bool
		paths     string
	}{
		{"UsePAM no", false, "george: george's account is locked (!) and sshd has no PAM|alice: alice has no key sshd reads"},
		{"UsePAM yes", true, "george key|alice: alice has no key sshd reads, password"},
	} {
		v := kbd(t, tt.pam)
		if got := read(t, v, sudoGeorge).PasswordsOn(); got != tt.passwords {
			t.Errorf("%s: PasswordsOn %v, want %v", tt.pam, got, tt.passwords)
		}
		if got := paths(t, v, false); got != tt.paths {
			t.Errorf("%s: %q\nwant %q", tt.pam, got, tt.paths)
		}
	}
	for pam, want := range map[string]bool{"UsePAM yes": false, "UsePAM no": true} {
		v := debianVM(t).shadowOf("root", "!").file(sshd.MainConfig, strings.Replace(debianMain, "UsePAM yes", pam, 1))
		if got := read(t, v, sudoGeorge).RootLocked; got != want {
			t.Errorf("root locked, %s: RootLocked %v, want %v", pam, got, want)
		}
	}
}

// TestRSASizes: an RSA key's size against RequiredRSASize: a blob cut
// short, and a key under 1024 bits where sshd -T prints no
// requiredrsasize, are no key at all (authkeys' checkBlob refuses a blob
// that ends early and a modulus under MinRSABits, as sshd does, before
// loginPath weighs the size: RSABits' 0 for the first is authkeys'
// TestRSABits'); a key of exactly RequiredRSASize bits is able.
func TestRSASizes(t *testing.T) {
	short := binary.BigEndian.AppendUint32(nil, 7)
	short = append(short, "ssh-rsa"...)
	short = binary.BigEndian.AppendUint32(short, 3)
	short = append(short, 1, 0, 1)
	short = binary.BigEndian.AppendUint32(short, 257)
	short = append(short, 0, 0xc5, 0xc5)
	for _, tt := range []struct {
		name, key, conf string
		noSize          bool
		want            string
	}{
		{"a blob cut short", "ssh-rsa " + base64.StdEncoding.EncodeToString(short) + " george@laptop-1", "", false,
			"george: george has no key sshd reads"},
		{"exactly RequiredRSASize", rsaKey(2048, 0x41, "george@laptop-1"), "RequiredRSASize 2048\n", false, "george key"},
		{"768 bits, requiredrsasize not printed", rsaKey(768, 0x41, "george@laptop-1"), "", true,
			"george: george has no key sshd reads"},
		{"1024 bits, requiredrsasize not printed", rsaKey(1024, 0x41, "george@laptop-1"), "", true, "george key"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := newVM(t, debian13(), true, george)
			v.noRSASize = tt.noSize
			v.f.AddFile("/home/george/.ssh/authorized_keys", []byte(tt.key+"\n"), 0o600, 1000, 1000)
			if tt.conf != "" {
				v.file(sshd.DropInDir+"/10-rsa.conf", tt.conf)
			}
			if got := paths(t, v.console(), true); got != tt.want {
				t.Errorf("%q\nwant %q", got, tt.want)
			}
		})
	}
}


package sshd_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// algos is PubkeyAcceptedAlgorithms as all three VMs print it (OpenSSH
// 10's default; 00-inspect-*.log, 2026-11-03).
const algos = "ssh-ed25519-cert-v01@openssh.com,ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com," +
	"ecdsa-sha2-nistp521-cert-v01@openssh.com,sk-ssh-ed25519-cert-v01@openssh.com,sk-ecdsa-sha2-nistp256-cert-v01@openssh.com," +
	"rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-ed25519,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384," +
	"ecdsa-sha2-nistp521,sk-ssh-ed25519@openssh.com,sk-ecdsa-sha2-nistp256@openssh.com,rsa-sha2-512,rsa-sha2-256"

// The VMs' sshd -T -C, in their forms and order (00-inspect-*.log), the
// lab's values replaced: Debian 13 (usepam, without-password, two key
// files), Ubuntu 26.04, Alpine 3.23 (no usepam line: built without PAM).
var (
	debianT = `port 22
addressfamily any
listenaddress [::]:22
listenaddress 0.0.0.0:22
usepam yes
logingracetime 120
maxauthtries 6
requiredrsasize 1024
permitrootlogin without-password
hostbasedauthentication no
pubkeyauthentication yes
kerberosauthentication no
gssapiauthentication no
passwordauthentication no
kbdinteractiveauthentication no
strictmodes yes
permitemptypasswords no
pubkeyacceptedalgorithms ` + algos + `
persourcepenaltyexemptlist 192.0.2.10
authorizedkeysfile .ssh/authorized_keys .ssh/authorized_keys2
authenticationmethods any
subsystem sftp /usr/lib/openssh/sftp-server
`
	ubuntuT = strings.Replace(debianT, "permitrootlogin without-password", "permitrootlogin prohibit-password", 1)
	alpineT = `port 22
addressfamily any
listenaddress [::]:22
listenaddress 0.0.0.0:22
logingracetime 120
maxauthtries 6
requiredrsasize 1024
permitrootlogin prohibit-password
hostbasedauthentication no
pubkeyauthentication yes
passwordauthentication no
kbdinteractiveauthentication yes
strictmodes yes
pubkeyacceptedalgorithms ` + algos + `
authorizedkeysfile .ssh/authorized_keys
authenticationmethods any
subsystem sftp internal-sftp
`
)

const georgeSpec = "user=george,host=192.0.2.10,addr=192.0.2.10"

func userHost(out string) *hostfake.System {
	h := hostfake.New(host.Options{})
	h.Respond(hostfake.Response{Stdout: out}, "sshd", "-T", "-f", sshd.MainConfig, "-C", georgeSpec)
	return h
}

func TestReadUserVMs(t *testing.T) {
	keys := []string{".ssh/authorized_keys", ".ssh/authorized_keys2"}
	for _, tc := range []struct {
		name, out string
		want      sshd.UserView
	}{
		{"debian13", debianT, sshd.UserView{PermitRootLogin: "prohibit-password", UsePAM: "yes", KbdInteractiveAuthentication: "no", AuthorizedKeysFile: keys}},
		{"ubuntu2604", ubuntuT, sshd.UserView{PermitRootLogin: "prohibit-password", UsePAM: "yes", KbdInteractiveAuthentication: "no", AuthorizedKeysFile: keys}},
		{"alpine323", alpineT, sshd.UserView{PermitRootLogin: "prohibit-password", UsePAM: "", KbdInteractiveAuthentication: "yes", AuthorizedKeysFile: keys[:1]}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := userHost(tc.out)
			v, err := sshd.ReadUser(context.Background(), h, "", "george", "192.0.2.10")
			if err != nil {
				t.Fatal(err)
			}
			w := tc.want
			w.User, w.Addr = "george", "192.0.2.10"
			w.PubkeyAuthentication, w.PasswordAuthentication, w.StrictModes = "yes", "no", "yes"
			w.AuthenticationMethods, w.RequiredRSASize = []string{"any"}, 1024
			w.PubkeyAcceptedAlgorithms = strings.Split(algos, ",")
			if !equalView(v, w) {
				t.Errorf("ReadUser =\n%+v\nwant\n%+v", v, w)
			}
			checkReadOnly(t, h)
			checkRuns(t, h, []string{"sshd", "-T", "-f", sshd.MainConfig, "-C", georgeSpec})
		})
	}
}

func equalView(a, b sshd.UserView) bool {
	return slices.Equal(a.AllowUsers, b.AllowUsers) && slices.Equal(a.DenyUsers, b.DenyUsers) &&
		slices.Equal(a.AllowGroups, b.AllowGroups) && slices.Equal(a.DenyGroups, b.DenyGroups) &&
		slices.Equal(a.AuthenticationMethods, b.AuthenticationMethods) && slices.Equal(a.AuthorizedKeysFile, b.AuthorizedKeysFile) &&
		slices.Equal(a.PubkeyAcceptedAlgorithms, b.PubkeyAcceptedAlgorithms) &&
		a.User == b.User && a.Addr == b.Addr && a.PermitRootLogin == b.PermitRootLogin && a.PubkeyAuthentication == b.PubkeyAuthentication &&
		a.PasswordAuthentication == b.PasswordAuthentication && a.KbdInteractiveAuthentication == b.KbdInteractiveAuthentication &&
		a.UsePAM == b.UsePAM && a.StrictModes == b.StrictModes && a.RequiredRSASize == b.RequiredRSASize
}

// TestReadUserOptions: the -o forms sshd -T -C printed on the VMs
// (01-inspect-b-*.log): AllowUsers, DenyUsers and AllowGroups one pattern
// per line, USER@HOST kept; RequiredRSASize and PubkeyAcceptedAlgorithms
// as set; AuthenticationMethods' lists on one line.
func TestReadUserOptions(t *testing.T) {
	out := strings.NewReplacer("requiredrsasize 1024", "requiredrsasize 2048",
		"pubkeyacceptedalgorithms "+algos, "pubkeyacceptedalgorithms rsa-sha2-512,rsa-sha2-256",
		"authenticationmethods any", "allowusers george@192.0.2.0/24\nallowusers nobody\ndenyusers alice@198.51.100.7\n"+
			"allowgroups wheel\nallowgroups sudo\ndenygroups nossh\nauthenticationmethods publickey,password publickey").Replace(alpineT)
	v, err := sshd.ReadUser(context.Background(), userHost(out), "", "george", "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name      string
		got, want []string
	}{
		{"AllowUsers", v.AllowUsers, []string{"george@192.0.2.0/24", "nobody"}},
		{"DenyUsers", v.DenyUsers, []string{"alice@198.51.100.7"}},
		{"AllowGroups", v.AllowGroups, []string{"wheel", "sudo"}},
		{"DenyGroups", v.DenyGroups, []string{"nossh"}},
		{"AuthenticationMethods", v.AuthenticationMethods, []string{"publickey,password", "publickey"}},
		{"PubkeyAcceptedAlgorithms", v.PubkeyAcceptedAlgorithms, []string{"rsa-sha2-512", "rsa-sha2-256"}},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if v.RequiredRSASize != 2048 {
		t.Errorf("RequiredRSASize = %d, want 2048", v.RequiredRSASize)
	}
}

func TestReadUserRefuses(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct{ user, addr string }{
		{"george,host=x", "192.0.2.10"}, {"geo rge", "192.0.2.10"}, {"user=george", "192.0.2.10"}, {"", "192.0.2.10"},
		{"george", "web-01"}, {"george", "192.0.2.10,addr=192.0.2.11"},
	} {
		h := hostfake.New(host.Options{})
		if _, err := sshd.ReadUser(ctx, h, "", tc.user, tc.addr); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("ReadUser(%q, %q) = %v, want fs.ErrInvalid", tc.user, tc.addr, err)
		}
		checkRuns(t, h)
	}
	for _, tc := range []struct{ out, want string }{
		{strings.Replace(alpineT, "permitrootlogin prohibit-password", "permitrootlogin maybe", 1), "permitrootlogin"},
		{strings.Replace(alpineT, "strictmodes yes", "strictmodes 1", 1), "strictmodes"},
		{strings.Replace(alpineT, "requiredrsasize 1024", "requiredrsasize big", 1), "requiredrsasize"},
	} {
		if _, err := sshd.ReadUser(ctx, userHost(tc.out), "", "george", "192.0.2.10"); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ReadUser = %v, want an error naming %s", err, tc.want)
		}
	}
	h := hostfake.New(host.Options{})
	h.Respond(hostfake.Response{Stdout: alpineT}, "/usr/sbin/sshd.pam", "-T", "-f", sshd.MainConfig, "-C", georgeSpec)
	if _, err := sshd.ReadUser(ctx, h, "/usr/sbin/sshd.pam", "george", "192.0.2.10"); err != nil {
		t.Errorf("ReadUser with a binary: %v", err)
	}
}

// TestListed: AllowUsers and DenyUsers' patterns against george from
// 192.0.2.10, in the forms the VMs printed (01-inspect-b-*.log).
func TestListed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		patterns []string
		ok       bool
		host     string
		named    bool
	}{
		{"the user alone", []string{"george"}, true, "", false},
		{"an exact address", []string{"george@192.0.2.10"}, true, "", false},
		{"another address", []string{"george@192.0.2.11"}, false, "192.0.2.11", false},
		{"a CIDR in", []string{"george@192.0.2.0/24", "nobody"}, true, "", false},
		{"a CIDR out", []string{"george@198.51.100.0/24", "nobody"}, false, "198.51.100.0/24", false},
		{"an address wildcard", []string{"george@192.0.2.*"}, true, "", false},
		{"a user wildcard", []string{"geo*@192.0.2.1?"}, true, "", false},
		{"any host", []string{"george@*"}, true, "", false},
		{"a host name", []string{"george@client.example"}, false, "client.example", true},
		{"a host-name wildcard", []string{"george@*.example"}, false, "*.example", true},
		{"another user", []string{"alice", "carol@192.0.2.10"}, false, "", false},
		{"a user wildcard elsewhere", []string{"a*"}, false, "", false},
		{"a negated user", []string{"!alice"}, false, "", false},
		{"a negated user named", []string{"!george"}, false, "", false},
		{"a negated address", []string{"george@!192.0.2.10,192.0.2.0/24"}, false, "!192.0.2.10,192.0.2.0/24", false},
		{"a negated address elsewhere", []string{"george@!192.0.2.11,192.0.2.0/24"}, true, "", false},
		{"a negation alone", []string{"george@!198.51.100.7"}, false, "!198.51.100.7", false},
		{"several, the second", []string{"george@client.example", "alice", "george@192.0.2.0/24"}, true, "", false},
		{"several, first host kept", []string{"george@198.51.100.7", "george@client.example"}, false, "198.51.100.7", false},
		{"none", nil, false, "", false},
	} {
		ok, host, named := sshd.Listed(tc.patterns, "george", "192.0.2.10")
		if ok != tc.ok || host != tc.host || named != tc.named {
			t.Errorf("%s: Listed(%q) = %v, %q, %v; want %v, %q, %v", tc.name, tc.patterns, ok, host, named, tc.ok, tc.host, tc.named)
		}
	}
	if ok, _, _ := sshd.Listed([]string{"george@2001:db8::/32"}, "george", "2001:db8::5"); !ok {
		t.Error("an IPv6 CIDR: not matched")
	}
	if ok, _, named := sshd.Listed([]string{"george@2001:db8::*"}, "george", "2001:db8::5"); !ok || named {
		t.Error("an IPv6 wildcard: not matched as an address")
	}
}

func TestMethodsMet(t *testing.T) {
	key := map[string]bool{"publickey": true}
	pwd := map[string]bool{"password": true}
	both := map[string]bool{"publickey": true, "password": true}
	kbd := map[string]bool{"keyboard-interactive": true}
	for _, tc := range []struct {
		lists []string
		have  map[string]bool
		want  bool
	}{
		{[]string{"any"}, key, true},
		{nil, pwd, true},
		{[]string{"any"}, map[string]bool{"publickey": false}, false},
		{[]string{"publickey,password", "publickey"}, key, true},
		{[]string{"publickey,password"}, key, false},
		{[]string{"publickey,password"}, both, true},
		{[]string{"publickey,publickey"}, key, true},
		{[]string{"keyboard-interactive:pam"}, kbd, true},
		{[]string{"password", "keyboard-interactive:bsdauth"}, kbd, true},
		{[]string{"publickey"}, pwd, false},
	} {
		if got := sshd.MethodsMet(tc.lists, tc.have); got != tc.want {
			t.Errorf("MethodsMet(%q, %v) = %v, want %v", tc.lists, tc.have, got, tc.want)
		}
	}
}

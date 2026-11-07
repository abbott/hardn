package ssh_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// CVE-2026-123158's accounts: root locked, alice "*" with a login shell
// (the target), bob a SHA-512 hash (the donor). Synthetic values only.
const (
	bypassPasswd = "root:x:0:0:root:/root:/bin/sh\nalice:x:1000:1000::/home/alice:/bin/ash\nbob:x:1001:1001::/home/bob:/bin/ash\n"
	bobHash      = "$6$Rt4mWq9x$Hn2Kd7Ls5Vb8Pc3Fz1Gy6Jx0Qe"
	bypassShadow = "root:!:20000:0:99999:7:::\nalice:*:20000:0:99999:7:::\nbob:" + bobHash + ":20000:0:99999:7:::\n"
	// unfixed is openssh-server one revision before 3.24's fix.
	unfixed = "10.3_p1-r1"
)

// exposedSite is alpine324 with the accounts above and openssh-server
// unfixed, sshd's stock files (password authentication on, no PAM).
func exposedSite() site {
	return site{plat: alpine324(), passwd: bypassPasswd, shadow: bypassShadow, apk: installedDB(unfixed)}
}

// TestBypass: SSH Auth's CVE-2026-123158 findings on Alpine, its state and
// detail as before: exposed (a "*" login account and a $6$ hash, the
// package unfixed), unfixed with no target or no donor, fixed, the
// package not established; none with PAM, with password authentication
// off; a Match block that allows passwords; root a target only with
// PermitRootLogin yes, globally or in a Match block; a nologin shell a
// target (sshd lets it in, and forwards for it), a shell not there and a
// locked field not targets; the accounts not read; off Alpine nothing.
func TestBypass(t *testing.T) {
	withShadow := func(s site, shadow string) site { s.shadow = shadow; return s }
	withPasswd := func(s site, passwd string) site { s.passwd = passwd; return s }
	withDrops := func(s site, kv ...string) site { s.drops = drops(kv...); return s }
	rootStar := "root:*:20000:0:99999:7:::\nalice:!:20000:0:99999:7:::\nbob:" + bobHash + ":20000:0:99999:7:::\n"
	exposedAlice := "anyone can log in as alice with a password of 257 to 1024 bytes (CVE-2026-123158)"
	lacks := "openssh-server 10.3_p1-r1 lacks the fix for CVE-2026-123158 (10.3_p1-r2)"
	for _, c := range []struct {
		r       row
		details []string
	}{
		{row{name: "exposed", site: exposedSite(), state: engine.NotConfigured, want: "password auth on", codes: []string{ssh.CodePasswordBypass}}, []string{exposedAlice}},
		{row{name: "carol's passwd field x, no shadow line", site: withPasswd(exposedSite(), bypassPasswd+"carol:x:1002:1002::/home/carol:/bin/sh\n"),
			state: engine.NotConfigured, want: "password auth on", codes: []string{ssh.CodePasswordBypass}}, []string{exposedAlice}},
		{row{name: "two targets", site: withShadow(withPasswd(exposedSite(), bypassPasswd+"carol:x:1002:1002::/home/carol:/bin/sh\n"), bypassShadow+"carol:*:20000:0:99999:7:::\n"),
			state: engine.NotConfigured, want: "password auth on", codes: []string{ssh.CodePasswordBypass}},
			[]string{"anyone can log in as alice, carol with a password of 257 to 1024 bytes (CVE-2026-123158)"}},
		{row{name: "no donor", site: withShadow(exposedSite(), "root:!:20000:0:99999:7:::\nalice:*:20000:0:99999:7:::\nbob:!:20000:0:99999:7:::\n"),
			state: engine.NotConfigured, want: "password auth on", codes: []string{ssh.CodeSSHDUnfixed}}, []string{lacks}},
		{row{name: "a yescrypt hash is no donor", site: withShadow(exposedSite(), "root:!:20000:0:99999:7:::\nalice:*:20000:0:99999:7:::\nbob:$y$j9T$salt$hash:20000:0:99999:7:::\n"),
			state: engine.NotConfigured, want: "password auth on", codes: []string{ssh.CodeSSHDUnfixed}}, []string{lacks}},
		{row{name: "no target: alice locked", site: withShadow(exposedSite(), "root:!:20000:0:99999:7:::\nalice:!:20000:0:99999:7:::\nbob:"+bobHash+":20000:0:99999:7:::\n"),
			state: engine.NotConfigured, want: "password auth on", codes: []string{ssh.CodeSSHDUnfixed}}, []string{lacks}},
		{row{name: "a nologin shell: a target, forwarding left", site: withPasswd(exposedSite(), "root:x:0:0:root:/root:/bin/sh\nalice:x:1000:1000::/home/alice:/sbin/nologin\nbob:x:1001:1001::/home/bob:/bin/ash\n"),
			state: engine.NotConfigured, want: "password auth on", codes: []string{ssh.CodePasswordBypass}}, []string{exposedAlice}},
		{row{name: "no target: a shell that isn't there", site: withPasswd(exposedSite(), "root:x:0:0:root:/root:/bin/sh\nalice:x:1000:1000::/home/alice:/bin/zsh\nbob:x:1001:1001::/home/bob:/bin/ash\n"),
			state: engine.NotConfigured, want: "password auth on", codes: []string{ssh.CodeSSHDUnfixed}}, []string{lacks}},
		{row{name: "fixed", site: site{plat: alpine324(), passwd: bypassPasswd, shadow: bypassShadow},
			state: engine.NotConfigured, want: "password auth on"}, nil},
		{row{name: "a later revision", site: site{plat: alpine324(), passwd: bypassPasswd, shadow: bypassShadow, apk: installedDB("10.3_p1-r5")},
			state: engine.NotConfigured, want: "password auth on"}, nil},
		{row{name: "no installed database", site: site{plat: alpine324(), passwd: bypassPasswd, shadow: bypassShadow, noAPK: true},
			state: engine.NotConfigured, want: "password auth on", codes: []string{ssh.CodeFixUnknown}},
			[]string{"can't tell if openssh-server has the fix for CVE-2026-123158: /lib/apk/db/installed is missing"}},
		{row{name: "no openssh-server entry", site: site{plat: alpine324(), passwd: bypassPasswd, shadow: bypassShadow, apk: "P:musl\nV:1.2.5-r10\n"},
			state: engine.NotConfigured, want: "password auth on", codes: []string{ssh.CodeFixUnknown}},
			[]string{"can't tell if openssh-server has the fix for CVE-2026-123158: openssh-server isn't in /lib/apk/db/installed"}},
		{row{name: "a version not compared", site: site{plat: alpine324(), passwd: bypassPasswd, shadow: bypassShadow, apk: installedDB("10.3_p1-r1~1")},
			state: engine.NotConfigured, want: "password auth on", codes: []string{ssh.CodeFixUnknown}},
			[]string{"can't tell if openssh-server has the fix for CVE-2026-123158: version 10.3_p1-r1~1 isn't one hardn compares"}},
		{row{name: "PAM: sshd.pam with UsePAM yes", site: func() site { s := withDrops(exposedSite(), "10-site.conf", "UsePAM yes\n"); s.pam = true; return s }(),
			state: engine.NotConfigured, want: "password auth on"}, nil},
		{row{name: "password authentication off", site: withDrops(exposedSite(), "10-site.conf", "PasswordAuthentication no\n"),
			state: engine.Configured, want: "key-only auth"}, nil},
		{row{name: "a Match block allows passwords", site: withDrops(exposedSite(), "10-site.conf", "PasswordAuthentication no\nMatch User alice\n\tPasswordAuthentication yes\n"),
			state: engine.Partial, want: "match block allows passwords", codes: []string{ssh.CodeMatchAllowsMore, ssh.CodePasswordBypass}}, []string{"", exposedAlice}},
		{row{name: "a Match block, keyboard-interactive off", site: withDrops(exposedSite(), "10-site.conf", "PasswordAuthentication no\nKbdInteractiveAuthentication no\nMatch User alice\n\tPasswordAuthentication yes\n"),
			state: engine.Partial, want: "match block allows passwords", codes: []string{ssh.CodeMatchAllowsMore, ssh.CodePasswordBypass}}, []string{"", exposedAlice}},
		{row{name: "root, PermitRootLogin yes", site: withDrops(withShadow(exposedSite(), rootStar), "10-site.conf", "PermitRootLogin yes\n"),
			state: engine.NotConfigured, want: "password auth on", codes: []string{ssh.CodePasswordBypass}},
			[]string{"anyone can log in as root with a password of 257 to 1024 bytes (CVE-2026-123158)"}},
		{row{name: "root, PermitRootLogin yes in a Match block", site: withDrops(withShadow(exposedSite(), rootStar), "10-site.conf", "Match Address 192.0.2.0/24\n\tPermitRootLogin yes\n"),
			state: engine.NotConfigured, want: "password auth on", codes: []string{ssh.CodePasswordBypass}},
			[]string{"anyone can log in as root with a password of 257 to 1024 bytes (CVE-2026-123158)"}},
		{row{name: "root, prohibit-password", site: withShadow(exposedSite(), rootStar),
			state: engine.NotConfigured, want: "password auth on", codes: []string{ssh.CodeSSHDUnfixed}}, []string{lacks}},
		{row{name: "the accounts not read", site: func() site { s := exposedSite(); s.unreadable = accounts.ShadowFile; return s }(),
			state: engine.NotConfigured, want: "password auth on", codes: []string{ssh.CodeSSHDUnfixed, ssh.CodeFixUnknown}},
			[]string{lacks, "can't tell who can log in with a long password (CVE-2026-123158): open /etc/shadow: permission denied"}},
		{row{name: "off Alpine", site: site{plat: debian13(), passwd: bypassPasswd, shadow: bypassShadow, apk: installedDB(unfixed)},
			state: engine.NotConfigured, want: "password auth on"}, nil},
	} {
		t.Run(c.r.name, func(t *testing.T) {
			c.r.ctl = auth
			st := run(t, c.r)
			for i, d := range c.details {
				if d != "" && (i >= len(st.Findings) || st.Findings[i].Detail != d) {
					t.Errorf("finding %d: %+v, want %q", i, st.Findings, d)
				}
			}
			for _, e := range st.Evidence {
				if strings.Contains(e, bobHash) || strings.Contains(e, "$6$") {
					t.Errorf("evidence holds a hash: %q", e)
				}
			}
			if slices.Contains(c.r.codes, ssh.CodePasswordBypass) && !slices.Contains(st.Evidence, "/etc/shadow:3: bob SHA-crypt hash") {
				t.Errorf("evidence %q: no donor", st.Evidence)
			}
		})
	}
}

// TestReadBypass: what Create asks (pkg/engine/controls/users): off Alpine
// nothing; on Alpine PAM, the password path (on, off, a Match block, not
// read) and the fix of the package of the sshd OpenRC's script runs
// (openssh-server-pam for sshd.pam); NoSSHD, with the read's error, where
// sshd is not installed or there is no sshd_config (an Alpine LXC with no
// openssh, 2026-11-07), and not for a read that fails otherwise.
func TestReadBypass(t *testing.T) {
	ctx := context.Background()
	noOpenSSH := "P:musl\nV:1.2.5-r10\n"
	notInstalled := &hostfake.Response{Err: fmt.Errorf("host: run /usr/sbin/sshd: %w", host.ErrNotFound)}
	for _, c := range []struct {
		name     string
		s        site
		applies  bool
		pam      detect.Tri
		password detect.Tri
		pkg      string
		fixed    detect.Tri
		fails    bool // ReadBypass returns the read's error
		noSSHD   bool
	}{
		{"off Alpine", site{plat: debian13()}, false, detect.Unknown, detect.Unknown, "", detect.Unknown, false, false},
		{"stock Alpine, fixed", site{plat: alpine324()}, true, detect.No, detect.Yes, alpine.OpenSSHServer, detect.Yes, false, false},
		{"unfixed", exposedSite(), true, detect.No, detect.Yes, alpine.OpenSSHServer, detect.No, false, false},
		{"password authentication off", site{plat: alpine324(), drops: drops("10-site.conf", "PasswordAuthentication no\n")}, true, detect.No, detect.No, alpine.OpenSSHServer, detect.Yes, false, false},
		{"a Match block", site{plat: alpine324(), drops: drops("10-site.conf", "PasswordAuthentication no\nMatch User alice\n\tPasswordAuthentication yes\n")}, true, detect.No, detect.Yes, alpine.OpenSSHServer, detect.Yes, false, false},
		{"a Match block's value not ranked", site{plat: alpine324(), drops: drops("10-site.conf", "PasswordAuthentication no\nMatch User alice\n\tPasswordAuthentication maybe\n")}, true, detect.No, detect.Unknown, alpine.OpenSSHServer, detect.Yes, false, false},
		{"sshd.pam", site{plat: alpine324(), pam: true, drops: drops("10-site.conf", "UsePAM yes\n")}, true, detect.Yes, detect.Yes, alpine.OpenSSHServerPAM, detect.Unknown, false, false},
		{"no database", site{plat: alpine324(), noAPK: true}, true, detect.No, detect.Yes, alpine.OpenSSHServer, detect.Unknown, false, false},
		{"sshd not installed", site{plat: alpine324(), failT: notInstalled, apk: noOpenSSH}, true, detect.Unknown, detect.Unknown, alpine.OpenSSHServer, detect.Unknown, true, true},
		{"no sshd_config", site{plat: alpine324(), noMain: true, apk: noOpenSSH}, true, detect.Unknown, detect.Unknown, alpine.OpenSSHServer, detect.Unknown, true, true},
		{"no sshd_config, no sshd", site{plat: alpine324(), noMain: true, failT: notInstalled, apk: noOpenSSH}, true, detect.Unknown, detect.Unknown, alpine.OpenSSHServer, detect.Unknown, true, true},
		{"sshd -T fails otherwise", site{plat: alpine324(), failT: &hostfake.Response{Stderr: "/etc/ssh/sshd_config line 3: Bad configuration option: Foo\n", ExitCode: 255}}, true, detect.Unknown, detect.Unknown, alpine.OpenSSHServer, detect.Yes, true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, facts := c.s.build(t)
			b, err := ssh.ReadBypass(ctx, f, facts)
			if (err != nil) != c.fails || b.NoSSHD != c.noSSHD || b.Applies != c.applies || b.PAM.Uses != c.pam || b.Password != c.password || b.Fix.Package != c.pkg || b.Fix.Fixed != c.fixed {
				t.Errorf("ReadBypass = %+v, %v", b, err)
			}
			readOnly(t, f, c.s.plat.bin)
		})
	}
}

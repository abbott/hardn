package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/netip"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/authkeys"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
)

// The SSH Login mock's hosts (refactor plan slice 6, unit (a),
// next/s6-gallery; docs/engine/gallery-ssh.md): usersOn's hosts in the
// shapes the three VMs showed on 2026-11-03 (vm-runs/s6/00-inspect-*.log),
// their values synthetic. What the fake answers beyond usersOn's is
// sshSim's, an addition, each answer's source beside it: sshd -T -C with
// the Match blocks applied, sshd's log of the session's login, and sshd -T
// failing where /run/sshd is not there yet.

// sshSim is what the fake host answers for the SSH Login mock: sshd's log
// and the Match blocks of sshd -T -C. Nil on every other host.
type sshSim struct {
	// log is sshd's log of the session's login, one line: journalctl -u
	// ssh's on systemd, /var/log/messages' on Alpine (written as a file).
	log string
}

// answer answers argv when it is the mock's, or reports that it is not.
func (s *sshSim) answer(h *fakeHost, argv []string) (hostfake.Response, bool) {
	if s == nil {
		return hostfake.Response{}, false
	}
	switch path.Base(argv[0]) {
	case "ss": // the Alpine VM has no ss, busybox's netstat alone (00-inspect-alpine323.log)
		if h.openRC {
			return hostfake.Response{Err: host.ErrNotFound, ExitCode: -1}, true
		}
	case "journalctl": // journalctl -u ssh: sshd's lines, as the Debian and Ubuntu VMs showed them
		if slices.Contains(argv, "-u") && !h.openRC {
			return hostfake.Response{Stdout: s.log}, true
		}
	case "sshd", "sshd.pam":
		// sshd.c's fatal() where the privilege separation directory is
		// missing: Ubuntu's ssh.socket creates /run/sshd at the first
		// connection (RuntimeDirectory of ssh.service), so sshd -T fails
		// before it (reload.md §2.4).
		if _, err := h.f.Stat(context.Background(), "/run/sshd"); err != nil {
			return hostfake.Response{Stderr: "Missing privilege separation directory: /run/sshd\r\n", ExitCode: 255}, true
		}
		if i := slices.Index(argv, "-C"); i >= 0 && i+1 < len(argv) && slices.Contains(argv, "-T") {
			main, drops := h.files(argv[slices.Index(argv, "-f")+1])
			m := matched(main, drops, argv[i+1])
			out, _ := h.sshdT(m, drops)
			// requiredrsasize, which the model does not print: the first
			// value, OpenSSH 9.1's default 1024 where none is set.
			size := strconv.Itoa(authkeys.MinRSABits)
			for _, l := range sshd.ReadOrder(m, drops) {
				if l.Match == nil && l.Keyword == "requiredrsasize" && len(l.Args) > 0 {
					size = l.Args[0]
					break
				}
			}
			return hostfake.Response{Stdout: out + "requiredrsasize " + size + "\n"}, true
		}
	}
	return hostfake.Response{}, false
}

// matched is main with the directives of the Match blocks spec's connection
// matches ("user=george,host=192.0.2.10,addr=192.0.2.10") put first, so that
// sshdT, which reads the first value outside Match blocks, prints them:
// sshd_config(5), "the keywords on the following lines override those set
// in the global section", the first matching block's value winning
// (servconf.c parses the matching blocks into one set of options, first
// value kept, then copies what it set over the global ones). The criteria
// modelled are All, User, Group (not matched: no group is read), Address and
// Host (the address, UseDNS no); a negated pattern is honoured.
func matched(main []byte, drops []sshd.File, spec string) []byte {
	conn := map[string]string{}
	for _, kv := range strings.Split(spec, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			conn[k] = v
		}
	}
	var first []string
	for _, l := range sshd.ReadOrder(main, drops) {
		if l.Match == nil || !matches(l.Match.Text, conn) {
			continue
		}
		first = append(first, l.Keyword+" "+strings.Join(l.Args, " "))
	}
	if len(first) == 0 {
		return main
	}
	return []byte(strings.Join(first, "\n") + "\n" + string(main))
}

// matches reports whether a Match line's criteria all match conn.
func matches(line string, conn map[string]string) bool {
	f := strings.Fields(line)
	if len(f) < 2 {
		return false
	}
	for i := 1; i < len(f); i++ {
		crit := strings.ToLower(f[i])
		if crit == "all" {
			continue
		}
		if i+1 >= len(f) {
			return false
		}
		list := f[i+1]
		i++
		switch crit {
		case "user":
			if !inList(conn["user"], list, func(s, p string) bool { ok, _ := path.Match(p, s); return ok }) {
				return false
			}
		case "address", "host":
			if !inList(conn["addr"], list, addrMatch) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// inList is match_pattern_list's answer: a negated pattern that matches
// refuses, else any pattern that matches.
func inList(s, list string, match func(s, p string) bool) bool {
	found := false
	for _, p := range strings.Split(list, ",") {
		neg := strings.HasPrefix(p, "!")
		if match(s, strings.TrimPrefix(p, "!")) {
			if neg {
				return false
			}
			found = true
		}
	}
	return found
}

// addrMatch matches an address against a CIDR or a wildcard pattern.
func addrMatch(s, p string) bool {
	if pr, err := netip.ParsePrefix(p); err == nil {
		a, err := netip.ParseAddr(s)
		return err == nil && pr.Contains(a)
	}
	ok, _ := path.Match(p, s)
	return ok
}

// The session's login, as sshd logs it (auth2.c, "Accepted %s for %s from
// %s port %d ssh2: %s %s"), its key a synthetic one's fingerprint.
const sshSessionPort = "50022"

// acceptedLine is sshd's line for the session's login: method, user, and
// the key's type and fingerprint for a key.
func acceptedLine(openRC bool, method, user, port string, seed byte) string {
	what := ""
	if method == "publickey" {
		k := synthKey(seed, "")
		blob, _ := base64.StdEncoding.DecodeString(strings.Fields(k)[1])
		sum := sha256.Sum256(blob)
		what = ": ED25519 SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	}
	line := fmt.Sprintf("sshd-session[3900]: Accepted %s for %s from 192.0.2.10 port %s ssh2%s", method, user, port, what)
	if openRC {
		return "Oct 20 11:58:00 " + hostName + " auth.info " + line + "\n"
	}
	return "2026-10-20T11:58:00+00:00 " + hostName + " " + line + "\n"
}

// withLog gives h sshSim, its session logged in by method as user with the
// key of seed.
func (h *fakeHost) withLog(method, user string, seed byte) *fakeHost {
	port := sshSessionPort
	if h.f != nil {
		if e, ok := h.f.Entry(sshd.HardnDropIn); ok && strings.Contains(string(e.Data), "Port 2230") {
			port = "50123" // confirmConn's: the session on 2230
		}
	}
	line := acceptedLine(h.openRC, method, user, port, seed)
	h.ssh = &sshSim{log: line}
	if h.openRC {
		h.f.AddFile("/var/log/messages", []byte(line), 0o640, 0, 0)
	}
	return h
}

// onConsole makes hardn's session root's on the console, not over SSH:
// hardn's process under login(1) on tty1, no SSH_CONNECTION in either's
// environment, and sshd's log without a line for it.
func (h *fakeHost) onConsole() *fakeHost {
	const loginPID = 4990
	h.f.AddFile(fmt.Sprintf("/proc/%d/stat", applyPID), statLine(applyPID, "hardn", loginPID, 4<<8|1, applyStart), 0o444, 0, 0)
	h.process(loginPID, "login", 1, 1500)
	for _, pid := range []int{applyPID, loginPID} {
		h.f.AddFile(fmt.Sprintf("/proc/%d/environ", pid), []byte("HOME=/root\x00TERM=linux\x00"), 0o400, 0, 0)
	}
	h.ssh = &sshSim{}
	if h.openRC {
		h.f.AddFile("/var/log/messages", nil, 0o640, 0, 0)
	}
	return h
}

// rsaKey is a synthetic ssh-rsa public key line whose modulus is bits
// long, every byte seed's but the first.
func rsaKey(bits int, seed byte, comment string) string {
	blob := binary.BigEndian.AppendUint32(nil, 7)
	blob = append(blob, "ssh-rsa"...)
	blob = binary.BigEndian.AppendUint32(blob, 3)
	blob = append(blob, 1, 0, 1)
	blob = binary.BigEndian.AppendUint32(blob, uint32(bits/8+1))
	blob = append(blob, 0, 0x80|seed)
	for range bits/8 - 1 {
		blob = append(blob, seed)
	}
	return "ssh-rsa " + base64.StdEncoding.EncodeToString(blob) + " " + comment
}

// rootLike gives root the shadow field pw and, as on the three VMs, the
// login user's first key (seed 1) as its one key.
func (h *fakeHost) rootLike(pw string) *fakeHost {
	data, _ := h.f.ReadFile(context.Background(), accounts.ShadowFile)
	lines := strings.Split(string(data), "\n")
	for i, l := range lines {
		if f := strings.SplitN(l, ":", 3); len(f) == 3 && f[0] == "root" {
			lines[i] = "root:" + pw + ":" + f[2]
		}
	}
	h.f.AddFile(accounts.ShadowFile, []byte(strings.Join(lines, "\n")), 0o640, 0, 42)
	h.f.AddFile("/root/.ssh/authorized_keys", []byte(synthKey(1, "george@laptop-1")+"\n"), 0o600, 0, 0)
	return h
}

// sshFile writes a drop-in or sshd_config and boots, so sshd runs with it.
func (h *fakeHost) sshFile(name, data string, mode int) *fakeHost {
	h.f.AddFile(name, []byte(data), fsMode(mode), 0, 0)
	h.boot()
	return h
}

// debianMain is the Debian 13 VM's sshd_config as sshd reads it: the
// stock Include first, later PasswordAuthentication no,
// KbdInteractiveAuthentication no and UsePAM yes.
const debianMain = sshd.IncludeLine + "\n\nKbdInteractiveAuthentication no\nPasswordAuthentication no\nUsePAM yes\n"

// sshYML is the Debian 13 VM's old /etc/hardn/hardn.yml's SSH keys, as it
// had them (permitRootLogin the bool).
const sshYML = "permitRootLogin: false\nsshPort: 22\nsshAllowedUsers: []\nsshListenAddress: 0.0.0.0\nsshKeyPath: .ssh_%u\n" +
	"sshConfigFile: /etc/ssh/sshd_config.d/hardn.conf\ndisableRootSSH: false\n"

// sshDebian13 is the Debian 13 VM: george (key, "!", sudo by cloud-init),
// alice (a password, no key, sudo), root "*" with george's first key, ufw
// active with 22 from anywhere, the old hardn.yml; george's session by key.
func sshDebian13() *fakeHost {
	h := usersOn(debian13Platform, true, george(debian13Platform), alice()).rootLike("*")
	h.sshFile(sshd.MainConfig, debianMain, 0o644)
	return h.withHardnYML(sshYML).withLog("publickey", "george", 1)
}

// sshUbuntu is the Ubuntu 26.04 VM: ssh.socket, 60-cloudimg-settings.conf
// refusing passwords, prohibit-password; otherwise as Debian 13.
func sshUbuntu() *fakeHost {
	h := usersOn(ubuntu2604Platform, true, george(ubuntu2604Platform), alice()).rootLike("*")
	h.inactive, h.allowed = false, []uint16{22}
	h.saveRules()
	h.f.AddFile(sshd.MainConfig, []byte(debianMain), 0o644, 0, 0)
	h.sshFile(sshd.DropInDir+"/60-cloudimg-settings.conf", "PasswordAuthentication no\n", 0o644)
	return h.withLog("publickey", "george", 1)
}

// sshAlpine is the Alpine 3.23 VM: sshd without PAM, 50-cloud-init.conf
// refusing passwords (0600), AllowTcpForwarding no, root "!" with a key,
// george "*" in wheel; no ufw.
func sshAlpine() *fakeHost {
	h := alpineVM().rootLike("!")
	h.f.AddFile(sshd.MainConfig, []byte(stockMain+"AuthorizedKeysFile .ssh/authorized_keys\nAllowTcpForwarding no\n"), 0o644, 0, 0)
	h.sshFile(sshd.DropInDir+"/50-cloud-init.conf", "PasswordAuthentication no\n", 0o600)
	return h.withLog("publickey", "george", 1)
}

// sshHardened is Figma's state: 00-hardn.conf with Port 2230,
// PermitRootLogin no and PasswordAuthentication no, ufw allowing 2230.
func sshHardened() *fakeHost {
	h := sshDebian13()
	h.sshFile(sshd.HardnDropIn, "Port 2230\nPermitRootLogin no\nPasswordAuthentication no\n", 0o644)
	h.allowed = []uint16{2230}
	h.saveRules()
	return h.withLog("publickey", "george", 1)
}

// sshPasswordLogin is Debian 13 installed from the ISO: cloud-init's
// 50-cloud-init.conf allowing passwords, read before sshd_config's no.
func sshPasswordLogin() *fakeHost {
	return sshDebian13().sshFile(sshd.DropInDir+"/50-cloud-init.conf", "PasswordAuthentication yes\n", 0o600)
}

// sshPasswordSession is sshPasswordLogin, alice's session by password.
func sshPasswordSession() *fakeHost {
	return sshPasswordLogin().withLog("password", "alice", 0)
}

// sshRootSession is Debian 13, hardn run from root's own SSH login.
func sshRootSession() *fakeHost { return sshDebian13().withLog("publickey", "root", 1) }

// sshLegacyV032 is Ubuntu 26.04 hardened by v0.3.2: its manage.conf, with
// v0.3.2's AuthorizedKeysFile line (v032-reference.md §4.2, V5) and
// george's keys where v0.3.2 put them, and listen.conf for 2230 written
// with no daemon-reload, as d9Host. george's key login is one sshd reads.
func sshLegacyV032() *fakeHost {
	h := sshUbuntu()
	h.f.AddFile(migrate.ManageConf, []byte("### Reference\n### https://cryptsus.com/blog/how-to-secure-your-ssh-server-with-public-key-elliptic-curve-ed25519-crypto.html\n"+
		"Protocol 2\nStrictModes yes\n\nListenAddress 0.0.0.0:2230\n\nAuthenticationMethods publickey\nPubkeyAuthentication yes\n\nPermitRootLogin no\nAllowUsers george\n\n"+
		"PasswordAuthentication no\nPermitEmptyPasswords no\n\nAuthorizedKeysFile    .ssh/authorized_keys    .ssh_%u/authorized_keys\n"), 0o644, 0, 0)
	h.f.AddFile(migrate.ListenConf, []byte("[Socket]\nListenStream=\nListenStream=2230\n"), 0o644, 0, 0)
	// v0.3.2's keys: the user's ~/.ssh_<user>/authorized_keys (v032-reference.md §4.1).
	ctx := context.Background()
	data, _ := h.f.ReadFile(ctx, "/home/george/.ssh/authorized_keys")
	_ = h.f.Remove(ctx, "/home/george/.ssh/authorized_keys")
	h.f.AddDir("/home/george/.ssh_george", 0o700, 1000, 1000)
	h.f.AddFile("/home/george/.ssh_george/authorized_keys", data, 0o600, 1000, 1000)
	h.allowed = []uint16{22, 2230}
	h.saveRules()
	h.configured = []string{"[::]:22", "0.0.0.0:22"}
	h.bound = slices.Clone(h.configured)
	return h
}

// legacyMainConf is origin/main's hardn.conf as SaveSSHConfig writes it
// for Disable root SSH access (that build's DisableRootSSH: the stub's
// Port 22, no key path, so one "AuthorizedKeysFile .ssh/authorized_keys"),
// where that build's AddAuthorizedKey put george's keys (side-effects.md
// D7).
const legacyMainConf = "# SSH configuration managed by Hardn\n\nProtocol 2\nStrictModes yes\n\nPort 22\n\n" +
	"AuthenticationMethods publickey\nPubkeyAuthentication yes\n\nPermitRootLogin no\n\n" +
	"PasswordAuthentication no\nPermitEmptyPasswords no\n\nAuthorizedKeysFile .ssh/authorized_keys\n"

// sshLegacyMain is Debian 13 with origin/main's hardn.conf, beaten for
// PasswordAuthentication by 50-cloud-init.conf, which sshd reads first.
func sshLegacyMain() *fakeHost {
	h := sshPasswordLogin()
	return h.sshFile(sshd.DropInDir+"/hardn.conf", legacyMainConf, 0o644)
}

// sshIncludeOther is Debian 13 with a Port line above the stock Include
// (P-38).
func sshIncludeOther() *fakeHost {
	return sshDebian13().sshFile(sshd.MainConfig, "Port 22\n"+debianMain, 0o644)
}

// sshMatchAllows is Debian 13 with a Match block letting root and
// passwords in from 192.0.2.0/24.
func sshMatchAllows() *fakeHost {
	return sshDebian13().sshFile(sshd.DropInDir+"/20-site.conf", "Match Address 192.0.2.0/24\n    PermitRootLogin yes\n    PasswordAuthentication yes\n", 0o644)
}

// sshAllowUsers is Debian 13 with AllowUsers george; alice, an admin with a
// key here, left out.
func sshAllowUsers() *fakeHost {
	a := alice()
	a.keys = []byte{3}
	h := usersOn(debian13Platform, true, george(debian13Platform), a).rootLike("*")
	h.sshFile(sshd.MainConfig, debianMain, 0o644)
	h.sshFile(sshd.DropInDir+"/10-allow.conf", "AllowUsers george\n", 0o644)
	return h.withLog("publickey", "george", 1)
}

// sshPVE is a Proxmox VE 9 node: root alone, by key, as hardn runs.
func sshPVE() *fakeHost {
	return usersOn(pve9Platform, false).rootLike("*").withLog("publickey", "root", 1)
}

// sshUFWRestricted is Debian 13 with ufw allowing 22 from 192.0.2.0/24
// alone.
func sshUFWRestricted() *fakeHost {
	return simulateUFW(sshDebian13(), ufwSim{incoming: "deny", active: true, enabled: true,
		rules: []simRule{{action: "allow", port: "22", src: "192.0.2.0/24"}}})
}

// sshNotRead is Ubuntu 26.04 before its first connection: no /run/sshd,
// so sshd -T fails.
func sshNotRead() *fakeHost {
	h := sshUbuntu()
	_ = h.f.Remove(context.Background(), "/run/sshd")
	return h
}

// sshNoPAMLocked is Alpine with george's account "!" and george the only
// admin: alpine is out of wheel. george cannot have logged in by key, so
// hardn runs as root on the console.
func sshNoPAMLocked() *fakeHost {
	p := alpine323CloudPlatform
	g := george(p)
	g.pw = "!"
	h := sshAlpine()
	data, _ := h.f.ReadFile(context.Background(), accounts.ShadowFile)
	h.f.AddFile(accounts.ShadowFile, []byte(strings.Replace(string(data), "george:*:", "george:!:", 1)), 0o640, 0, 42)
	data, _ = h.f.ReadFile(context.Background(), "/etc/group")
	h.f.AddFile("/etc/group", []byte(strings.Replace(string(data), "wheel:x:10:root,alpine,george", "wheel:x:10:root,george", 1)), 0o644, 0, 0)
	return h.onConsole()
}

// sshRSASmall is Debian 13 with george's only key RSA 1024 bits and
// RequiredRSASize 2048 in a drop-in: sshd refuses the key, so hardn runs
// as root on the console.
func sshRSASmall() *fakeHost {
	h := sshDebian13()
	h.f.AddFile("/home/george/.ssh/authorized_keys", []byte(rsaKey(1024, 0x41, "george@laptop-1")+"\n"), 0o600, 1000, 1000)
	return h.sshFile(sshd.DropInDir+"/10-rsa.conf", "RequiredRSASize 2048\n", 0o644).onConsole()
}

// sshAdminNoSudo is Debian 13 with george alone, by key, his only grant
// %sudo, which asks a password his "!" account hasn't got; hardn runs as
// root on the console.
func sshAdminNoSudo() *fakeHost {
	h := usersOn(debian13Platform, false, george(debian13Platform)).rootLike("*")
	h.sshFile(sshd.MainConfig, debianMain, 0o644)
	return h.withHardnYML(sshYML).onConsole()
}

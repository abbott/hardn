package ssh_test

import (
	"context"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
)

// The hosts, as the gallery's SSH Login hosts (sshHosts, then the variants
// its refusals read), each as its note there says.

// sshHosts are the gallery's hosts, in its menu's order.
var sshHosts = []string{"debian13", "ubuntu2604", "alpine323", "hardened", "password-login", "password-session", "root-session",
	"legacy-v032", "legacy-main", "include-other", "match-allows", "allow-users", "pve9", "ufw-restricted", "confirm-window",
	"not-read", "nopam-locked", "rsa-small", "admin-no-sudo"}

// debianMain is the Debian 13 VM's sshd_config as sshd reads it: the
// stock Include first, later PasswordAuthentication no,
// KbdInteractiveAuthentication no and UsePAM yes.
const debianMain = sshd.IncludeLine + "\n\nKbdInteractiveAuthentication no\nPasswordAuthentication no\nUsePAM yes\n"

// legacyMainConf is origin/main's hardn.conf as its SaveSSHConfig wrote it
// for Disable root SSH access.
const legacyMainConf = "# SSH configuration managed by Hardn\n\nProtocol 2\nStrictModes yes\n\nPort 22\n\n" +
	"AuthenticationMethods publickey\nPubkeyAuthentication yes\n\nPermitRootLogin no\n\n" +
	"PasswordAuthentication no\nPermitEmptyPasswords no\n\nAuthorizedKeysFile .ssh/authorized_keys\n"

// newHost is the host name names, its session's login logged.
func newHost(t *testing.T, name string) *sshHost {
	t.Helper()
	ctx := context.Background()
	deb, ubu := debian13(), ubuntu2604()
	vm := func(p platform) *sshHost {
		h := usersOn(p, true, george(p), alice()).rootLike("*")
		h.env = map[string]string{"SUDO_USER": "george"}
		return h.sshFile(sshd.MainConfig, debianMain, 0o644)
	}
	switch name {
	case "debian13":
		return vm(deb).withLog("publickey", "george", 1)
	case "ubuntu2604":
		return vm(ubu).sshFile(sshd.DropInDir+"/60-cloudimg-settings.conf", "PasswordAuthentication no\n", 0o644).withLog("publickey", "george", 1)
	case "alpine323":
		return alpineVM().withLog("publickey", "george", 1)
	case "hardened":
		h := vm(deb).sshFile(sshd.HardnDropIn, "Port 2230\nPermitRootLogin no\nPasswordAuthentication no\n", 0o644)
		h.rules, h.sess = []rule{{port: 2230}}, confirmSession
		h.saveRules()
		return h.withLog("publickey", "george", 1)
	case "password-login":
		return passwordLogin(deb).withLog("publickey", "george", 1)
	case "password-session":
		h := passwordLogin(deb)
		h.env["SUDO_USER"] = "alice"
		return h.withLog("password", "alice", 0)
	case "root-session":
		h := vm(deb)
		h.env = nil
		return h.withLog("publickey", "root", 1)
	case "legacy-v032":
		h := vm(ubu).sshFile(sshd.DropInDir+"/60-cloudimg-settings.conf", "PasswordAuthentication no\n", 0o644)
		h.AddFile(migrate.ManageConf, []byte("### Reference\n### https://cryptsus.com/blog/how-to-secure-your-ssh-server-with-public-key-elliptic-curve-ed25519-crypto.html\n"+
			"Protocol 2\nStrictModes yes\n\nListenAddress 0.0.0.0:2230\n\nAuthenticationMethods publickey\nPubkeyAuthentication yes\n\n"+
			"PermitRootLogin no\nAllowUsers george\n\nPasswordAuthentication no\nPermitEmptyPasswords no\n\nAuthorizedKeysFile    .ssh/authorized_keys    .ssh_%u/authorized_keys\n"), 0o644, 0, 0)
		h.AddFile(migrate.ListenConf, []byte("[Socket]\nListenStream=\nListenStream=2230\n"), 0o644, 0, 0)
		// v0.3.2's keys: the user's ~/.ssh_<user>/authorized_keys.
		data, _ := h.ReadFile(ctx, "/home/george/.ssh/authorized_keys")
		_ = h.Remove(ctx, "/home/george/.ssh/authorized_keys")
		h.AddDir("/home/george/.ssh_george", 0o700, 1000, 1000)
		h.AddFile("/home/george/.ssh_george/authorized_keys", data, 0o600, 1000, 1000)
		h.rules = []rule{{port: 22}, {port: 2230}}
		h.saveRules()
		h.configured = []string{"[::]:22", "0.0.0.0:22"}
		h.bound = []string{"[::]:22", "0.0.0.0:22"}
		return h.withLog("publickey", "george", 1)
	case "legacy-main":
		return passwordLogin(deb).sshFile(sshd.DropInDir+"/hardn.conf", legacyMainConf, 0o644).withLog("publickey", "george", 1)
	case "include-other":
		return vm(deb).sshFile(sshd.MainConfig, "Port 22\n"+debianMain, 0o644).withLog("publickey", "george", 1)
	case "match-allows":
		return vm(deb).sshFile(sshd.DropInDir+"/20-site.conf", "Match Address 192.0.2.0/24\n    PermitRootLogin yes\n    PasswordAuthentication yes\n", 0o644).
			withLog("publickey", "george", 1)
	case "allow-users":
		a := alice()
		a.keys = []byte{3}
		h := usersOn(deb, true, george(deb), a).rootLike("*")
		h.env = map[string]string{"SUDO_USER": "george"}
		h.sshFile(sshd.MainConfig, debianMain, 0o644)
		return h.sshFile(sshd.DropInDir+"/10-allow.conf", "AllowUsers george\n", 0o644).withLog("publickey", "george", 1)
	case "pve9":
		return pveNode("*").withLog("publickey", "root", 1)
	case "pve9-password":
		return pveNode(syntheticHash).withLog("password", "root", 0)
	case "ufw-restricted":
		h := vm(deb)
		h.rules = []rule{{port: 22, src: "192.0.2.0/24"}}
		h.saveRules()
		return h.withLog("publickey", "george", 1)
	case "confirm-window":
		return vm(deb).withLog("publickey", "george", 1) // the port change is applied by the test (confirmWindow)
	case "not-read":
		h := vm(ubu).sshFile(sshd.DropInDir+"/60-cloudimg-settings.conf", "PasswordAuthentication no\n", 0o644).withLog("publickey", "george", 1)
		_ = h.Remove(ctx, "/run/sshd")
		return h
	case "no-hostkeys-alpine": // sshd installed, never started: OpenRC's sshd neither started nor in a runlevel, no host keys
		h := alpineVM().withLog("publickey", "george", 1)
		h.noKeys = true
		h.plat.facts.SSH = detect.SSH{OpenRC: detect.OpenRCService{Started: detect.No, InRunlevel: detect.No}, Err: detect.ErrNoSignal}
		h.AddDir("/etc/apk", 0o755, 0, 0)
		h.AddFile(alpine.World, []byte("alpine-base\ndoas\nopenssh\nopenssh-server-pam\n"), 0o644, 0, 0)
		return h
	case "no-hostkeys-debian": // ssh.service and ssh.socket disabled and inactive, no host keys
		h := vm(deb).withLog("publickey", "george", 1)
		h.noKeys = true
		down := detect.UnitState{Enabled: "disabled", Active: "inactive"}
		h.plat.facts.SSH = detect.SSH{Generator: detect.No, Socket: down, Service: down, Err: detect.ErrNoSignal}
		return h
	case "alpine-unfixed":
		return alpineVM().unfixedSSH().withLog("publickey", "george", 1)
	case "nopam-locked":
		h := alpineVM()
		data, _ := h.ReadFile(ctx, accounts.ShadowFile)
		h.AddFile(accounts.ShadowFile, []byte(strings.Replace(string(data), "george:*:", "george:!:", 1)), 0o640, 0, 42)
		data, _ = h.ReadFile(ctx, "/etc/group")
		h.AddFile("/etc/group", []byte(strings.Replace(string(data), "wheel:x:10:root,alpine,george", "wheel:x:10:root,george", 1)), 0o644, 0, 0)
		return h.onConsole()
	case "rsa-small":
		h := vm(deb)
		h.AddFile("/home/george/.ssh/authorized_keys", []byte(rsaKey(1024, 0x41, "george@laptop-1")+"\n"), 0o600, 1000, 1000)
		return h.sshFile(sshd.DropInDir+"/10-rsa.conf", "RequiredRSASize 2048\n", 0o644).onConsole()
	case "admin-no-sudo":
		h := usersOn(deb, false, george(deb)).rootLike("*")
		return h.sshFile(sshd.MainConfig, debianMain, 0o644).onConsole()
	case "allow-users-host":
		return vm(deb).sshFile(sshd.DropInDir+"/10-site.conf", "AllowUsers george@198.51.100.7\n", 0o644).withLog("publickey", "george", 1)
	case "no-accepted":
		return vm(deb) // sshd's log has no line for the session
	}
	t.Fatalf("no host %q", name)
	return nil
}

// passwordLogin is Debian 13 installed from the ISO: cloud-init's
// 50-cloud-init.conf allowing passwords, read before sshd_config's no.
func passwordLogin(p platform) *sshHost {
	h := usersOn(p, true, george(p), alice()).rootLike("*")
	h.env = map[string]string{"SUDO_USER": "george"}
	h.sshFile(sshd.MainConfig, debianMain, 0o644)
	return h.sshFile(sshd.DropInDir+"/50-cloud-init.conf", "PasswordAuthentication yes\n", 0o600)
}

// pveNode is a Proxmox VE 9 node as installed, root alone with the shadow
// field pw: sshd_config's PermitRootLogin yes, passwords on.
func pveNode(pw string) *sshHost {
	h := usersOn(pve9(), false).rootLike(pw)
	return h.sshFile(sshd.MainConfig, sshd.IncludeLine+"\n\nUsePAM yes\nPermitRootLogin yes\n", 0o644)
}

// alpineVM is the Alpine VM: alpine and george in wheel, doas nopass,
// sshd without PAM, 50-cloud-init.conf refusing passwords (0600),
// AllowTcpForwarding no, root "!" with a key; no ufw; run through doas.
func alpineVM() *sshHost {
	p := alpine323()
	h := usersOn(p, true, simUser{name: "alpine", uid: 1000, groups: []string{"wheel"}, pw: "*", shell: "/bin/ash"}, george(p)).rootLike("!")
	h.env = map[string]string{"DOAS_USER": "george"}
	h.sshFile(sshd.MainConfig, sshd.IncludeLine+"\n\nUsePAM yes\nAuthorizedKeysFile .ssh/authorized_keys\nAllowTcpForwarding no\n", 0o644)
	return h.sshFile(sshd.DropInDir+"/50-cloud-init.conf", "PasswordAuthentication no\n", 0o600)
}

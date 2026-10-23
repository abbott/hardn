package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/netip"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// The simulated host of the engine's scenarios (next/p3-gallery-engine):
// hostfake's recording host.System, whose commands a model of the host
// answers. Nothing is run, written or read on this machine: the fake's
// files are in memory, and a command is a call of fakeHost.run. The model
// is ported from internal/tools/p2drive's fake VM (fake_test.go), which a
// main package's test file cannot share, and kept in step with it by hand;
// its formats are the ones the units' own tests pin (sshd -T, ss -Htln,
// systemctl show -p Listen, ufw 0.36.2's status). Where the VM runs of
// docs/safety/vmtest.md §4.9 settled what a host answers, the model
// answers so, where p2drive's fake does not: sshd -T's permitrootlogin on
// Debian 13, sudo-rs on Ubuntu 26.04, ufw's delete while inactive. Each
// host is what detect.Detect establishes on it (TestScenarioHosts), and
// the engine detects it itself. Synthetic values only: alice,
// web-01.example.net at 192.0.2.20, the runner at 192.0.2.10, port 2230.

const (
	// galleryHardn is where the simulated host's hardn binary is, which
	// the revert timer runs.
	galleryHardn = "/usr/local/bin/hardn"
	// hostFQDN is the simulated host's name, and hostName its short
	// name, as hostname(1) prints it: the phrase a lockout-risk change is
	// confirmed by typing.
	hostFQDN = "web-01.example.net"
	hostName = "web-01"
	// applyConn is the session that applies a change: alice, from the
	// runner, on port 22 (SSH_CONNECTION's form).
	applyConn = "192.0.2.10 50022 192.0.2.20 22"
	// confirmConn is the new SSH connection to 2230 that confirms it.
	confirmConn = "192.0.2.10 50123 192.0.2.20 2230"
)

// galleryNow is the simulated host's clock: every scenario runs at this
// moment, so that its change id, its deadline and its countdown are the
// same on every run.
var galleryNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// stockMain is sshd_config as the simulated hosts ship it: the stock
// Include first (sshd.IncludeLine), so 00-hardn.conf can be written.
const stockMain = sshd.IncludeLine + "\n\nUsePAM yes\n"

// fakeHost is one simulated host: the fake, and the state the model keeps
// of what the host runs. sshd listens on 22 through ssh.service, or
// through ssh.socket when socket is set, or OpenRC's sshd; ufw is active
// and allows 22 unless set otherwise; sudo has its stock includedir.
type fakeHost struct {
	f          *hostfake.System
	platform            // what the release ships
	allowed    []uint16 // ufw's allow rules, both families
	inactive   bool     // ufw stopped; saveRules writes ENABLED=no for it
	bound      []string // what ss shows of sshd's (or ssh.socket's) sockets
	configured []string // ssh.socket's Listen, as the last daemon-reload set it
	// failReloads is how many reloads of ssh.service to refuse, as
	// systemctl does when sshd -t fails in ExecReload.
	failReloads int
	// resolved is systemd-resolved's state, nil where the release does not
	// run it (resolver.go).
	resolved *resolved
	// immutable are the files whose atomic writes fail, as chattr +i makes
	// the rename over them fail (engineSys): the operation is not
	// permitted. Only WriteFileAtomic is refused, which is how hardn.yml is
	// saved (config.Save); the file's other changes (removed, renamed,
	// chmodded, chowned, appended to) are not modelled.
	immutable []string
	// What the status checks and the header probe ask beyond the
	// scenarios' commands (statushost.go): nil on the scenarios' hosts,
	// where those commands stay unanswered.
	status *readings

	// mu guards timers and unexpected, which a test reads once play has
	// ended; the rest is the model's, changed only by run, which the
	// engine calls one command at a time.
	mu         sync.Mutex
	timers     map[string]time.Time // each systemd-run timer unit, and when it fires
	unexpected []string             // the commands the model does not answer
}

// platform is what a simulated host's release ships, as detect reads it.
type platform struct {
	// osRelease is /etc/os-release, the lines detect reads of
	// pkg/host/detect's fixture for the release (testdata/os-release).
	osRelease string
	openRC    bool // OpenRC booted the host, not systemd
	socket    bool // ssh.socket holds the listeners, from sshd-socket-generator
	noUFW     bool // ufw not installed
	// sudo is sudo --version's first line; "" for no sudo.
	sudo string
	doas bool // doas installed, setuid root
	// proxmox is pve-manager's version, as dpkg-query shows it, on a
	// Proxmox VE node (pveversion, pmxcfs mounted on /etc/pve); "" off
	// Proxmox VE.
	proxmox string
	// session is the comm of the sshd process of a connection:
	// sshd-session since OpenSSH 9.8, sshd before (revert's sshdNames).
	session string
	// rootLogin is what sshd -T prints for PermitRootLogin when the files
	// set none.
	rootLogin string
	// noPAM: sshd is built without PAM (Alpine's /usr/sbin/sshd), so sshd
	// -T prints no usepam line and ignores the files' UsePAM.
	noPAM bool
	// kbdInteractive is what sshd -T prints for KbdInteractiveAuthentication
	// when the files set none; "" is modelDefaults' no, which stands for the
	// line Debian's and Ubuntu's sshd_config set. Alpine's leaves it
	// commented: OpenSSH's default, yes. Both as the Alpine 3.23.6 VM's sshd
	// -T printed them; 3.24 builds sshd alike (docs/engine/check-ssh.md §8).
	kbdInteractive string
	// resolver is what manages /etc/resolv.conf as the release installs
	// it (resolver.go).
	resolver resolverKind
	// resolvedReload: the release's systemd-resolved reloads its files
	// (systemd 256 and later); before, it restarts (docs/safety/dns.md §3).
	resolvedReload bool
}

// The simulated platforms. Debian 13's and Ubuntu 26.04's sudo, OpenSSH
// and permitrootlogin are the VM runs' (docs/safety/vmtest.md §4.9);
// Ubuntu 22.04's, Alpine 3.24's and Proxmox VE 9's are the model's, not
// checked on a host, and so are Ubuntu 24.04's, Debian 12's and Alpine
// 3.23's (next/s2-engine-gallery: the sudo versions, and an sshd before
// OpenSSH 9.8 on the first two). Each resolver is the release's, as
// docs/safety/dns.md §3 has it, and whether its systemd-resolved reloads
// is its systemd's version there: 256 and later reload (Ubuntu 26.04's
// 259); Ubuntu 24.04's 255 and Debian 12's 252 restart, as Ubuntu
// 22.04's does, released before 256.
var (
	debian13Platform = platform{
		osRelease: "PRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\nVERSION_ID=\"13\"\nVERSION_CODENAME=trixie\nID=debian\n",
		sudo:      "Sudo version 1.9.16p2", session: "sshd-session", rootLogin: "without-password",
		resolver: resolverDHCPCD,
	}
	ubuntu2604Platform = platform{
		osRelease: "PRETTY_NAME=\"Ubuntu 26.04.1 LTS\"\nVERSION_ID=\"26.04\"\nVERSION_CODENAME=resolute\nID=ubuntu\nID_LIKE=debian\n",
		socket:    true, sudo: "sudo-rs 0.2.13", session: "sshd-session", rootLogin: "prohibit-password",
		resolver: resolverStub, resolvedReload: true,
	}
	ubuntu2404Platform = platform{
		osRelease: "PRETTY_NAME=\"Ubuntu 24.04.5 LTS\"\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\nID=ubuntu\nID_LIKE=debian\n",
		socket:    true, sudo: "Sudo version 1.9.15p5", session: "sshd", rootLogin: "without-password",
		resolver: resolverStub,
	}
	ubuntu2204Platform = platform{
		osRelease: "PRETTY_NAME=\"Ubuntu 22.04.5 LTS\"\nVERSION_ID=\"22.04\"\nVERSION_CODENAME=jammy\nID=ubuntu\nID_LIKE=debian\n",
		sudo:      "Sudo version 1.9.9", session: "sshd", rootLogin: "without-password",
		resolver: resolverStub,
	}
	// debian12Platform is Debian 12's cloud image: systemd-resolved, its
	// uplink file linked.
	debian12Platform = platform{
		osRelease: "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nVERSION_ID=\"12\"\nVERSION_CODENAME=bookworm\nID=debian\n",
		sudo:      "Sudo version 1.9.13p3", session: "sshd", rootLogin: "without-password",
		resolver: resolverUplink,
	}
	alpine324Platform = platform{
		osRelease: "NAME=\"Alpine Linux\"\nID=alpine\nVERSION_ID=3.24.2\nPRETTY_NAME=\"Alpine Linux v3.24\"\n",
		openRC:    true, noUFW: true, doas: true, session: "sshd-session", rootLogin: "prohibit-password",
		noPAM: true, kbdInteractive: "yes", resolver: resolverUDHCPC,
	}
	alpine323Platform = platform{
		osRelease: "NAME=\"Alpine Linux\"\nID=alpine\nVERSION_ID=3.23.6\nPRETTY_NAME=\"Alpine Linux v3.23\"\n",
		openRC:    true, noUFW: true, doas: true, session: "sshd-session", rootLogin: "prohibit-password",
		noPAM: true, kbdInteractive: "yes", resolver: resolverUDHCPC,
	}
	// alpine323CloudPlatform is Alpine 3.23's cloud image, as the
	// 2026-10-23 VM: dhcpcd installed, which ifupdown-ng runs, not udhcpc.
	alpine323CloudPlatform = func() platform { p := alpine323Platform; p.resolver = resolverAlpineDHCPCD; return p }()
	// pve9Platform is a Proxmox VE 9 node: Debian 13 underneath, with
	// pve-manager, no sudo and no ufw (brief 2.5: pve-firewall instead),
	// and OpenSSH 10.0p1's word for prohibit-password (check-ssh.md §3).
	pve9Platform = platform{
		osRelease: debian13Platform.osRelease, noUFW: true, proxmox: "9.0.10",
		session: "sshd-session", rootLogin: "without-password", resolver: resolverStatic,
	}
)

// The processes of a simulated host (/proc/<pid>/stat): init, sshd's
// listener, and the sshd process of alice's connection that applies the
// change, all running before hardn (applyPID), which starts at 500000
// clock ticks after boot.
const (
	listenerPID, applySSHDPID, applyPID = 700, 3900, 5000
	applySSHDStart, applyStart          = 390000, 500000
)

// applySession is the session that applies a change: alice's connection
// to port 22, whose sshd process is applySSHDPID.
var applySession = revert.Session{Remote: true, Conn: applyConn, SSHD: revert.ProcStart{PID: applySSHDPID, Start: applySSHDStart}, Source: "SSH_CONNECTION"}

// The new connection that confirms a change (fakeHost.connect): its sshd
// process, and the hardn confirm it runs, both started a minute after the
// apply, at 100 clock ticks a second.
const (
	confirmSSHDPID, confirmPID     = 5100, 5110
	confirmSSHDStart, confirmStart = applyStart + 6000, applyStart + 6100
)

// connect is alice's new connection to port 2230, which runs hardn
// confirm: its sshd process appears in /proc, and /proc/self is the new
// hardn. It returns the connection's session.
func (h *fakeHost) connect() revert.Session {
	h.process(confirmSSHDPID, h.session, listenerPID, confirmSSHDStart)
	h.self(confirmPID, confirmPID-1, confirmStart)
	return revert.Session{Remote: true, Conn: confirmConn, SSHD: revert.ProcStart{PID: confirmSSHDPID, Start: confirmSSHDStart}, Source: "SSH_CONNECTION"}
}

// newHost returns a simulated Debian 13 host (simulate).
func newHost() *fakeHost { return simulate(debian13Platform) }

// socketHost returns a simulated Ubuntu 26.04 host, ssh.socket holding the
// listeners.
func socketHost() *fakeHost { return simulate(ubuntu2604Platform) }

// openRCHost returns a simulated Alpine 3.24 host: OpenRC's sshd started
// and in the default runlevel, doas, no ufw.
func openRCHost() *fakeHost { return simulate(alpine324Platform) }

// ubuntu2204Host returns a simulated Ubuntu 22.04 host, sshd on
// ssh.service.
func ubuntu2204Host() *fakeHost { return simulate(ubuntu2204Platform) }

// hardnYML is where a simulated host keeps hardn.yml when a scenario gives
// it one: the system-wide path, the first config.FindConfigFile looks at.
const hardnYML = "/etc/hardn/hardn.yml"

// withHardnYML writes hardn.yml with settings, YAML, under a comment,
// root's 0600, as the DNS control's tests write it.
func (h *fakeHost) withHardnYML(settings string) *fakeHost {
	h.f.AddFile(hardnYML, []byte("# hardn's configuration ("+hostFQDN+")\n"+settings), 0o600, 0, 0)
	return h
}

// simulate returns a simulated host of platform p on hostfake: its
// os-release, sshd_config with the stock Include, sshd on 22 (by
// ssh.service, ssh.socket or OpenRC), ufw active allowing 22 where it is
// installed, sudo with /etc/sudoers.d, or doas, the directories revert
// and the boot unit use, the processes /proc shows, what manages
// /etc/resolv.conf (addResolver), root and alice's accounts, and on
// Proxmox VE pveversion and pmxcfs mounted on /etc/pve: no container.
func simulate(p platform) *fakeHost {
	h := &fakeHost{f: hostfake.New(host.Options{}), platform: p, allowed: []uint16{22}, timers: map[string]time.Time{}}
	f := h.f
	f.AddFile(detect.EtcOSRelease, []byte(p.osRelease), 0o644, 0, 0)
	f.AddFile(sshd.MainConfig, []byte(stockMain), 0o644, 0, 0)
	f.AddDir(sshd.DropInDir, 0o755, 0, 0)
	f.AddDir("/run/sshd", 0o755, 0, 0)
	f.AddDir("/var/lib", 0o755, 0, 0)
	f.AddDir("/var/backups", 0o755, 0, 0)
	f.AddFile(galleryHardn, []byte("ELF"), 0o755, 0, 0)
	initComm := "systemd"
	switch {
	case p.openRC:
		initComm = "init"
		f.AddFile(detect.OpenRCRun, []byte("ELF"), 0o755, 0, 0)
		f.AddFile(detect.OpenRCSSHD, []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
		f.AddDir(detect.OpenRCStarted, 0o755, 0, 0)
		f.AddSymlink(detect.OpenRCStarted+"/sshd", detect.OpenRCSSHD)
		f.AddDir(revert.OpenRCDefault, 0o755, 0, 0)
		f.AddSymlink(revert.OpenRCDefault+"/sshd", detect.OpenRCSSHD)
	default:
		f.AddDir(detect.SystemdRunDir, 0o755, 0, 0)
		f.AddDir(revert.SystemdUnitDir, 0o755, 0, 0)
	}
	if p.socket {
		f.AddFile(detect.SocketGenerator, []byte("ELF"), 0o755, 0, 0)
	}
	h.process(1, initComm, 0, 1)
	h.process(listenerPID, "sshd", 1, 1200)
	h.process(applySSHDPID, p.session, listenerPID, applySSHDStart)
	h.self(applyPID, applyPID-1, applyStart)
	f.AddFile("/proc/1/environ", []byte("HOME=/\x00TERM=linux\x00"), 0o400, 0, 0)
	f.AddSymlink(detect.SelfPIDNamespace, fmt.Sprintf("pid:[%d]", detect.InitPIDNamespaceInode))
	mounts := "proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0\n/dev/vda1 / ext4 rw,relatime 0 0\n"
	if p.proxmox != "" {
		f.AddFile(detect.PVEVersionBin, []byte("#!/usr/bin/perl\n"), 0o755, 0, 0)
		f.AddDir(detect.PVEDir, 0o755, 0, 33)
		mounts += "/dev/fuse /etc/pve fuse rw,nosuid,nodev,relatime,user_id=0,group_id=0,default_permissions,allow_other 0 0\n"
	}
	f.AddFile(detect.SelfMounts, []byte(mounts), 0o444, 0, 0)
	f.AddFile("/proc/sys/kernel/random/boot_id", []byte("8e2a3c4d-1b2c-4d3e-8f40-0123456789ab\n"), 0o444, 0, 0)
	if !p.noUFW {
		f.AddDir(ufw.KernelIPv6, 0o555, 0, 0)
		f.AddFile(ufw.DefaultsFile, []byte("IPV6=yes\nDEFAULT_INPUT_POLICY=\"DROP\"\nDEFAULT_OUTPUT_POLICY=\"ACCEPT\"\n"), 0o644, 0, 0)
		h.saveRules()
	}
	if p.sudo != "" {
		f.AddFile("/usr/bin/sudo", []byte("ELF"), 0o4755, 0, 0)
		f.AddFile("/etc/sudoers", []byte("Defaults env_reset\nroot ALL=(ALL:ALL) ALL\n%sudo ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n"), 0o440, 0, 0)
		f.AddDir("/etc/sudoers.d", 0o755, 0, 0)
	}
	if p.doas {
		f.AddFile("/usr/bin/doas", []byte("ELF"), 0o4755, 0, 0)
	}
	h.addResolver()
	f.AddUser(host.User{Name: "root", UID: 0, GID: 0, HomeDir: "/root"})
	f.AddUser(host.User{Name: "alice", UID: 1000, GID: 1000, HomeDir: "/home/alice"})
	f.AddGroup(host.Group{Name: "root", GID: 0})
	f.AddGroup(host.Group{Name: "alice", GID: 1000})
	f.AddFile("/etc/group", []byte("root:x:0:\nalice:x:1000:\n"), 0o644, 0, 0)
	h.boot()
	f.Handler = h.run
	return h
}

// process adds a running process with no controlling terminal to /proc:
// its stat line, with the fields revert reads (proc_pid_stat(5): pid,
// comm, state, ppid, tty_nr, starttime).
func (h *fakeHost) process(pid int, comm string, ppid int, start uint64) {
	h.f.AddFile(fmt.Sprintf("/proc/%d/stat", pid), statLine(pid, comm, ppid, 0, start), 0o444, 0, 0)
}

// self makes hardn, process pid started at start under sudo (ppid), on
// the session's pseudo-terminal (pts/3), the process that reads
// /proc/self.
func (h *fakeHost) self(pid, ppid int, start uint64) {
	h.f.AddFile("/proc/self/stat", statLine(pid, "hardn", ppid, 136<<8|3, start), 0o444, 0, 0)
}

func statLine(pid int, comm string, ppid, tty int, start uint64) []byte {
	return fmt.Appendf(nil, "%d (%s) S %d %d %d %d -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 %d 0 0\n", pid, comm, ppid, pid, pid, tty, start)
}

// boot makes the files on disk the running configuration, as a reboot
// does: sshd's (or the socket's) listeners from the model.
func (h *fakeHost) boot() {
	if h.socket {
		h.bound, h.configured = h.socketListens(), h.socketListens()
		return
	}
	h.bound, h.configured = h.listening(), h.listening()
}

// socketListens is ssh.socket's Listen as a daemon-reload or a boot sets
// it: the generator's, the listeners the sshd files configure, unless
// v0.3.x's listen.conf ("ListenStream=", then "ListenStream=<port>") is in
// ssh.socket.d, whose empty assignment resets them to its port alone, on
// a dual-stack [::] (docs/safety/reload.md §3).
func (h *fakeHost) socketListens() []string {
	if e, ok := h.f.Entry(migrate.ListenConf); ok {
		for _, l := range strings.Split(string(e.Data), "\n") {
			if p, ok := strings.CutPrefix(l, "ListenStream="); ok && p != "" {
				return []string{"[::]:" + p}
			}
		}
	}
	return h.listening()
}

// files is the configuration sshd -f conf reads: conf (sshd_config, or
// sshd.Validate's copy with its Include mapped back) and the drop-ins
// beside it, by their real paths.
func (h *fakeHost) files(conf string) ([]byte, []sshd.File) {
	work := path.Dir(conf)
	e, _ := h.f.Entry(conf)
	main := strings.ReplaceAll(string(e.Data), "Include "+work+"/sshd_config.d/*.conf", sshd.IncludeLine)
	entries, _ := h.f.ReadDir(context.Background(), work+"/sshd_config.d")
	var drops []sshd.File
	for _, de := range entries {
		if d, ok := h.f.Entry(work + "/sshd_config.d/" + de.Name()); ok && d.Mode.IsRegular() {
			drops = append(drops, sshd.File{Path: sshd.DropInDir + "/" + de.Name(), Data: d.Data})
		}
	}
	return []byte(main), drops
}

// modelDefaults are the model's values for the keywords sshd -T always
// prints, shaped like OpenSSH's (sshd_config(5)), as migrate's tests and
// p2drive's fake model them; not read from a binary. permitrootlogin's is
// the platform's (platform.rootLogin), not this one.
var modelDefaults = []struct{ k, v string }{
	{"permitrootlogin", "prohibit-password"}, {"pubkeyauthentication", "yes"}, {"passwordauthentication", "yes"},
	{"kbdinteractiveauthentication", "no"}, {"permitemptypasswords", "no"}, {"usepam", "no"},
	{"hostbasedauthentication", "no"}, {"gssapiauthentication", "no"}, {"kerberosauthentication", "no"},
	{"authorizedkeysfile", ".ssh/authorized_keys .ssh/authorized_keys2"}, {"authenticationmethods", "any"},
	{"ciphers", "chacha20-poly1305@openssh.com,aes256-gcm@openssh.com"}, {"macs", "hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com"},
	{"kexalgorithms", "mlkem768x25519-sha256,curve25519-sha256"}, {"hostkeyalgorithms", "ssh-ed25519,ecdsa-sha2-nistp256,rsa-sha2-512"},
	{"pubkeyacceptedalgorithms", "ssh-ed25519,ecdsa-sha2-nistp256,rsa-sha2-512,rsa-sha2-256"}, {"casignaturealgorithms", "ssh-ed25519,ecdsa-sha2-nistp256,rsa-sha2-512"},
	{"authorizedkeyscommand", "none"}, {"authorizedkeyscommanduser", "none"}, {"authorizedprincipalsfile", "none"},
	{"authorizedprincipalscommand", "none"}, {"authorizedprincipalscommanduser", "none"}, {"trustedusercakeys", "none"},
	{"revokedkeys", "none"}, {"pubkeyauthoptions", "none"}, {"maxauthtries", "6"}, {"forcecommand", "none"}, {"chrootdirectory", "none"},
	{"allowtcpforwarding", "yes"}, {"gatewayports", "no"}, {"x11forwarding", "no"}, {"addressfamily", "any"},
}

// sshdT is sshd -T's output by a model of sshd's reading (sshd.ReadOrder):
// the first value of each keyword outside Match blocks, every value of an
// accumulating one, modelDefaults and the platform's PermitRootLogin and
// KbdInteractiveAuthentication for the rest, no usepam line from a build
// without PAM; and the listeners: a ListenAddress with a port as it is,
// one without on every Port, with none every Port on [::] and 0.0.0.0
// (sshd_config(5) ListenAddress).
func (h *fakeHost) sshdT(main []byte, drops []sshd.File) (out string, listeners []string) {
	first, acc := map[string]string{}, map[string][]string{}
	for _, l := range sshd.ReadOrder(main, drops) {
		switch {
		case l.Match != nil:
		case sshd.Accumulates(l.Keyword):
			acc[l.Keyword] = append(acc[l.Keyword], l.Args...)
		default:
			if _, ok := first[l.Keyword]; !ok {
				first[l.Keyword] = strings.Join(l.Args, " ")
			}
		}
	}
	var b strings.Builder
	ports := acc["port"]
	if len(ports) == 0 {
		ports = []string{"22"}
	}
	ports = slices.Compact(ports)
	for _, p := range ports {
		b.WriteString("port " + p + "\n")
	}
	on := func(a netip.Addr, p string) {
		n, _ := strconv.Atoi(p)
		listeners = append(listeners, netip.AddrPortFrom(a, uint16(n)).String())
	}
	if las := acc["listenaddress"]; len(las) > 0 {
		for _, la := range las {
			if ap, err := netip.ParseAddrPort(la); err == nil {
				listeners = append(listeners, ap.String())
				continue
			}
			for _, p := range ports {
				on(netip.MustParseAddr(strings.Trim(la, "[]")), p)
			}
		}
	} else {
		for _, p := range ports {
			on(netip.IPv6Unspecified(), p)
			on(netip.IPv4Unspecified(), p)
		}
	}
	for _, l := range listeners {
		b.WriteString("listenaddress " + l + "\n")
	}
	for _, d := range modelDefaults {
		v, ok := first[d.k]
		switch {
		case d.k == "usepam" && h.noPAM:
			continue
		case ok:
		case d.k == "permitrootlogin":
			v = h.rootLogin
		case d.k == "kbdinteractiveauthentication" && h.kbdInteractive != "":
			v = h.kbdInteractive
		default:
			v = d.v
		}
		if d.k != "authorizedkeysfile" {
			v = strings.ToLower(v)
		}
		b.WriteString(d.k + " " + v + "\n")
	}
	for _, k := range []string{"allowusers", "allowgroups", "denyusers", "denygroups"} {
		for _, v := range acc[k] {
			b.WriteString(k + " " + v + "\n")
		}
	}
	return b.String(), listeners
}

// listening is the listeners the installed files configure, by the model.
func (h *fakeHost) listening() []string {
	_, ls := h.sshdT(h.files(sshd.MainConfig))
	return ls
}

// ssOutput is ss -Htln's output for the sockets given ("0.0.0.0:22").
func ssOutput(socks []string) string {
	var b strings.Builder
	for _, s := range socks {
		peer := "0.0.0.0:*"
		if strings.HasPrefix(s, "[") {
			peer = "[::]:*"
		}
		fmt.Fprintf(&b, "LISTEN 0      4096   %20s   %s\n", s, peer)
	}
	return b.String()
}

// ufwStatus is ufw 0.36.2's verbose status for the allow rules.
func (h *fakeHost) ufwStatus() string {
	if h.inactive {
		return "Status: inactive\n"
	}
	var b strings.Builder
	b.WriteString("Status: active\nLogging: on (low)\nDefault: deny (incoming), allow (outgoing), disabled (routed)\nNew profiles: skip\n")
	fmt.Fprintf(&b, "\n%-26s %-12s%s\n%-26s %-12s%s\n", "To", "Action", "From", "--", "------", "----")
	for _, sfx := range []string{"", " (v6)"} {
		for _, p := range h.allowed {
			fmt.Fprintf(&b, "%-26s %-12s%-26s\n", fmt.Sprintf("%d/tcp%s", p, sfx), "ALLOW IN", "Anywhere"+sfx)
		}
	}
	return b.String()
}

// saveRules writes ufw's configuration and rule files as ufw keeps them.
func (h *fakeHost) saveRules() {
	enabled := "yes"
	if h.inactive {
		enabled = "no"
	}
	h.f.AddFile(ufw.ConfFile, []byte("ENABLED="+enabled+"\nLOGLEVEL=low\n"), 0o644, 0, 0)
	for _, r := range []struct{ path, any string }{{ufw.UserRules, "0.0.0.0/0"}, {ufw.User6Rules, "::/0"}} {
		var b strings.Builder
		b.WriteString("*filter\n:ufw-user-input - [0:0]\n### RULES ###\n")
		for _, p := range h.allowed {
			fmt.Fprintf(&b, "\n### tuple ### allow tcp %d %s any %s in\n-A ufw-user-input -p tcp --dport %d -j ACCEPT\n", p, r.any, r.any, p)
		}
		b.WriteString("\n### END RULES ###\nCOMMIT\n")
		h.f.AddFile(r.path, []byte(b.String()), 0o640, 0, 0)
	}
}

// run answers the commands the engine and the primitives run, changing
// the model as the host would change.
func (h *fakeHost) run(argv []string, _ []byte) hostfake.Response {
	cmd := strings.Join(argv, " ")
	if !h.installed(path.Base(argv[0])) {
		return hostfake.Response{Err: host.ErrNotFound, ExitCode: -1}
	}
	switch path.Base(argv[0]) {
	case "sshd":
		return h.sshd(argv)
	case "rc-service":
		if cmd == "rc-service sshd reload" {
			h.bound = slices.Clone(h.listening())
			return hostfake.Response{}
		}
	case "sh": // OpenRC's timer, the sleeper revert starts detached; sh -n, the boot script's check
		return hostfake.Response{}
	case "systemd-analyze": // the boot unit's check, verify on its copy
		return hostfake.Response{}
	case "rc-update":
		if cmd == "rc-update add "+revert.BootUnitName+" default" {
			h.f.AddSymlink(revert.OpenRCDefault+"/"+revert.BootUnitName, revert.OpenRCInitDir+"/"+revert.BootUnitName)
			return hostfake.Response{}
		}
	case "ss":
		return hostfake.Response{Stdout: ssOutput(h.bound)}
	case "systemd-run":
		h.arm(argv)
		return hostfake.Response{}
	case "visudo":
		return hostfake.Response{Stdout: "/etc/sudoers: parsed OK\n"}
	case "sudo": // detect's question, which sudo it is
		if cmd == "sudo --version" {
			return hostfake.Response{Stdout: h.sudo + "\n"}
		}
	case "dpkg-query": // detect's Proxmox VE check; on a status host, any package
		if pkg := argv[len(argv)-1]; pkg == "pve-manager" || h.status != nil {
			return h.dpkg(pkg)
		}
	case "ufw":
		if r, ok := h.ufw(argv); ok {
			return r
		}
	case "resolvectl":
		if r, ok := h.resolvedCommand(argv); ok {
			return r
		}
	case "systemctl":
		if r, ok := h.resolvedCommand(argv); ok {
			return r
		}
		if r, ok := h.systemctl(argv); ok {
			return r
		}
	}
	if r, ok := h.status.answer(argv); ok {
		return r
	}
	h.mu.Lock()
	h.unexpected = append(h.unexpected, cmd)
	h.mu.Unlock()
	return hostfake.Response{Err: fmt.Errorf("the gallery's host does not model %q: %w", cmd, hostfake.ErrUnscripted), ExitCode: -1}
}

// installed reports whether the platform ships the command name: an
// init system's own tools on that init system alone, ufw unless it is not
// installed, sudo's on a host with sudo, doas on a host with doas, apt's
// and dpkg-query on Debian, Ubuntu and Proxmox VE, apk on Alpine, and
// resolvectl where systemd-resolved is. A command that is not is not
// found, as on the host.
func (h *fakeHost) installed(name string) bool {
	switch name {
	case "systemctl", "systemd-run", "systemd-analyze":
		return !h.openRC
	case "rc-service", "rc-update", "apk":
		return h.openRC
	case "ufw":
		return !h.noUFW
	case "sudo", "visudo":
		return h.sudo != ""
	case "doas":
		return h.doas
	case "dpkg-query", "apt-config":
		return !h.openRC
	case "resolvectl":
		return h.resolved != nil
	}
	return true
}

// dpkg is dpkg-query --show's answer for pkg: pve-manager at the
// platform's version on Proxmox VE, a package a status host's readings
// install at its version, and dpkg-query(1)'s "no packages found", exit 1,
// for any other.
func (h *fakeHost) dpkg(pkg string) hostfake.Response {
	version := h.proxmox
	if pkg != "pve-manager" {
		version = h.status.installed(pkg)
	}
	if version == "" {
		return hostfake.Response{Stderr: "dpkg-query: no packages found matching " + pkg + "\n", ExitCode: 1}
	}
	return hostfake.Response{Stdout: "install ok installed\t" + version + "\n"}
}

// unanswered returns the commands the model did not answer, which a
// scenario's test fails on.
func (h *fakeHost) unanswered() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.unexpected)
}

// engineSys is the simulated host as the engine reaches it: the fake, but
// an atomic write of one of the files the model holds immutable fails, as
// chattr +i makes the rename over it fail (fakeHost.immutable).
type engineSys struct {
	*hostfake.System
	h *fakeHost
}

// system returns the simulated host as the engine reaches it.
func (h *fakeHost) system() engineSys { return engineSys{h.f, h} }

// WriteFileAtomic is the fake's, but for an immutable file.
func (s engineSys) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	if slices.Contains(s.h.immutable, name) {
		return &fs.PathError{Op: "rename", Path: name, Err: fs.ErrPermission}
	}
	return s.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

// arm records the timer systemd-run starts (--on-active=<N>s
// --unit=<unit>), firing a second after the window, as the Ubuntu 26.04
// run found the timer firing 1 to 2 s after the deadline hardn prints
// (docs/safety/vmtest.md §4.9).
func (h *fakeHost) arm(argv []string) {
	var unit string
	var after time.Duration
	for _, a := range argv[1:] {
		if v, ok := strings.CutPrefix(a, "--unit="); ok {
			unit = v
		}
		if v, ok := strings.CutPrefix(a, "--on-active="); ok {
			after, _ = time.ParseDuration(v)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.timers[unit+".timer"] = galleryNow.Add(after + time.Second)
}

// sshd answers sshd -t and sshd -T over the files -f names.
func (h *fakeHost) sshd(argv []string) hostfake.Response {
	main, drops := h.files(argv[slices.Index(argv, "-f")+1])
	if slices.Contains(argv, "-t") {
		return hostfake.Response{}
	}
	out, _ := h.sshdT(main, drops)
	return hostfake.Response{Stdout: out}
}

func (h *fakeHost) systemctl(argv []string) (hostfake.Response, bool) {
	state := func(on bool, yes, no string, code int) hostfake.Response {
		if on {
			return hostfake.Response{Stdout: yes + "\n"}
		}
		return hostfake.Response{Stdout: no + "\n", ExitCode: code}
	}
	switch cmd := strings.Join(argv, " "); {
	case cmd == "systemctl is-active ssh.service": // running on every systemd host, by itself or socket-activated
		return hostfake.Response{Stdout: "active\n"}, true
	case cmd == "systemctl is-enabled ssh.service":
		return state(!h.socket, "enabled", "disabled", 1), true
	case cmd == "systemctl is-active ssh.socket":
		return state(h.socket, "active", "inactive", 3), true
	case cmd == "systemctl is-enabled ssh.socket":
		return state(h.socket, "enabled", "disabled", 1), true
	case cmd == "systemctl reload ssh.service" && h.failReloads > 0:
		h.failReloads--
		return hostfake.Response{Stderr: "Job for ssh.service failed because the control process exited with error code.\n", ExitCode: 1}, true
	case cmd == "systemctl reload ssh.service":
		if !h.socket {
			h.bound = h.listening()
		}
		return hostfake.Response{}, true
	case cmd == "systemctl daemon-reload":
		h.configured = h.socketListens()
		return hostfake.Response{}, true
	case cmd == "systemctl show -p Listen ssh.socket":
		var b strings.Builder
		for _, l := range h.configured {
			fmt.Fprintf(&b, "Listen=%s (Stream)\n", l)
		}
		return hostfake.Response{Stdout: b.String()}, true
	case cmd == "systemctl restart ssh.socket":
		h.bound = slices.Clone(h.configured)
		return hostfake.Response{}, true
	case len(argv) == 3 && argv[1] == "stop" && strings.HasPrefix(argv[2], revert.UnitPrefix):
		return hostfake.Response{}, true
	case cmd == "systemctl --no-reload enable "+revert.BootUnitName+".service":
		h.f.AddSymlink(revert.SystemdWants+"/"+revert.BootUnitName+".service", revert.SystemdUnitDir+"/"+revert.BootUnitName+".service")
		return hostfake.Response{}, true
	case len(argv) == 6 && argv[1] == "list-timers":
		return h.listTimers(argv[5]), true
	}
	return hostfake.Response{}, false
}

// listTimers is systemctl list-timers --output=json for unit: its next
// trigger time in microseconds since the epoch, as systemd v257 prints it
// (pkg/engine's timerTrigger reads it).
func (h *fakeHost) listTimers(unit string) hostfake.Response {
	h.mu.Lock()
	at, ok := h.timers[unit]
	h.mu.Unlock()
	if !ok {
		return hostfake.Response{Stdout: "[]\n"}
	}
	return hostfake.Response{Stdout: fmt.Sprintf("[{\"next\":%d,\"left\":0,\"last\":null,\"passed\":null,\"unit\":%q,\"activates\":%q}]\n",
		at.UnixMicro(), unit, strings.TrimSuffix(unit, ".timer")+".service")}
}

// ufw answers ufw 0.36.2's commands: its status, enable, and a port's
// allow rule added or deleted, in both families, in the words the Debian
// 13 run found (docs/safety/vmtest.md §4.9, the ufw pass): "Rules updated"
// for either while ufw is inactive.
func (h *fakeHost) ufw(argv []string) (hostfake.Response, bool) {
	cmd := strings.Join(argv, " ")
	switch cmd {
	case "ufw status verbose":
		return hostfake.Response{Stdout: h.ufwStatus()}, true
	case "ufw --force enable":
		h.inactive = false
		h.saveRules()
		return hostfake.Response{Stdout: "Firewall is active and enabled on system startup\n"}, true
	}
	word := "Rule added"
	if h.inactive {
		word = "Rules updated"
	}
	n, err := strconv.Atoi(strings.TrimSuffix(argv[len(argv)-1], "/tcp"))
	switch {
	case err != nil:
	case len(argv) == 3 && argv[1] == "allow":
		h.allowed = append(h.allowed, uint16(n))
		slices.Sort(h.allowed)
		h.allowed = slices.Compact(h.allowed)
		h.saveRules()
		return hostfake.Response{Stdout: word + "\n" + word + " (v6)\n"}, true
	case len(argv) == 4 && argv[1] == "delete" && argv[2] == "allow":
		h.allowed = slices.DeleteFunc(h.allowed, func(p uint16) bool { return p == uint16(n) })
		h.saveRules()
		if h.inactive {
			return hostfake.Response{Stdout: word + "\n" + word + " (v6)\n"}, true
		}
		return hostfake.Response{Stdout: "Rule deleted\nRule deleted (v6)\n"}, true
	}
	return hostfake.Response{}, false
}

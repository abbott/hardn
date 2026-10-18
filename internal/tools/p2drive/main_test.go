package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// Nothing here runs a real command or touches the machine: every host is
// hostfake's, and its commands are answered by vm.run below, written from
// the formats the units' own tests pin (sshd -T, ss, systemctl, ufw 0.36.2).
// Synthetic values only: alice, web-01.example.net, 192.0.2.0/24, 2230.

const (
	testHardn = "/usr/local/bin/hardn"
	applyConn = "192.0.2.10 50022 192.0.2.20 22"
)

var testNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// debian13 is detect's view of a stock Debian 13 host: ssh.service, sudo.
var debian13 = detect.Facts{
	OS:       detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie"},
	Platform: detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
	Init:     detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir},
	SSH: detect.SSH{Mode: detect.SSHService, Generator: detect.No,
		Socket:  detect.UnitState{Enabled: "disabled", Active: "inactive"},
		Service: detect.UnitState{Enabled: "enabled", Active: "active"}},
	Proxmox:   detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No},
	Container: detect.Container{Detected: detect.No},
	Elevation: detect.Elevation{Sudo: detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}, SudoFlavor: detect.SudoOriginal,
		Doas: detect.Tool{Present: detect.No}},
}

// vm is a fake disposable VM: sshd on 22 through ssh.service, an active ufw
// allowing 22, sudo with its stock includedir.
type vm struct {
	t           *testing.T
	f           *hostfake.System
	allowed     []uint16 // ufw's allow rules, both families
	inactive    bool     // ufw stopped, ENABLED=no
	bound       []uint16 // what sshd listens on since its last reload
	failReloads int      // forward reloads to refuse, as sshd -t would
}

func newVM(t *testing.T) *vm {
	v := &vm{t: t, f: hostfake.New(host.Options{}), allowed: []uint16{22}, bound: []uint16{22}}
	f := v.f
	f.AddFile(sshd.MainConfig, []byte(sshd.IncludeLine+"\n\nUsePAM yes\n"), 0o644, 0, 0)
	f.AddDir(sshd.DropInDir, 0o755, 0, 0)
	f.AddDir("/run/sshd", 0o755, 0, 0)
	f.AddDir(detect.SystemdRunDir, 0o755, 0, 0)
	f.AddDir("/var/lib", 0o755, 0, 0)
	f.AddDir("/var/backups", 0o755, 0, 0)
	f.AddFile(testHardn, []byte("ELF"), 0o755, 0, 0)
	f.AddFile("/proc/self/stat", []byte("5000 (p2drive) S 1 5000 5000 34819 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 500000 0 0\n"), 0o444, 0, 0)
	f.AddFile("/proc/sys/kernel/random/boot_id", []byte("8e2a3c4d-1b2c-4d3e-8f40-0123456789ab\n"), 0o444, 0, 0)
	f.AddDir(ufw.KernelIPv6, 0o555, 0, 0)
	f.AddFile(ufw.DefaultsFile, []byte("IPV6=yes\nDEFAULT_INPUT_POLICY=\"DROP\"\nDEFAULT_OUTPUT_POLICY=\"ACCEPT\"\n"), 0o644, 0, 0)
	f.AddFile("/etc/sudoers", []byte("Defaults env_reset\nroot ALL=(ALL:ALL) ALL\n%sudo ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n"), 0o440, 0, 0)
	f.AddDir("/etc/sudoers.d", 0o755, 0, 0)
	v.saveRules()
	f.Handler = v.run
	return v
}

// env is production's with the fake and the facts given.
func (v *vm) env(facts detect.Facts, out, errOut io.Writer) env {
	return env{
		goos: "linux", euid: func() int { return 0 }, now: func() time.Time { return testNow },
		getenv: func(k string) string {
			if k == gateEnv {
				return gateValue
			}
			return ""
		},
		stdout: out, stderr: errOut,
		system: func(io.Writer) host.System { return v.f },
		detect: func(context.Context, host.System) (detect.Facts, error) { return facts, nil },
		session: func(context.Context, host.System) (revert.Session, error) {
			return revert.Session{Remote: true, Conn: applyConn, Source: "SSH_CONNECTION"}, nil
		},
	}
}

// dropInPorts are the Port lines of the 00-hardn.conf beside sshd_config
// file conf, or 22 when there are none.
func (v *vm) dropInPorts(conf string) []uint16 {
	e, ok := v.f.Entry(path.Join(path.Dir(conf), "sshd_config.d", "00-hardn.conf"))
	var ps []uint16
	if ok {
		for _, l := range strings.Split(string(e.Data), "\n") {
			if p, found := strings.CutPrefix(l, "Port "); found {
				n, _ := strconv.Atoi(p)
				ps = append(ps, uint16(n))
			}
		}
	}
	if len(ps) == 0 {
		ps = []uint16{22}
	}
	return ps
}

func sshdT(ports []uint16) string {
	var b strings.Builder
	for _, p := range ports {
		fmt.Fprintf(&b, "port %d\n", p)
	}
	b.WriteString("addressfamily any\n")
	for _, p := range ports {
		fmt.Fprintf(&b, "listenaddress [::]:%d\nlistenaddress 0.0.0.0:%d\n", p, p)
	}
	b.WriteString("usepam yes\npermitrootlogin without-password\npubkeyauthentication yes\npasswordauthentication yes\nkbdinteractiveauthentication no\nusedns no\n")
	return b.String()
}

func ss(ports []uint16) string {
	var b strings.Builder
	for _, p := range ports {
		fmt.Fprintf(&b, "LISTEN 0      4096         0.0.0.0:%-5d     0.0.0.0:*\nLISTEN 0      4096            [::]:%-5d        [::]:*\n", p, p)
	}
	return b.String()
}

// ufwStatus is ufw 0.36.2's verbose status for the allow rules.
func (v *vm) ufwStatus() string {
	if v.inactive {
		return "Status: inactive\n"
	}
	var b strings.Builder
	b.WriteString("Status: active\nLogging: on (low)\nDefault: deny (incoming), allow (outgoing), disabled (routed)\nNew profiles: skip\n")
	fmt.Fprintf(&b, "\n%-26s %-12s%s\n%-26s %-12s%s\n", "To", "Action", "From", "--", "------", "----")
	for _, sfx := range []string{"", " (v6)"} {
		for _, p := range v.allowed {
			fmt.Fprintf(&b, "%-26s %-12s%-26s\n", fmt.Sprintf("%d/tcp%s", p, sfx), "ALLOW IN", "Anywhere"+sfx)
		}
	}
	return b.String()
}

func (v *vm) saveRules() {
	enabled := "yes"
	if v.inactive {
		enabled = "no"
	}
	v.f.AddFile(ufw.ConfFile, []byte("ENABLED="+enabled+"\nLOGLEVEL=low\n"), 0o644, 0, 0)
	for _, f := range []struct{ path, any string }{{ufw.UserRules, "0.0.0.0/0"}, {ufw.User6Rules, "::/0"}} {
		var b strings.Builder
		b.WriteString("*filter\n:ufw-user-input - [0:0]\n### RULES ###\n")
		for _, p := range v.allowed {
			fmt.Fprintf(&b, "\n### tuple ### allow tcp %d %s any %s in\n-A ufw-user-input -p tcp --dport %d -j ACCEPT\n", p, f.any, f.any, p)
		}
		b.WriteString("\n### END RULES ###\nCOMMIT\n")
		v.f.AddFile(f.path, []byte(b.String()), 0o640, 0, 0)
	}
}

// run answers the commands the driver and the primitives run.
func (v *vm) run(argv []string, _ []byte) hostfake.Response {
	cmd := strings.Join(argv, " ")
	switch argv[0] {
	case "sshd":
		i := slices.Index(argv, "-f")
		conf := argv[i+1]
		if slices.Contains(argv, "-t") {
			e, _ := v.f.Entry(path.Join(path.Dir(conf), "sshd_config.d", "00-hardn.conf"))
			if bytes.Contains(e.Data, []byte("maybe")) {
				return hostfake.Response{Stderr: path.Join(path.Dir(conf), "sshd_config.d", "00-hardn.conf") + " line 3: unsupported option \"maybe\".\r\n", ExitCode: 255}
			}
			return hostfake.Response{}
		}
		return hostfake.Response{Stdout: sshdT(v.dropInPorts(conf))}
	case "ss":
		return hostfake.Response{Stdout: ss(v.bound)}
	case "systemd-run":
		return hostfake.Response{}
	case "visudo":
		return hostfake.Response{Stdout: "/etc/sudoers: parsed OK\n"}
	case "ufw":
		return v.ufw(argv)
	case "systemctl":
		switch {
		case cmd == "systemctl is-active ssh.service":
			return hostfake.Response{Stdout: "active\n"}
		case cmd == "systemctl is-enabled ssh.service":
			return hostfake.Response{Stdout: "enabled\n"}
		case cmd == "systemctl is-active ssh.socket":
			return hostfake.Response{Stdout: "inactive\n", ExitCode: 3}
		case cmd == "systemctl is-enabled ssh.socket":
			return hostfake.Response{Stdout: "disabled\n", ExitCode: 1}
		case cmd == "systemctl reload ssh.service" && v.failReloads > 0:
			v.failReloads--
			return hostfake.Response{Stderr: "Job for ssh.service failed.\n", ExitCode: 1}
		case cmd == "systemctl reload ssh.service":
			v.bound = v.dropInPorts(sshd.MainConfig)
			return hostfake.Response{}
		case len(argv) == 3 && argv[1] == "stop" && strings.HasPrefix(argv[2], revert.UnitPrefix):
			return hostfake.Response{}
		}
	}
	v.t.Errorf("unexpected command %q", cmd)
	return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
}

func (v *vm) ufw(argv []string) hostfake.Response {
	cmd := strings.Join(argv, " ")
	switch cmd {
	case "ufw status verbose":
		return hostfake.Response{Stdout: v.ufwStatus()}
	case "ufw --force enable", "ufw disable":
		v.inactive = cmd == "ufw disable"
		v.saveRules()
		return hostfake.Response{Stdout: "Firewall is active and enabled on system startup\n"}
	}
	word := "Rule added"
	if v.inactive {
		word = "Rules updated"
	}
	spec := argv[len(argv)-1]
	n, err := strconv.Atoi(strings.TrimSuffix(spec, "/tcp"))
	switch {
	case err != nil:
	case len(argv) == 3 && argv[1] == "allow":
		v.allowed = append(v.allowed, uint16(n))
		v.saveRules()
		return hostfake.Response{Stdout: word + "\n" + word + " (v6)\n"}
	case len(argv) == 4 && argv[1] == "delete" && argv[2] == "allow":
		v.allowed = slices.DeleteFunc(v.allowed, func(p uint16) bool { return p == uint16(n) })
		v.saveRules()
		return hostfake.Response{Stdout: "Rule deleted\nRule deleted (v6)\n"}
	}
	v.t.Errorf("unexpected command %q", cmd)
	return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
}

// index is the position in the fake's record of the first call match
// accepts, or -1.
func (v *vm) index(match func(hostfake.Call) bool) int {
	return slices.IndexFunc(v.f.Calls(), match)
}

func runIs(cmd string) func(hostfake.Call) bool {
	return func(c hostfake.Call) bool { return c.Op == "run" && strings.Join(c.Argv, " ") == cmd }
}

func writeOf(p string) func(hostfake.Call) bool {
	return func(c hostfake.Call) bool { return c.Op == "write-atomic" && c.Path == p && c.Err == nil }
}

// journal decodes the change journal file name, or fails.
func (v *vm) journal(name string) revert.Journal {
	v.t.Helper()
	e, ok := v.f.Entry(path.Join(revert.DefaultDir, name))
	if !ok {
		v.t.Fatalf("%s: missing", name)
	}
	var j revert.Journal
	if err := json.Unmarshal(e.Data, &j); err != nil {
		v.t.Fatalf("%s: %v", name, err)
	}
	return j
}

func TestGates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		goos   string
		args   []string
		gate   string
		euid   int
		code   int
		built  bool
		stderr string
	}{
		{"macOS, whatever else", "darwin", []string{"--" + gateFlag, "facts"}, gateValue, 0, 2, false, "macOS"},
		{"macOS, no arguments", "darwin", nil, gateValue, 0, 2, false, "macOS"},
		{"not Linux", "freebsd", []string{"--" + gateFlag, "facts"}, gateValue, 0, 2, false, "Linux VM only"},
		{"no flag", "linux", []string{"facts"}, gateValue, 0, 2, false, "--" + gateFlag},
		{"no variable", "linux", []string{"--" + gateFlag, "facts"}, "", 0, 2, false, gateEnv},
		{"another value", "linux", []string{"--" + gateFlag, "facts"}, "1", 0, 2, false, gateEnv},
		{"not root", "linux", []string{"--" + gateFlag, "facts"}, gateValue, 1000, 2, false, "root"},
		{"unknown command", "linux", []string{"--" + gateFlag, "reboot"}, gateValue, 0, 2, false, "unknown command"},
		{"no command", "linux", []string{"--" + gateFlag}, gateValue, 0, 2, false, "usage"},
		{"stray argument", "linux", []string{"--" + gateFlag, "facts", "now"}, gateValue, 0, 2, false, "unexpected arguments"},
		{"all hold", "linux", []string{"--" + gateFlag, "facts"}, gateValue, 0, 1, true, "detect failed"},
		{"the flag after the command", "linux", []string{"facts", "--" + gateFlag}, gateValue, 0, 1, true, "detect failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			built := false
			var errOut bytes.Buffer
			e := env{
				goos: tc.goos, euid: func() int { return tc.euid }, now: func() time.Time { return testNow },
				getenv: func(k string) string {
					if k == gateEnv {
						return tc.gate
					}
					return ""
				},
				stdout: io.Discard, stderr: &errOut,
				system: func(io.Writer) host.System { built = true; return hostfake.New(host.Options{}) },
				detect: func(context.Context, host.System) (detect.Facts, error) {
					return detect.Facts{}, errors.New("detect failed")
				},
			}
			if code := run(context.Background(), tc.args, e); code != tc.code {
				t.Errorf("exit %d, want %d; stderr %q", code, tc.code, errOut.String())
			}
			if built != tc.built {
				t.Errorf("host built %v, want %v", built, tc.built)
			}
			if !strings.Contains(errOut.String(), tc.stderr) {
				t.Errorf("stderr %q lacks %q", errOut.String(), tc.stderr)
			}
		})
	}
}

func TestSetDirectives(t *testing.T) {
	port := func(ps ...string) []migrate.Directive {
		var out []migrate.Directive
		for _, p := range ps {
			out = append(out, migrate.Directive{Keyword: "Port", Args: []string{p}})
		}
		return out
	}
	for _, tc := range []struct {
		name string
		old  string
		set  []migrate.Directive
		want string
		err  bool
	}{
		{"a new file", "", port("22", "2230"), dropInHeader + "Port 22\nPort 2230\n", false},
		{"other lines kept, set lines replaced, after the leading comments",
			"# From /etc/ssh/sshd_config.d/manage.conf\nport=22\nAuthorizedKeysFile .ssh/authorized_keys .ssh_%u/authorized_keys\n  Port 2222\nPasswordAuthentication no",
			port("22", "2230"),
			"# From /etc/ssh/sshd_config.d/manage.conf\nPort 22\nPort 2230\nAuthorizedKeysFile .ssh/authorized_keys .ssh_%u/authorized_keys\nPasswordAuthentication no\n", false},
		{"no leading comment", "PermitRootLogin yes\nAllowUsers alice\n",
			[]migrate.Directive{{Keyword: "PermitRootLogin", Args: []string{"prohibit-password"}}},
			"PermitRootLogin prohibit-password\nAllowUsers alice\n", false},
		{"a Match block", "Port 22\nMatch User alice\n  PasswordAuthentication yes\n", port("2230"), "", true},
		{"an Include", "Include /etc/ssh/extra/*.conf\n", port("2230"), "", true},
		{"an argument that needs quotes", "", []migrate.Directive{{Keyword: "Banner", Args: []string{"a b"}}}, "", true},
		{"no argument", "", []migrate.Directive{{Keyword: "Port"}}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var old []byte
			if tc.old != "" {
				old = []byte(tc.old)
			}
			got, err := setDirectives(old, tc.set)
			switch {
			case tc.err && err == nil:
				t.Fatalf("got %q, want an error", got)
			case !tc.err && err != nil:
				t.Fatalf("error %v", err)
			case !tc.err && string(got) != tc.want:
				t.Errorf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestWanted(t *testing.T) {
	after := sshd.Effective{
		Port:                   sshd.Set{Values: []string{"22", "2230"}, Known: true},
		PermitRootLogin:        sshd.Setting{Value: "prohibit-password", Known: true},
		PasswordAuthentication: sshd.Setting{Value: "yes", Known: true},
	}
	d := func(k, v string) migrate.Directive { return migrate.Directive{Keyword: k, Args: []string{v}} }
	for _, tc := range []struct {
		name string
		set  []migrate.Directive
		ok   bool
	}{
		{"the ports", []migrate.Directive{d("Port", "22"), d("Port", "2230")}, true},
		{"a port sshd does not show", []migrate.Directive{d("Port", "2231")}, false},
		{"without-password is prohibit-password", []migrate.Directive{d("PermitRootLogin", "without-password")}, true},
		{"another value", []migrate.Directive{d("PasswordAuthentication", "no")}, false},
		{"Unknown is not a pass", []migrate.Directive{d("KbdInteractiveAuthentication", "no")}, false},
		{"a keyword not checked", []migrate.Directive{d("MaxAuthTries", "3")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := wanted(after, tc.set); (err == nil) != tc.ok {
				t.Errorf("wanted: %v, want ok %v", err, tc.ok)
			}
		})
	}
}

// TestPort2230 runs the brief's scenario on the fake, up to the
// confirmation: armed first, the firewall before sshd, the backup and the
// journal before the write, then the reload, the listener and MarkApplied.
func TestPort2230(t *testing.T) {
	v := newVM(t)
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"--" + gateFlag, "port", "--port", "2230", "--hardn", testHardn}, v.env(debian13, &out, &errOut))
	if code != 0 {
		t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut.String(), out.String())
	}
	e, ok := v.f.Entry(sshd.HardnDropIn)
	if want := dropInHeader + "Port 22\nPort 2230\n"; !ok || string(e.Data) != want || e.Mode.Perm() != 0o644 || e.UID != 0 {
		t.Errorf("00-hardn.conf %q (mode %v), want %q 0644 root's", e.Data, e.Mode, want)
	}
	if !slices.Equal(v.allowed, []uint16{22, 2230}) || !slices.Equal(v.bound, []uint16{22, 2230}) {
		t.Errorf("ufw allows %v, sshd listens on %v; want 22 and 2230 both", v.allowed, v.bound)
	}
	j := v.journal(revert.PendingName)
	switch {
	case j.Applied == nil:
		t.Error("the journal is not marked applied")
	case j.Timer == nil || !slices.Contains(j.Timer.Argv, testHardn) || !slices.Contains(j.Timer.Argv, "rollback"):
		t.Errorf("timer %+v, want %s rollback <id>", j.Timer, testHardn)
	case len(j.Undo) != 1 || strings.Join(j.Undo[0].Argv, " ") != "ufw delete allow 2230/tcp":
		t.Errorf("undo %+v, want ufw delete allow 2230/tcp", j.Undo)
	case j.Backup == nil || !slices.Equal(j.Backup.Seqs, []int{1}) || j.Backup.Dir != path.Join("/var/backups/hardn", j.ID):
		t.Errorf("backup %+v", j.Backup)
	case len(j.Reload) != 1 || j.Reload[0].IfUp || len(j.Reload[0].After) == 0:
		t.Errorf("reload %+v, want ssh after the forward reload ran, with the restored listeners", j.Reload)
	case len(j.ConfirmVia) == 0 || j.ConfirmVia[0].Port != 2230:
		t.Errorf("confirmVia %v, want port 2230", j.ConfirmVia)
	}
	armed := v.index(func(c hostfake.Call) bool { return c.Op == "run" && c.Argv[0] == "systemd-run" })
	allow := v.index(runIs("ufw allow 2230/tcp"))
	backedUp := v.index(func(c hostfake.Call) bool {
		return c.Op == "write-atomic" && strings.HasPrefix(c.Path, "/var/backups/hardn/") && strings.HasSuffix(c.Path, "manifest.json")
	})
	written := v.index(writeOf(sshd.HardnDropIn))
	journalled := slices.IndexFunc(v.f.Calls()[backedUp+1:], writeOf(path.Join(revert.DefaultDir, revert.PendingName))) + backedUp + 1
	reloaded := v.index(runIs("systemctl reload ssh.service"))
	if !(armed >= 0 && armed < allow && allow < backedUp && backedUp < journalled && journalled < written && written < reloaded) {
		t.Errorf("order: armed %d, ufw allow %d, backup %d, journal %d, write %d, reload %d; want ascending", armed, allow, backedUp, journalled, written, reloaded)
	}
	if _, ok := v.f.Entry(path.Join(runRoot, j.ID)); ok {
		t.Error("the run directory is left behind")
	}
	if !strings.Contains(out.String(), "to port 2230 and run") || !strings.Contains(out.String(), "sudo "+testHardn+" confirm") {
		t.Errorf("stdout does not say how to confirm:\n%s", out.String())
	}
}

// TestPortRollsBack: a reload sshd refuses rolls the change back at once,
// while the session is open: the file is gone, the firewall rule too, sshd
// reloaded on 22 alone, the timer stopped, the journal cleared.
func TestPortRollsBack(t *testing.T) {
	v := newVM(t)
	v.failReloads = 1
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"--" + gateFlag, "port", "--port", "2230", "--hardn", testHardn}, v.env(debian13, &out, &errOut))
	if code != 1 || !strings.Contains(errOut.String(), "rolled back") {
		t.Fatalf("exit %d, want 1 rolled back; stderr %s\nstdout %s", code, errOut.String(), out.String())
	}
	if _, ok := v.f.Entry(sshd.HardnDropIn); ok {
		t.Error("00-hardn.conf is still there")
	}
	if !slices.Equal(v.allowed, []uint16{22}) || !slices.Equal(v.bound, []uint16{22}) {
		t.Errorf("ufw allows %v, sshd listens on %v; want 22 alone", v.allowed, v.bound)
	}
	for _, name := range []string{revert.PendingName, revert.RollbackName, revert.RevertedName} {
		if _, ok := v.f.Entry(path.Join(revert.DefaultDir, name)); ok {
			t.Errorf("%s left behind", name)
		}
	}
	if v.index(func(c hostfake.Call) bool { return c.Op == "run" && c.Argv[0] == "systemctl" && c.Argv[1] == "stop" }) < 0 {
		t.Error("the timer was not stopped")
	}
}

// TestPortRefusedBeforeArm: on Proxmox VE the port stays 22 (§3.18), and
// the refusal comes before the timer or any write.
func TestPortRefusedBeforeArm(t *testing.T) {
	pveFacts := debian13
	pveFacts.Platform.Distro = detect.DistroProxmox
	pveFacts.Proxmox = detect.Proxmox{Installed: detect.Yes, Version: "9.0.10", ClusterFS: detect.Yes, EtcPVE: detect.Yes}
	v := newVM(t)
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"--" + gateFlag, "port", "--port", "2230"}, v.env(pveFacts, &out, &errOut))
	if code != 1 || !strings.Contains(errOut.String(), "refused before anything changed") {
		t.Fatalf("exit %d; stderr %s", code, errOut.String())
	}
	for _, c := range v.f.Calls() {
		if c.Op == "write-atomic" || c.Op == "run" && c.Argv[0] != "sshd" {
			t.Errorf("call before the refusal: %v", c)
		}
	}
}

func TestBroken(t *testing.T) {
	v := newVM(t)
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"--" + gateFlag, "broken"}, v.env(debian13, &out, &errOut)); code != 0 {
		t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), "refused: "+sshd.HardnDropIn+" line 3") {
		t.Errorf("stdout does not name the real file:\n%s", out.String())
	}
	for _, c := range v.f.Calls() {
		if (c.Op == "write-atomic" || c.Op == "remove") && !strings.HasPrefix(c.Path, runRoot+"/") {
			t.Errorf("a change outside the run directory: %v", c)
		}
	}
}

func TestSudoers(t *testing.T) {
	v := newVM(t)
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"--" + gateFlag, "sudoers", "--user", "alice", "--grant", "nopasswd"}, v.env(debian13, &out, &errOut)); code != 0 {
		t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut.String(), out.String())
	}
	grant := "/etc/sudoers.d/hardn-grant-alice"
	e, ok := v.f.Entry(grant)
	if !ok || !strings.Contains(string(e.Data), "alice ALL=(ALL) NOPASSWD: ALL") || e.Mode.Perm() != 0o440 {
		t.Fatalf("%s: %q %v", grant, e.Data, e.Mode)
	}
	backedUp := v.index(func(c hostfake.Call) bool {
		return c.Op == "write-atomic" && strings.HasSuffix(c.Path, "manifest.json")
	})
	checked := v.index(func(c hostfake.Call) bool { return c.Op == "run" && c.Argv[0] == "visudo" && c.Argv[1] == "-cf" })
	if written := v.index(writeOf(grant)); !(backedUp >= 0 && backedUp < checked && checked < written) {
		t.Errorf("order: backup %d, visudo -cf %d, write %d; want ascending", backedUp, checked, written)
	}
}

// TestUFWEnable: allow before enable (brief 2.3 step 2; ufw.md §2.7), under
// confirm-or-revert, from an inactive ufw with no rules.
func TestUFWEnable(t *testing.T) {
	v := newVM(t)
	v.allowed, v.inactive = nil, true
	v.saveRules()
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"--" + gateFlag, "ufw-enable"}, v.env(debian13, &out, &errOut)); code != 0 {
		t.Fatalf("exit %d; stderr %s\nstdout %s", code, errOut.String(), out.String())
	}
	allow, enable := v.index(runIs("ufw allow 22/tcp")), v.index(runIs("ufw --force enable"))
	if armed := v.index(func(c hostfake.Call) bool { return c.Op == "run" && c.Argv[0] == "systemd-run" }); !(armed >= 0 && armed < allow && allow < enable) {
		t.Errorf("order: armed %d, allow %d, enable %d; want ascending", armed, allow, enable)
	}
	if v.inactive || !slices.Equal(v.allowed, []uint16{22}) {
		t.Errorf("ufw inactive %v, allows %v; want active with 22", v.inactive, v.allowed)
	}
	j := v.journal(revert.PendingName)
	var undo []string
	for _, s := range j.Undo {
		undo = append(undo, strings.Join(s.Argv, " "))
	}
	if want := []string{"ufw disable", "ufw delete allow 22/tcp"}; j.Applied == nil || !slices.Equal(undo, want) || j.Backup != nil || j.Reload != nil {
		t.Errorf("journal applied %v, undo %q (want %q), backup %v, reload %v", j.Applied != nil, undo, want, j.Backup, j.Reload)
	}
}

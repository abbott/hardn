package main

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// passOps are the fake's records of each method the guard passes through:
// ReadFile asks what the file is first.
var passOps = map[string][]string{
	"ReadFile": {"stat", "read"}, "ReadFileNoFollow": {"read-nofollow"}, "Stat": {"stat"}, "Lstat": {"lstat"},
	"ReadDir": {"stat", "readdir"}, "Readlink": {"readlink"}, "ResolveSymlink": {"resolve-symlink"},
	"LookupUser": {"lookup-user"}, "LookupGroup": {"lookup-group"},
}

// TestGuardDecidesEveryMethod holds the guard to decisions, and decisions
// to host.System: every method of the interface is decided, and does
// what its decision says. A refused method makes no call on the host
// under the guard and is recorded; a read is passed through once; Run
// runs an allowed command and refuses any other with no call made.
func TestGuardDecidesEveryMethod(t *testing.T) {
	iface := reflect.TypeFor[host.System]()
	var methods []string
	for i := range iface.NumMethod() {
		methods = append(methods, iface.Method(i).Name)
	}
	if got := slices.Sorted(maps.Keys(decisions)); !slices.Equal(got, methods) {
		t.Fatalf("decisions name %v; host.System has %v: decide every method in guard.go", got, methods)
	}
	ctx := context.Background()
	for _, name := range methods {
		t.Run(name, func(t *testing.T) {
			f := hostfake.New(host.Options{})
			f.Handler = func([]string, []byte) hostfake.Response { return hostfake.Response{} }
			g := newGuard(f)
			call := func(argv ...string) error {
				m := reflect.ValueOf(g).MethodByName(name)
				var in []reflect.Value
				for i := range m.Type().NumIn() {
					switch p := m.Type().In(i); {
					case p == reflect.TypeFor[context.Context]():
						in = append(in, reflect.ValueOf(ctx))
					case m.Type().IsVariadic() && i == m.Type().NumIn()-1:
						in = append(in, reflect.ValueOf(argv[1:]))
					case p.Kind() == reflect.String && m.Type().IsVariadic():
						in = append(in, reflect.ValueOf(argv[0]))
					case p.Kind() == reflect.String:
						in = append(in, reflect.ValueOf("/etc/ssh/sshd_config"))
					default:
						in = append(in, reflect.Zero(p))
					}
				}
				var out []reflect.Value
				if m.Type().IsVariadic() {
					out = m.CallSlice(in)
				} else {
					out = m.Call(in)
				}
				err, _ := out[len(out)-1].Interface().(error)
				return err
			}
			switch decisions[name] {
			case pass:
				f.AddFile("/etc/ssh/sshd_config", []byte("Port 22\n"), 0o644, 0, 0)
				_ = call()
				var ops []string
				for _, c := range f.Calls() {
					ops = append(ops, c.Op)
				}
				if !slices.Equal(ops, passOps[name]) {
					t.Errorf("calls %v, want %v", f.Calls(), passOps[name])
				}
				if len(g.calls()) != 0 {
					t.Errorf("recorded %v", g.calls())
				}
			case refuse:
				if err := call("systemctl", "is-active", "ssh.service"); !errors.Is(err, errRefused) {
					t.Errorf("error %v, want refused", err)
				}
				if calls := f.Calls(); len(calls) != 0 {
					t.Errorf("calls %v, want none", calls)
				}
				if g.refusals() != 1 {
					t.Errorf("recorded %v, want one refusal", g.calls())
				}
			case allow:
				if err := call("systemctl", "restart", "ssh.service"); !errors.Is(err, errRefused) || len(f.Calls()) != 0 {
					t.Errorf("systemctl restart: error %v, calls %v", err, f.Calls())
				}
				if err := call("systemctl", "is-active", "ssh.service"); err != nil || len(f.Runs()) != 1 {
					t.Errorf("systemctl is-active: error %v, runs %v", err, f.Runs())
				}
				if cs := g.calls(); len(cs) != 2 || !cs[0].refused() || cs[1].refused() || cs[1].exit != 0 {
					t.Errorf("recorded %+v", cs)
				}
			default:
				t.Errorf("decision %d", decisions[name])
			}
		})
	}
}

// TestGuardIsNoLegacySystem: the guard has no in-place write, so code that
// asserts its System to host.LegacySystem finds none.
func TestGuardIsNoLegacySystem(t *testing.T) {
	var sys host.System = newGuard(hostfake.New(host.Options{}))
	if _, ok := sys.(host.LegacySystem); ok {
		t.Error("the guard is a host.LegacySystem")
	}
}

// TestAllowed checks each rule's shape: the commands the checks and the
// probe run, and near misses of each, refused.
func TestAllowed(t *testing.T) {
	const format = `--showformat=${Status}\t${Version}\n`
	yes := [][]string{
		{"sshd", "-T", "-f", "/etc/ssh/sshd_config"},
		{"sshd", "-T", "-f", "/etc/ssh/sshd_config", "-C", "user=hardn-probe,host=probe.invalid,addr=192.0.2.1"},
		{"sshd", "-T", "-f", "/etc/ssh/sshd_config", "-C", "user=root,host=web-01.example.net,addr=192.0.2.7,laddr=192.0.2.20,lport=2230,rdomain=vrf1"},
		{"/usr/sbin/sshd", "-T", "-f", "/etc/ssh/sshd_config", "-C", "user=alice,host=probe.invalid,addr=192.0.2.1"},
		{"/usr/sbin/sshd.pam", "-f", "/etc/ssh/sshd_config", "-G"},
		{"/usr/sbin/sshd.pam", "-T", "-f", "/etc/ssh/sshd_config"},
		{"/usr/sbin/sshd.krb5", "-f", "/etc/ssh/sshd_config", "-G"},
		{"systemctl", "is-enabled", "ssh.socket"}, {"systemctl", "is-active", "ssh.service"},
		{"systemctl", "is-enabled", "apparmor.service"}, {"systemctl", "is-enabled", "apt-daily-upgrade.service"},
		{"systemctl", "is-active", "apt-daily.timer"}, {"systemctl", "is-active", "pve-firewall.service"},
		{"systemctl", "is-active", "proxmox-firewall.service"}, {"systemctl", "show", "-p", "Listen", "ssh.socket"},
		{"ss", "-Htln"}, {"ufw", "status", "verbose"},
		{"apt-config", "dump", "APT::Periodic", "Unattended-Upgrade::Origins-Pattern", "Unattended-Upgrade::Allowed-Origins"},
		{"dpkg-query", "--show", format, "pve-manager"}, {"dpkg-query", "--show", format, "unattended-upgrades"},
		{"dpkg-query", "--show", format, "linux-image-6.12.43+deb13-amd64"},
		{"sudo", "--version"}, {"visudo", "-c"},
		{"doas", "-C", "/etc/doas.conf"}, {"doas", "-C", "/etc/doas.d/20-wheel.conf"},
		{"ip", "-4", "addr", "show", "dev", "eth0"}, {"ip", "-4", "addr", "show", "dev", "vmbr0"},
		{"apk", "info", "-e", "apk-cron"}, {"rc-service", "crond", "status"},
	}
	no := [][]string{
		{}, {""}, {"sh", "-c", "sshd -T"}, {"uname", "-r"}, {"pveversion"}, {"aa-status"}, {"rc-status"}, {"hostname"},
		{"sshd", "-t", "-f", "/etc/ssh/sshd_config"}, {"sshd", "-T"}, {"sshd", "-T", "-f", "/tmp/sshd_config"},
		{"sshd", "-T", "-f", "/etc/ssh/sshd_config", "-C", "user=alice,host=probe.invalid"},
		{"sshd", "-T", "-f", "/etc/ssh/sshd_config", "-C", "user=a b,host=h,addr=192.0.2.1"},
		{"sshd", "-T", "-f", "/etc/ssh/sshd_config", "-C", "user=a,host=h,addr=192.0.2.1", "-o", "Port=1"},
		{"sshd", "-T", "-f", "/etc/ssh/sshd_config", "-o", "user=a,host=h,addr=192.0.2.1"},
		{"sshd", "-D", "-f", "/etc/ssh/sshd_config"}, {"/usr/sbin/sshd", "-f", "/etc/ssh/sshd_config", "-G"},
		{"/usr/local/sbin/sshd", "-T", "-f", "/etc/ssh/sshd_config"}, {"/usr/sbin/sshd.pam", "-f", "/etc/ssh/sshd_config", "-G", "-o", "x"},
		{"systemctl", "restart", "ssh.service"}, {"systemctl", "reload", "ssh.service"}, {"systemctl", "daemon-reload"},
		{"systemctl", "is-enabled", "sshd.service"}, {"systemctl", "is-active", "apparmor.service", "--now"},
		{"systemctl", "is-active"}, {"systemctl", "enable", "apparmor.service"}, {"systemctl", "show", "ssh.socket"},
		{"systemctl", "show", "-p", "Listen", "ssh.service"}, {"systemctl", "start", "apt-daily.timer"},
		{"ss", "-K"}, {"ss", "-Htln", "-K"}, {"ss"},
		{"ufw", "status"}, {"ufw", "enable"}, {"ufw", "status", "verbose", "numbered"}, {"ufw", "--force", "reset"},
		{"apt-config", "dump"}, {"apt-config", "shell", "X", "APT::Periodic"},
		{"dpkg-query", "--show", format, "linux-image-*"}, {"dpkg-query", "--show", format, "vim"},
		{"dpkg-query", "-W", "unattended-upgrades"}, {"dpkg-query", "--show", format, "linux-image-6.1", "x"},
		{"dpkg", "--configure", "-a"},
		{"sudo", "-l"}, {"sudo", "--version", "-u", "root"}, {"sudo", "systemctl", "restart", "ssh"},
		{"visudo"}, {"visudo", "-cf", "/etc/sudoers.d/hardn"}, {"visudo", "-c", "-f", "/tmp/x"},
		{"doas", "-C", "/tmp/doas.conf"}, {"doas", "-C", "/etc/doas.d/../shadow.conf"}, {"doas", "-C", "/etc/doas.d/x.conf.bak"},
		{"doas", "-C", "/etc/doas.d/sub/x.conf"}, {"doas", "-C", "/etc/doas.conf", "reboot"}, {"doas", "reboot"},
		{"ip", "-4", "addr", "show", "dev", "eth0", "x"}, {"ip", "link", "set", "eth0", "down"},
		{"ip", "-4", "addr", "show", "dev", "eth0:1"}, {"ip", "-4", "addr", "show", "dev", "../x"},
		{"ip", "-4", "addr", "show", "dev", "abcdefghijklmnop"}, {"ip", "-4", "addr", "flush", "dev", "eth0"},
		{"apk", "add", "apk-cron"}, {"apk", "info", "-e", "vim"}, {"apk", "-U", "upgrade"},
		{"rc-service", "crond", "start"}, {"rc-service", "sshd", "status"}, {"rc-update", "add", "crond", "default"},
	}
	for _, argv := range yes {
		if !allowed(argv) {
			t.Errorf("%q refused", argv)
		}
	}
	for _, argv := range no {
		if allowed(argv) {
			t.Errorf("%q allowed", argv)
		}
	}
}

// TestRulesMatchTheCode holds the words of the rules to the constants of
// the code that runs them, where they are exported.
func TestRulesMatchTheCode(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{sshd.DefaultBinary, "sshd"}, {sshd.MainConfig, "/etc/ssh/sshd_config"}, {sshd.SocketUnit, "ssh.socket"},
		{reload.BinarySSHD, "/usr/sbin/sshd"}, {reload.BinaryPAM, "/usr/sbin/sshd.pam"}, {reload.BinaryKrb5, "/usr/sbin/sshd.krb5"},
		{reload.ServiceUnit, "ssh.service"}, {pve.FirewallUnit, "pve-firewall.service"}, {pve.NFTablesUnit, "proxmox-firewall.service"},
		{alpine.Package, "apk-cron"}, {alpine.Service, "crond"},
		{sudoers.DoasConfig, "/etc/doas.conf"}, {sudoers.DoasDir, "/etc/doas.d"}, {header.KernelPackagePrefix, "linux-image-"},
	} {
		if c.got != c.want {
			t.Errorf("the code has %q where the rules have %q", c.got, c.want)
		}
	}
	if spec := sshd.DefaultConn().Spec(); !connSpec.MatchString(spec) {
		t.Errorf("sshd.DefaultConn's spec %q does not match connSpec", spec)
	}
	for verb, us := range units {
		for _, u := range us {
			if strings.ContainsAny(u, " /") || !strings.Contains(u, ".") {
				t.Errorf("%s %q is not a unit's name", verb, u)
			}
		}
	}
}

// deviceSys is a fake on which name is a character device.
type deviceSys struct {
	*hostfake.System
	name string
}

func (s deviceSys) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	if name == s.name {
		return deviceInfo{}, nil
	}
	return s.System.Stat(ctx, name)
}

// deviceInfo describes a character device.
type deviceInfo struct{ fs.FileInfo }

func (deviceInfo) Mode() fs.FileMode { return fs.ModeDevice | fs.ModeCharDevice | 0o600 }
func (deviceInfo) IsDir() bool       { return false }

// TestGuardReadsNoDevice: ReadFile and ReadDir of what is neither a
// regular file nor a directory (here a device, where a symlink in a
// configuration directory could lead) are refused before it is opened.
func TestGuardReadsNoDevice(t *testing.T) {
	ctx := context.Background()
	f := hostfake.New(host.Options{})
	g := newGuard(deviceSys{f, "/etc/doas.d/x.conf"})
	if _, err := g.ReadFile(ctx, "/etc/doas.d/x.conf"); !errors.Is(err, errRefused) {
		t.Errorf("ReadFile: error %v, want refused", err)
	}
	if _, err := g.ReadDir(ctx, "/etc/doas.d/x.conf"); !errors.Is(err, errRefused) {
		t.Errorf("ReadDir: error %v, want refused", err)
	}
	if slices.ContainsFunc(f.Calls(), func(c hostfake.Call) bool { return c.Op == "read" || c.Op == "readdir" }) || g.refusals() != 2 {
		t.Errorf("calls %v, recorded %v", f.Calls(), g.calls())
	}
}

// TestGuardReadsPassErrors: a read the host refuses comes back as the
// host's own error, not as the guard's refusal.
func TestGuardReadsPassErrors(t *testing.T) {
	g := newGuard(hostfake.New(host.Options{}))
	if _, err := g.ReadFile(context.Background(), "/etc/passwd"); !errors.Is(err, fs.ErrNotExist) || errors.Is(err, errRefused) {
		t.Errorf("error %v, want the host's not-exist", err)
	}
	if g.refusals() != 0 {
		t.Errorf("recorded %v", g.calls())
	}
}

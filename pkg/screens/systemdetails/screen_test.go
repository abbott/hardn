package systemdetails

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine/details"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The System Details screen through Run and Print, on hostfake, on the states the
// gallery's hosts cover (gallery-system-details.md §3.1), each command's output in
// the forms the VMs printed (readers-system-details.md §3), ZFS's from their
// manuals, no VM having it. Synthetic values only: alice, web-01, RFC 5737 and 3849
// addresses. These tests are in the package, not beside the parity table's in
// systemdetails_test, so that each set's many small names stay its own.

// now is the clock of every test: the hosts' logins are this year's.
var now = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

func clock() time.Time { return now }

const (
	vmCPU      = "QEMU Virtual CPU version 2.5+"
	serverCPU  = "Intel(R) Xeon(R) Gold 6338 CPU @ 2.00GHz"
	pveDataset = "rpool/ROOT/pve-1"
	wtmpRecord = "\x06\x00\x00\x00\x10\x02\x00\x00LOGIN\x00"
	rootStatus = "Name:\thardn\nUid:\t0\t0\t0\t0\nGid:\t0\t0\t0\t0\n"
	userStatus = "Name:\thardn\nUid:\t1000\t1000\t1000\t1000\nGid:\t1000\t1000\t1000\t1000\n"
	memVM      = "MemTotal:        4030544 kB\nMemFree:          812344 kB\nMemAvailable:    2801234 kB\nBuffers:           91234 kB\n"
	memServer  = "MemTotal:       131072000 kB\nMemFree:        81234000 kB\nMemAvailable:   96123000 kB\n"
	dfRoot     = "Filesystem     1024-blocks    Used Available Capacity Mounted on\n/dev/vda1         40470732 4412345  36058387      11% /\n"
	passwd     = "root:x:0:0:root:/root:/bin/bash\nnobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\nalice:x:1000:1000::/home/alice:/bin/bash\n"
	aliceLogin = "alice 2026-10-20T11:42:05+00:00 pts/0 192.0.2.10"
)

// lsloginsArgs is how the reader asks lslogins about names, which follow -l.
var lsloginsArgs = []string{"--time-format=iso", "--noheadings", "--raw", "--output=USER,LAST-LOGIN,LAST-TTY,LAST-HOSTNAME", "-l"}

// model is what a host answers beyond its files.
type model struct {
	addrs, df, health, zfs string
	logins                 map[string]string // a name's line; another listed name answers blanks
	lslogins               *hostfake.Response
}

// testHost is one host: the fake, and the facts detect gives of it.
type testHost struct {
	*hostfake.System
	facts detect.Facts
	m     model
	// base is how many calls making the host took.
	base int
}

// Stat is the fake's, with the elevation tools setuid, which the rules' reader asks
// of them and the fake's modes do not hold.
func (h *testHost) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	fi, err := h.System.Stat(ctx, name)
	if err == nil && (name == "/usr/bin/sudo" || name == "/usr/bin/doas") {
		return setuid{fi}, nil
	}
	return fi, err
}

type setuid struct{ fs.FileInfo }

func (i setuid) Mode() fs.FileMode { return i.FileInfo.Mode() | fs.ModeSetuid }

func ipLine(index int, iface, family, addr string) string {
	if family == "inet6" {
		return fmt.Sprintf("%d: %s    inet6 %s scope global \\       valid_lft 7104sec preferred_lft 7104sec\n", index, iface, addr)
	}
	return fmt.Sprintf("%d: %s    inet %s metric 100 brd 192.0.2.255 scope global dynamic %s\\       valid_lft 7104sec preferred_lft 7104sec\n", index, iface, addr, iface)
}

func cpuinfo(model string, n, sockets int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "processor\t: %d\nmodel name\t: %s\nphysical id\t: %d\n\n", i, model, i%sockets)
	}
	return b.String()
}

// add writes data at name, readable.
func (h *testHost) add(name, data string) { h.AddFile(name, []byte(data), 0o644, 0, 0) }

// on returns a host of the release: its files, sudo or doas, the kernel, its CPU;
// alice in the sudo group, or on doas in wheel, their login from 192.0.2.10, root's
// none.
func on(release string) *testHost {
	h := &testHost{System: hostfake.New(host.Options{})}
	h.m = model{addrs: ipLine(2, "eth0", "inet", "192.0.2.20/24"), df: dfRoot, logins: map[string]string{"alice": aliceLogin}}
	h.add(header.HostnameFile, "web-01\n")
	h.add(header.UptimeFile, "277920.37 553000.12\n")
	h.add(details.LoadAvgFile, "0.12 0.08 0.03 1/123 4567\n")
	h.add(details.SelfStatusFile, rootStatus)
	h.add(details.MemInfoFile, memVM)
	h.add(details.CPUInfoFile, cpuinfo(vmCPU, 4, 1))
	h.add(detect.SelfMounts, "proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0\n/dev/vda1 / ext4 rw,relatime 0 0\n")
	h.add("/etc/passwd", passwd)
	h.add("/etc/group", "root:x:0:\nsudo:x:27:alice\nwheel:x:10:alice\nalice:x:1000:\n")
	sudo := detect.Elevation{Sudo: detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}, Doas: detect.Tool{Present: detect.No}}
	kernel := "6.12.113+deb13-cloud-amd64"
	switch release {
	case "debian13":
		h.facts = detect.Facts{OS: detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie"}, Elevation: sudo}
	case "ubuntu2604":
		kernel = "7.0.0-38-generic"
		sudo.SudoFlavor = detect.SudoRS
		h.facts = detect.Facts{OS: detect.OSRelease{ID: "ubuntu", VersionID: "26.04", VersionCodename: "resolute"}, Elevation: sudo}
	case "alpine323":
		kernel = "6.18.53-0-virt"
		h.facts = detect.Facts{OS: detect.OSRelease{ID: "alpine", VersionID: "3.23.6"}, Platform: detect.Platform{Distro: detect.DistroAlpine},
			Elevation: detect.Elevation{Sudo: detect.Tool{Present: detect.No}, Doas: detect.Tool{Present: detect.Yes, Path: "/usr/bin/doas"}}}
		h.AddFile("/usr/bin/doas", []byte("ELF"), 0o755, 0, 0)
		h.AddFile(sudoers.DoasConfig, []byte("# permit persist :wheel\n"), 0o640, 0, 0)
		h.AddDir(sudoers.DoasDir, 0o750, 0, 0)
		h.AddFile(sudoers.DoasDir+"/20-wheel.conf", []byte("permit persist :wheel\n"), 0o440, 0, 0)
		h.m.lslogins = &hostfake.Response{Err: fmt.Errorf("host: %q: not in %s: %w", "lslogins", host.SearchPath, host.ErrNotFound)}
	}
	h.add(header.KernelReleaseFile, kernel+"\n")
	if release != "alpine323" {
		h.AddFile("/usr/bin/sudo", []byte("ELF"), 0o755, 0, 0)
		h.AddFile(sudoers.MainConfig, []byte("root ALL=(ALL:ALL) ALL\n%sudo ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n"), 0o440, 0, 0)
		h.AddDir(sudoers.DropInDir, 0o750, 0, 0)
		h.AddFile(details.WtmpFile, []byte(wtmpRecord), 0o664, 0, 43)
	}
	h.Handler = h.answer
	return h
}

// answer answers the reader's commands.
func (h *testHost) answer(argv []string, _ []byte) hostfake.Response {
	m := h.m
	cmd := strings.Join(argv, " ")
	switch {
	case cmd == "ip -o addr show scope global":
		return hostfake.Response{Stdout: m.addrs}
	case cmd == "df -kP /":
		return hostfake.Response{Stdout: m.df}
	case cmd == "visudo -c":
		return hostfake.Response{Stdout: "/etc/sudoers: parsed OK\n"}
	case len(argv) == 3 && argv[0] == "doas" && argv[1] == "-C":
		return hostfake.Response{}
	case argv[0] == "zpool" && m.health != "":
		return hostfake.Response{Stdout: m.health + "\n"}
	case cmd == "zfs get -Hp -o value used,available "+pveDataset && m.zfs != "":
		return hostfake.Response{Stdout: m.zfs}
	case argv[0] == "lslogins" && m.lslogins != nil:
		return *m.lslogins
	case len(argv) == len(lsloginsArgs)+2 && argv[0] == "lslogins" && slices.Equal(argv[1:len(argv)-1], lsloginsArgs):
		var b strings.Builder
		for _, name := range strings.Split(argv[len(argv)-1], ",") {
			if line, ok := m.logins[name]; ok {
				b.WriteString(line + "\n")
			} else {
				b.WriteString(name + "   \n")
			}
		}
		return hostfake.Response{Stdout: b.String()}
	}
	return hostfake.Response{Err: fmt.Errorf("%s: %w", cmd, hostfake.ErrUnscripted), ExitCode: -1}
}

// pve is a Proxmox VE 9 node with root on ZFS, its pool as health says, no user but
// root, root's own login.
func pve(health string) *testHost {
	h := on("debian13")
	h.facts.OS.VersionCodename = "trixie"
	h.facts.Proxmox = detect.Proxmox{Installed: detect.Yes, Version: "9.0.10"}
	h.facts.Elevation = detect.Elevation{Sudo: detect.Tool{Present: detect.No}, Doas: detect.Tool{Present: detect.No}}
	h.add(header.KernelReleaseFile, "6.17.2-1-pve\n")
	h.add(header.UptimeFile, "3546000.00 7000000.00\n")
	h.add("/etc/passwd", "root:x:0:0:root:/root:/bin/bash\n")
	h.add("/etc/group", "root:x:0:\n")
	h.add(detect.SelfMounts, "proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0\n"+pveDataset+" / zfs rw,relatime,xattr,noacl 0 0\n")
	h.add(details.CPUInfoFile, cpuinfo(serverCPU, 48, 2))
	h.add(details.MemInfoFile, memServer)
	h.m.addrs = ipLine(4, "vmbr0", "inet", "192.0.2.20/24")
	h.m.health, h.m.zfs = health, "53687091200\n429496729600\n"
	h.m.logins = map[string]string{"root": "root 2026-10-20T11:42:05+00:00 pts/0 192.0.2.10"}
	return h
}

// many is Debian 13 with 14 users and 9 addresses, two of the users having logged in.
func many() *testHost {
	h := on("debian13")
	pw, group := passwd, "root:x:0:\nalice:x:1000:\n"
	var inSudo []string
	for i, u := range []string{"bob", "carol", "dave", "erin", "frank", "grace", "heidi", "ivan", "judy", "mallory", "niaj", "olivia", "peggy"} {
		pw += fmt.Sprintf("%s:x:%d:%d::/home/%s:/bin/bash\n", u, 1001+i, 1001+i, u)
		group += fmt.Sprintf("%s:x:%d:\n", u, 1001+i)
		if i < 4 {
			inSudo = append(inSudo, u)
		}
	}
	h.add("/etc/passwd", pw)
	h.add("/etc/group", group+"sudo:x:27:alice,"+strings.Join(inSudo, ",")+"\n")
	h.m.logins["bob"] = "bob 2026-10-19T08:01:22+00:00 pts/1 192.0.2.11"
	h.m.addrs = ipLine(2, "eth0", "inet", "192.0.2.20/24") + ipLine(2, "eth0", "inet", "192.0.2.21/24") + ipLine(2, "eth0", "inet6", "2001:db8:0:1::20/64") +
		ipLine(3, "eth1", "inet", "198.51.100.20/24") + ipLine(3, "eth1", "inet6", "2001:db8:0:2::20/64") + ipLine(4, "bond0", "inet", "203.0.113.20/24") +
		ipLine(5, "wg0", "inet", "198.18.0.20/32") + ipLine(5, "wg0", "inet6", "2001:db8:0:3::20/128") + ipLine(6, "docker0", "inet", "198.19.0.1/16")
	return h
}

// hosts are the gallery's nine.
var hosts = map[string]func() *testHost{
	"debian13":     func() *testHost { return on("debian13") },
	"ubuntu2604":   func() *testHost { return on("ubuntu2604") },
	"alpine323":    func() *testHost { return on("alpine323") },
	"pve9":         func() *testHost { return pve("ONLINE") },
	"zfs-degraded": func() *testHost { return pve("DEGRADED") },
	"unreadable": func() *testHost {
		h := on("debian13")
		if err := h.Remove(context.Background(), details.MemInfoFile); err != nil {
			panic(err)
		}
		h.base = len(h.Calls())
		return h
	},
	"no-logins": func() *testHost { h := on("debian13"); h.m.logins = nil; return h },
	"login-console": func() *testHost {
		h := on("debian13")
		h.m.logins["alice"] = "alice 2026-10-20T11:42:05+00:00 tty1 "
		return h
	},
	"many": many,
}

// played is one run of the screen: what it wrote with the answers echoed, what Run
// returned, and how many lines were left unread.
type played struct {
	out    string
	err    error
	unread int
}

// play runs the screen on h at 80 columns, plain, the user typing lines.
func play(h *testHost, lines ...string) played {
	return playIn(h, offEnv(), lines...)
}

// playIn is play in env.
func playIn(h *testHost, env term.Env, lines ...string) played {
	var out bytes.Buffer
	ty := uitest.NewTypist(&out, lines...)
	err := Run(context.Background(), Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		System:   h, Facts: h.facts, Now: clock,
	})
	return played{out.String(), err, ty.Unread()}
}

// print writes the report on h in env.
func printed(t *testing.T, h *testHost, env term.Env) string {
	t.Helper()
	var out bytes.Buffer
	if err := Print(context.Background(), PrintOptions{Out: &out, Env: env, System: h, Facts: h.facts, Now: clock}); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// terminalEnv is an interactive terminal's environment, 80x24.
func terminalEnv() term.Env {
	return term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Height: 24, Environ: []string{"TERM=xterm-256color", "COLORTERM=truecolor", "LANG=C.UTF-8"}})
}

// offEnv is hardn's environment off a terminal, stdout a pipe.
func offEnv() term.Env { return term.Resolve(term.Options{}, term.Facts{Width: 80}) }

// sgr is a colour or attribute's escape sequence.
var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// strip is s with its colours gone, and only those.
func strip(s string) string { return sgr.ReplaceAllString(s, "") }

// assertOnlyReads fails t for anything but a read among the calls on h since it was
// made: a file changed, a lock, a process started, a pager; and a command no draw runs.
func assertOnlyReads(t *testing.T, name string, h *testHost) {
	t.Helper()
	for _, c := range h.Calls()[h.base:] {
		switch c.Op {
		case "read", "read-nofollow", "scan-lines", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group":
		case "run":
			if !slices.Contains([]string{"ip", "df", "zpool", "zfs", "visudo", "doas", "lslogins"}, path.Base(c.Argv[0])) {
				t.Errorf("%s: ran %s", name, c)
			}
		default:
			t.Errorf("%s: %s", name, c)
		}
	}
}

// count is how many calls on h from the nth on c matches.
func count(h *testHost, n int, match func(hostfake.Call) bool) int {
	got := 0
	for _, c := range h.Calls()[n:] {
		if match(c) {
			got++
		}
	}
	return got
}

// lslogins matches one lslogins call, of which one details.Read makes one, and
// meminfo the one read of /proc/meminfo.
func lslogins(c hostfake.Call) bool { return c.Op == "run" && c.Argv[0] == "lslogins" }
func meminfo(c hostfake.Call) bool  { return c.Op == "read" && c.Path == details.MemInfoFile }

// TestScreenHosts: every host through Run: the rows its reading holds, in the
// decided words, the items, and the report whole, as many lines at any height, the
// prompt last (decision 4 (a)), writing nothing.
func TestScreenHosts(t *testing.T) {
	session := "Oct 20 11:42 from 192.0.2.10"
	for _, tt := range []struct {
		host  string
		lines int
		want  []string
	}{
		{"debian13", 44, []string{"OS                   Debian 13 (Trixie)", "Kernel               6.12.113+deb13-cloud-amd64", "Uptime               3 days, 5 hours, 12 minutes",
			"Hostname             web-01", "eth0                 192.0.2.20/24", "alice                sudo", "Model                " + vmCPU, "CPUs                 4, 1 socket",
			"Load (1, 5, 15 min)  0.12, 0.08, 0.03", "/                    /dev/vda1 ext4", "Used                 4.2 of 38.6 GiB (11%)", "Used                 1.2 of 3.8 GiB (30%)",
			"root                 None recorded", "alice                " + session}},
		{"ubuntu2604", 44, []string{"OS                   Ubuntu 26.04 (Resolute)", "Kernel               7.0.0-38-generic", "alice                sudo", "alice                " + session}},
		{"alpine323", 43, []string{"OS                   Alpine 3.23.6", "alice                doas", "Logins               Not known: lslogins isn't installed"}},
		{"pve9", 44, []string{"OS                   Proxmox VE 9.0.10 (Trixie)", "None but root", "/                    " + pveDataset + " zfs", "Used                 50.0 of 450.0 GiB (11%)",
			"[+] Pool rpool           ONLINE", "CPUs                 48, 2 sockets", "root                 " + session, "Used                 33.3 of 125.0 GiB (27%)"}},
		{"zfs-degraded", 44, []string{"[ ] Pool rpool           DEGRADED"}},
		{"unreadable", 44, []string{"Can't read /proc/meminfo", "alice                " + session, "Used                 4.2 of 38.6 GiB (11%)"}},
		{"no-logins", 44, []string{"root                 None recorded", "alice                None recorded"}},
		{"login-console", 44, []string{"alice                Oct 20 11:42 on tty1"}},
		{"many", 78, []string{"2001:db8:0:3::20/128", "peggy                No sudo", "bob                  sudo", "bob                  Oct 19 08:01 from 192.0.2.11", "peggy                None recorded"}},
	} {
		h := hosts[tt.host]()
		p := play(h, "0")
		if p.err != nil || p.unread != 0 {
			t.Errorf("%s: Run = %v, %d lines unread", tt.host, p.err, p.unread)
		}
		lines := strings.Split(strings.TrimRight(p.out, "\n"), "\n")
		if got := slices.Index(lines, "    > Enter selection [0-1 or q]: 0"); got != tt.lines-1 {
			t.Errorf("%s: the prompt at line %d, want %d:\n%s", tt.host, got+1, tt.lines, p.out)
		}
		for _, w := range tt.want {
			if !strings.Contains(p.out, w) {
				t.Errorf("%s: no %q in:\n%s", tt.host, w, p.out)
			}
		}
		for _, w := range []string{"# System Details ###", "# OS and Kernel ---", "# Host and IPs ---", "# Users ---", "# CPU and Load ---", "# Disk ---", "# Memory ---", "# Last Login ---",
			"  Select an option", "   1) Refresh              Read the host again", "   0) Return"} {
			if !strings.Contains(p.out, w) {
				t.Errorf("%s: no %q in:\n%s", tt.host, w, p.out)
			}
		}
		assertOnlyReads(t, tt.host, h)
	}
	if len(hosts) != 9 {
		t.Errorf("%d hosts", len(hosts))
	}
}

// TestDrawsOnce: every draw reads the host once (decision 2 (a), defect 20): one
// lslogins and one read of /proc/meminfo for the opening draw and one for each
// Refresh, none for a line the prompt refuses, which does not draw again. On Print,
// one.
func TestDrawsOnce(t *testing.T) {
	for _, tt := range []struct {
		lines []string
		draws int
	}{{[]string{"0"}, 1}, {[]string{"1", "0"}, 2}, {[]string{"1", "1", "q"}, 3}, {[]string{"7", "", "b", "0"}, 1}} {
		h := hosts["debian13"]()
		play(h, tt.lines...)
		if got := count(h, 0, lslogins); got != tt.draws {
			t.Errorf("%q: lslogins %d times, want %d", tt.lines, got, tt.draws)
		}
		if got := count(h, 0, meminfo); got != tt.draws {
			t.Errorf("%q: /proc/meminfo %d times, want %d", tt.lines, got, tt.draws)
		}
	}
	h := hosts["debian13"]()
	printed(t, h, offEnv())
	if count(h, 0, lslogins) != 1 || count(h, 0, meminfo) != 1 {
		t.Errorf("Print: lslogins %d times, /proc/meminfo %d", count(h, 0, lslogins), count(h, 0, meminfo))
	}
}

// TestRefresh: item 1 draws from a fresh reading: what changed on the host shows,
// under what the last round left, nothing cleared.
func TestRefresh(t *testing.T) {
	h := hosts["debian13"]()
	first := "Uptime               3 days, 5 hours, 12 minutes"
	var out bytes.Buffer
	env := offEnv()
	ty := uitest.NewTypist(&out, "1", "0")
	n := 0
	err := Run(context.Background(), Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		System: hostWatch{h, func() {
			n++
			if n == 2 {
				h.add(header.UptimeFile, "277980.37 553000.12\n")
			}
		}}, Facts: h.facts, Now: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s := out.String(); !strings.Contains(s, first) || !strings.Contains(s, "Uptime               3 days, 5 hours, 13 minutes") || strings.Contains(s, "\x1b") {
		t.Errorf("%s", s)
	}
}

// hostWatch calls each before it reads /proc/uptime.
type hostWatch struct {
	*testHost
	each func()
}

func (w hostWatch) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if name == header.UptimeFile {
		w.each()
	}
	return w.testHost.ReadFile(ctx, name)
}

// TestNavigation: 0 returns, q quits, the end of the input aborts; a line the
// prompt refuses asks again.
func TestNavigation(t *testing.T) {
	for _, tt := range []struct {
		lines []string
		want  error
	}{{[]string{"0"}, nil}, {[]string{"q"}, nav.ErrQuit}, {nil, prompt.ErrEndOfInput}, {[]string{"1"}, prompt.ErrAborted}, {[]string{"2", "0"}, nil}} {
		p := play(hosts["debian13"](), tt.lines...)
		if !errors.Is(p.err, tt.want) || (tt.want == nil && p.err != nil) || p.unread != 0 {
			t.Errorf("%q: Run = %v, want %v; %d unread", tt.lines, p.err, tt.want, p.unread)
		}
	}
	if s := play(hosts["debian13"](), "2", "0").out; !strings.Contains(s, "Invalid option") {
		t.Errorf("%s", s)
	}
}

// TestRunNeeds: without a Prompter's In or Out or a System, Run and Print are
// refused, writing nothing.
func TestRunNeeds(t *testing.T) {
	h := hosts["debian13"]()
	env := offEnv()
	var out bytes.Buffer
	in := prompt.New(uitest.NewTypist(&out), false)
	for name, o := range map[string]Options{
		"no in":     {Prompter: nav.Prompter{Ctx: component.NewCtx(env), Out: &out}, System: h},
		"no out":    {Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: in}, System: h},
		"no system": {Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: in, Out: &out}},
	} {
		if err := Run(context.Background(), o); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, o := range map[string]PrintOptions{"no out": {Env: env, System: h}, "no system": {Env: env, Out: &out}} {
		if err := Print(context.Background(), o); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Print, %s: %v", name, err)
		}
	}
	if out.Len() != 0 || len(h.Calls()) != 0 {
		t.Errorf("wrote %q, called %v", out.String(), h.Calls())
	}
}

// TestPlanOnly: Options has no mode to differ by, as neither screen plans: plan
// only opens and reads as live (gallery-system-details.md §5, Made here).
func TestPlanOnly(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(Options{}), reflect.TypeOf(PrintOptions{})} {
		for i := range typ.NumField() {
			if name := typ.Field(i).Name; strings.Contains(strings.ToLower(name), "dry") || strings.Contains(strings.ToLower(name), "plan") {
				t.Errorf("%s has %s", typ, name)
			}
		}
	}
}

// TestPrint: hardn system-details. To a terminal it is the screen's report with no
// menu and no prompt, colour kept (cli-terminal). Off one it is plain, no escape
// sequence, ASCII glyphs, no clear and no prompt, whatever the environment says of
// its width or colour (cli-pipe); as a user other than root, the rows from the report
// (cli-not-root); writing nothing.
func TestPrint(t *testing.T) {
	for name, mk := range hosts {
		h := mk()
		on, off := printed(t, h, terminalEnv()), printed(t, h, offEnv())
		for _, s := range []string{on, off} {
			if strings.Contains(s, "Enter selection") || strings.Contains(s, "Refresh") || strings.Contains(s, "\x1b[2J") || !strings.HasSuffix(s, "\n") {
				t.Errorf("%s: %q", name, s)
			}
		}
		if !strings.Contains(on, "\x1b[") {
			t.Errorf("%s: no colour on a terminal", name)
		}
		if strings.Contains(off, "\x1b") || !strings.HasPrefix(off, "# System Details ####") {
			t.Errorf("%s: off a terminal: %q", name, off)
		}
		assertOnlyReads(t, name, h)
		if got := strip(on); strings.Count(got, "\n") != strings.Count(off, "\n") {
			t.Errorf("%s: a terminal's report %d lines, off one %d", name, strings.Count(got, "\n"), strings.Count(off, "\n"))
		}
	}
	// A pipe with --color=always or COLUMNS set is still the plain form.
	env := term.Resolve(term.Options{Color: term.ColorAlways}, term.Facts{Width: 40, Environ: []string{"COLORTERM=truecolor", "LANG=C.UTF-8"}})
	h := hosts["debian13"]()
	if got, want := printed(t, h, env), printed(t, hosts["debian13"](), offEnv()); got != want {
		t.Errorf("off a terminal differs by the environment:\n%s\n--\n%s", got, want)
	}
	// As alice: the rules refused, each named; a user's row not known.
	asUser := hosts["debian13"]()
	asUser.add(details.SelfStatusFile, userStatus)
	var out bytes.Buffer
	if err := Print(context.Background(), PrintOptions{Out: &out, Env: offEnv(), System: refusing{asUser}, Facts: asUser.facts, Now: clock}); err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"[?] alice                Not known", "Not root: /etc/sudoers, /etc/sudoers.d not read"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("as alice, no %q:\n%s", w, out.String())
		}
	}
	// A failed write is said.
	if err := Print(context.Background(), PrintOptions{Out: failing{}, Env: offEnv(), System: hosts["debian13"](), Facts: hosts["debian13"]().facts}); err == nil || !strings.Contains(err.Error(), "write") {
		t.Errorf("a failed write: %v", err)
	}
}

// failing is a writer that fails.
type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// refusing is a host as a user other than root reads it: the rules refuse them.
type refusing struct{ *testHost }

func (s refusing) refusal(name string) error {
	if slices.Contains([]string{sudoers.MainConfig, sudoers.DropInDir, sudoers.DoasConfig, sudoers.DoasDir}, name) {
		return &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return nil
}

func (s refusing) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if err := s.refusal(name); err != nil {
		return nil, err
	}
	return s.testHost.ReadFile(ctx, name)
}

func (s refusing) ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error) {
	if err := s.refusal(name); err != nil {
		return nil, nil, err
	}
	return s.testHost.ReadFileNoFollow(ctx, name)
}

func (s refusing) ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error) {
	if err := s.refusal(name); err != nil {
		return nil, err
	}
	return s.testHost.ReadDir(ctx, name)
}

// TestLongValue: a value too long for its row is cut on a terminal, on the screen and
// from Print, and whole off one (gallery-system-details.md §5, Made here; DEV-067).
func TestLongValue(t *testing.T) {
	const cut = "Intel(R) Xeon(R) Gold 6338 CPU @ 2.0…"
	if s := strip(playIn(hosts["pve9"](), terminalEnv(), "0").out); strings.Contains(s, serverCPU) || !strings.Contains(s, cut) {
		t.Errorf("the screen:\n%s", s)
	}
	if s := strip(printed(t, hosts["pve9"](), terminalEnv())); strings.Contains(s, serverCPU) || !strings.Contains(s, cut) {
		t.Errorf("Print to a terminal:\n%s", s)
	}
	if s := printed(t, hosts["pve9"](), offEnv()); !strings.Contains(s, "   -  Model                "+serverCPU+"\n") {
		t.Errorf("Print off a terminal:\n%s", s)
	}
}

// TestSafe: what the host holds reaches no terminal raw, on the screen or from Print,
// on or off a terminal: a value with U+202E and an OSC shows them as text, in a value,
// a user's name and an interface's, a mount's source and type, a pool's name and health
// (readers-system-details.md §12, decision 32).
func TestSafe(t *testing.T) {
	const bad = "\u202e\x1b]0;a title\x07\x1b[2J"
	const shown = "<U+202E>^[]0;a title^G^[[2J"
	const field, fieldShown = "\u202e", "<U+202E>" // short: a mount's row is cut on a terminal
	mk := func() *testHost {
		h := hosts["debian13"]()
		h.add(header.KernelReleaseFile, "6.12"+bad+"\n")
		h.add(details.CPUInfoFile, cpuinfo("cpu"+bad, 4, 1))
		h.m.addrs = ipLine(2, "eth0\u202e", "inet", "192.0.2.20/24")
		h.add("/etc/passwd", passwd+"bob\u202e:x:1001:1001::/home/bob:/bin/bash\n")
		h.add(detect.SelfMounts, "/dev/vda1"+field+" / ext4"+field+" rw,relatime 0 0\n")
		return h
	}
	check := func(name, s string, wants ...string) {
		t.Helper()
		plain := strip(s)
		if strings.ContainsAny(plain, "\u202e\x1b\x07") {
			t.Errorf("%s: raw: %q", name, plain)
		}
		for _, w := range wants {
			if !strings.Contains(plain, w) {
				t.Errorf("%s: no %q in:\n%s", name, w, plain)
			}
		}
	}
	each := func(mk func() *testHost, wants ...string) {
		t.Helper()
		check("the screen", playIn(mk(), terminalEnv(), "0").out, wants...)
		check("the screen, plain", play(mk(), "0").out, wants...)
		check("Print to a terminal", printed(t, mk(), terminalEnv()), wants...)
		check("Print off a terminal", printed(t, mk(), offEnv()), wants...)
	}
	each(mk, "6.12"+shown, "cpu"+shown, "eth0<U+202E>", "bob<U+202E>", "/dev/vda1"+fieldShown+" ext4"+fieldShown)
	each(func() *testHost {
		h := pve("DEGRADED" + field)
		h.add(detect.SelfMounts, "rpool"+field+"/ROOT/pve-1 / zfs rw,relatime,xattr,noacl 0 0\n")
		return h
	}, "rpool"+fieldShown+"/ROOT/pve-1 zfs", "Pool rpool"+fieldShown, "DEGRADED"+fieldShown)
}

// rowsOf is section's rows from a report, as the view holds them.
func rowsOf(r details.Report, section string) []component.KeyValue {
	v := viewOf(r, now)
	switch section {
	case "OS":
		return v.OS
	case "Host":
		return v.Host
	case "Users":
		return v.Users
	case "CPU":
		return v.CPU
	case "Disk":
		return v.Disk
	case "Memory":
		return v.Memory
	}
	return v.Logins
}

// readOn reads h as the screen does.
func readOn(h *testHost) details.Report { return details.Read(context.Background(), h, h.facts) }

// The row tests below moved here from the gallery's mocks (unit (c)): every state of
// readers-system-details.md §8's table, in its exact words.

// TestSDLastLogin holds the rows to lslogins' one answer: root first, then the users,
// whatever order it answers in; a login with a host, a dotless one included, and with
// none; another year's; blanks. A listed name with no line reads not known and the
// others stay. A line for a name not asked, a garbled one or a failure says it could
// not read it, never no login, and a host with no lslogins says that.
func TestSDLastLogin(t *testing.T) {
	const at = "2026-10-20T11:42:05+00:00"
	cant := []component.KeyValue{{Key: "Can't read lslogins", State: component.ValueWarn}}
	none := component.KeyValue{Key: "root", Value: noLogin, State: component.ValueNone}
	alice := func(value string) []component.KeyValue {
		return []component.KeyValue{none, {Key: "alice", Value: value}}
	}
	aliceUnknown := component.KeyValue{Key: "alice", Value: notKnown, State: component.ValueUnknown}
	root := "root   \n"
	for _, tt := range []struct {
		name string
		res  hostfake.Response
		want []component.KeyValue
	}{
		{"a host", hostfake.Response{Stdout: root + "alice " + at + " pts/0 192.0.2.10\n"}, alice("Oct 20 11:42 from 192.0.2.10")},
		{"a dotless host", hostfake.Response{Stdout: root + "alice " + at + " pts/0 bastion\n"}, alice("Oct 20 11:42 from bastion")},
		{"no host", hostfake.Response{Stdout: root + "alice " + at + " tty1 \n"}, alice("Oct 20 11:42 on tty1")},
		{"another year", hostfake.Response{Stdout: root + "alice 2025-12-31T23:59:00+00:00 pts/0 192.0.2.10\n"}, alice("Dec 31 2025 from 192.0.2.10")},
		{"all blank", hostfake.Response{Stdout: root + "alice   \n"}, []component.KeyValue{none, {Key: "alice", Value: noLogin, State: component.ValueNone}}},
		{"another order", hostfake.Response{Stdout: "alice " + at + " pts/0 192.0.2.10\n" + root}, alice("Oct 20 11:42 from 192.0.2.10")},
		{"a name with no line", hostfake.Response{Stdout: root}, []component.KeyValue{none, aliceUnknown}},
		{"no line at all", hostfake.Response{}, []component.KeyValue{{Key: "root", Value: notKnown, State: component.ValueUnknown}, aliceUnknown}},
		{"a name not asked", hostfake.Response{Stdout: root + "alice-long " + at + " pts/0 192.0.2.10\n"}, cant},
		{"garbled time", hostfake.Response{Stdout: root + "alice yesterday pts/0 192.0.2.10\n"}, cant},
		{"too few fields", hostfake.Response{Stdout: root + "alice " + at + " pts/0\n"}, cant},
		{"a terminal and no time", hostfake.Response{Stdout: root + "alice  pts/0 \n"}, cant},
		{"a blank line", hostfake.Response{Stdout: root + "\n" + "alice   \n" + "\n"}, cant},
		{"a failure", hostfake.Response{Stderr: "lslogins: cannot open\n", ExitCode: 1}, cant},
		{"not found", hostfake.Response{Err: host.ErrNotFound}, []component.KeyValue{{Key: rowLogins, Value: noLslogins, State: component.ValueUnknown}}},
	} {
		h := hosts["debian13"]()
		res := tt.res
		h.m.lslogins = &res
		if got := rowsOf(readOn(h), "Logins"); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %+v, want %+v", tt.name, got, tt.want)
		}
	}
	session := "Oct 20 11:42 from 192.0.2.10"
	for name, want := range map[string][]component.KeyValue{
		"debian13":      alice(session),
		"ubuntu2604":    alice(session),
		"unreadable":    alice(session),
		"alpine323":     {{Key: rowLogins, Value: noLslogins, State: component.ValueUnknown}},
		"pve9":          {{Key: "root", Value: session}},
		"zfs-degraded":  {{Key: "root", Value: session}},
		"no-logins":     {none, {Key: "alice", Value: noLogin, State: component.ValueNone}},
		"login-console": alice("Oct 20 11:42 on tty1"),
	} {
		if got := rowsOf(readOn(hosts[name]()), "Logins"); !slices.Equal(got, want) {
			t.Errorf("%s: %+v, want %+v", name, got, want)
		}
	}
	// A blank with no source to back it is not a fact (decision 3 (a)): with wtmp
	// gone, and no lastlog here, each blank reads not known, the login stays.
	h := hosts["debian13"]()
	if err := h.Remove(context.Background(), details.WtmpFile); err != nil {
		t.Fatal(err)
	}
	want := []component.KeyValue{{Key: "root", Value: notKnown, State: component.ValueUnknown}, {Key: "alice", Value: session}}
	if got := rowsOf(readOn(h), "Logins"); !slices.Equal(got, want) {
		t.Errorf("no source: %+v, want %+v", got, want)
	}
}

// TestSDLsloginsCall: every draw asks lslogins once, all the names in the one call, as
// its cost is per call.
func TestSDLsloginsCall(t *testing.T) {
	h := many()
	play(h, "0")
	var calls [][]string
	for _, c := range h.Calls() {
		if lslogins(c) {
			calls = append(calls, c.Argv)
		}
	}
	const names = "root,alice,bob,carol,dave,erin,frank,grace,heidi,ivan,judy,mallory,niaj,olivia,peggy"
	if want := append(append([]string{"lslogins"}, lsloginsArgs...), names); len(calls) != 1 || !slices.Equal(calls[0], want) {
		t.Errorf("lslogins called %q, want once, %q", calls, want)
	}
}

// TestSDBlankCPU: where /proc/cpuinfo has no model name or physical id, as on arm64
// [I, not run], the row says so and no socket count is made up.
func TestSDBlankCPU(t *testing.T) {
	got := rowsOf(details.Report{CPU: details.CPU{CPUs: 4}}, "CPU")
	if got[0].Value != notKnown || got[0].State != component.ValueUnknown || got[1].Value != "4" {
		t.Errorf("%+v", got)
	}
}

// TestSDNotRootNamesTheRules: as a user other than root the one row names each of the
// tool's files refused, and a user's row reads not known, never a group's guess; the
// login section, which a user reads the same (vm-runs/s1/02-login.log), stays.
func TestSDNotRootNamesTheRules(t *testing.T) {
	for _, tt := range []struct{ host, want string }{
		{"debian13", "Not root: /etc/sudoers, /etc/sudoers.d not read"},
		{"alpine323", "Not root: /etc/doas.conf, /etc/doas.d not read"},
	} {
		h := hosts[tt.host]()
		h.add(details.SelfStatusFile, userStatus)
		r := details.Read(context.Background(), refusing{h}, h.facts)
		want := []component.KeyValue{{Key: "alice", Value: notKnown, State: component.ValueUnknown}, {Key: tt.want, State: component.ValueWarn}}
		if rows := rowsOf(r, "Users"); !slices.Equal(rows, want) {
			t.Errorf("%s: %+v, want %+v", tt.host, rows, want)
		}
		if r.Users.Root != detect.No {
			t.Errorf("%s: Root = %v", tt.host, r.Users.Root)
		}
	}
}

// TestSDRootRefusedSaysCantRead: a refusal as root is not "Not root": each rule file
// says it cannot be read, as a root process can be refused one (a root-squashed path).
func TestSDRootRefusedSaysCantRead(t *testing.T) {
	h := hosts["debian13"]()
	h.add(details.SelfStatusFile, "Name:\thardn\nUid:\t0\t0\t0\t0\nGid:\t0\t0\t0\t0\n")
	r := details.Read(context.Background(), refusing{h}, h.facts)
	if r.Users.Root != detect.Yes {
		t.Fatalf("Root = %v", r.Users.Root)
	}
	want := []component.KeyValue{
		{Key: "alice", Value: notKnown, State: component.ValueUnknown},
		{Key: "Can't read /etc/sudoers", State: component.ValueWarn},
		{Key: "Can't read /etc/sudoers.d", State: component.ValueWarn},
	}
	if rows := rowsOf(r, "Users"); !slices.Equal(rows, want) {
		t.Errorf("%+v, want %+v", rows, want)
	}
}

// TestSDUnreadableKeepsTheRest: one section not read leaves the other six, where
// origin/main loses the whole report (details.go:91-117): the section says which file.
func TestSDUnreadableKeepsTheRest(t *testing.T) {
	r := readOn(hosts["unreadable"]())
	if got := rowsOf(r, "Memory"); len(got) != 1 || !strings.Contains(got[0].Key, "/proc/meminfo") {
		t.Errorf("Memory's rows: %+v", got)
	}
	for _, s := range []string{"OS", "Host", "Users", "CPU", "Disk", "Logins"} {
		if len(rowsOf(r, s)) == 0 {
			t.Errorf("%s has no row", s)
		}
	}
}

// TestSDRows: a fact keeps its section's others, and what is read but not known says
// so: each row from the report, readers-system-details.md §8's table.
func TestSDRows(t *testing.T) {
	cant := func(source string) component.KeyValue {
		return component.KeyValue{Key: "Can't read " + source, State: component.ValueWarn}
	}
	unread := func(source string) []details.Unread { return []details.Unread{{Source: source}} }
	used := component.KeyValue{Key: rowUsed, Value: "1.0 of 2.0 GiB (50%)"}
	for _, tt := range []struct {
		name, section string
		report        details.Report
		want          []component.KeyValue
	}{
		{"the kernel not read", "OS", details.Report{OS: details.OSKernel{Unread: unread("/proc/sys/kernel/osrelease")}}, []component.KeyValue{cant("/proc/sys/kernel/osrelease")}},
		{"ip failed, the hostname stays", "Host", details.Report{Host: details.HostIPs{Hostname: "web-01", Unread: unread(details.SourceIP)}},
			[]component.KeyValue{{Key: rowHostname, Value: "web-01"}, cant(details.SourceIP)}},
		{"ip answered none", "Host", details.Report{Host: details.HostIPs{Hostname: "web-01", Addresses: []details.Address{}}},
			[]component.KeyValue{{Key: rowHostname, Value: "web-01"}, {Key: noAddresses, State: component.ValueNone}}},
		{"ip answered only lines not read", "Host", details.Report{Host: details.HostIPs{Hostname: "web-01", Addresses: []details.Address{}, Unread: unread(details.SourceIP)}},
			[]component.KeyValue{{Key: rowHostname, Value: "web-01"}, cant(details.SourceIP)}},
		{"ip answered none, the hostname not read", "Host", details.Report{Host: details.HostIPs{Addresses: []details.Address{}, Unread: unread("/proc/sys/kernel/hostname")}},
			[]component.KeyValue{{Key: noAddresses, State: component.ValueNone}, cant("/proc/sys/kernel/hostname")}},
		{"no account file", "Users", details.Report{Users: details.Users{Unread: unread("/etc/passwd")}}, []component.KeyValue{cant("/etc/passwd")}},
		{"root alone", "Users", details.Report{Users: details.Users{List: []details.User{}}}, []component.KeyValue{{Key: noUsers, State: component.ValueNone}}},
		{"as root, a rule the tool rejects", "Users", details.Report{Users: details.Users{List: []details.User{{Name: "alice"}}, Root: detect.Yes, Unread: unread("/etc/sudoers")}},
			[]component.KeyValue{{Key: "alice", Value: noSudo}, cant("/etc/sudoers")}},
		{"no grant on doas", "Users", details.Report{Users: details.Users{List: []details.User{{Name: "alice"}}, Root: detect.Yes, Tool: "doas"}},
			[]component.KeyValue{{Key: "alice", Value: noDoas}}},
		{"the load not read, the model stays", "CPU", details.Report{CPU: details.CPU{Model: "m", CPUs: 2, Sockets: 2, Unread: unread(details.LoadAvgFile)}},
			[]component.KeyValue{{Key: rowModel, Value: "m"}, {Key: rowCPUs, Value: "2, 2 sockets"}, cant(details.LoadAvgFile)}},
		{"cpuinfo not read, the load stays", "CPU", details.Report{CPU: details.CPU{Load: [3]string{"1", "2", "3"}, Unread: unread(details.CPUInfoFile)}},
			[]component.KeyValue{{Key: rowLoad, Value: "1, 2, 3"}, cant(details.CPUInfoFile)}},
		{"df failed, the mount stays", "Disk", details.Report{Disk: details.Disk{Source: "/dev/vda1", Type: "ext4", Unread: unread(details.SourceDF)}},
			[]component.KeyValue{{Key: rowRoot, Value: "/dev/vda1 ext4"}, cant(details.SourceDF)}},
		{"no zpool, df's figures", "Disk", details.Report{Disk: details.Disk{Source: "rpool/ROOT/pve-1", Type: "zfs", Used: 1 << 30, Total: 2 << 30, Known: true, Pool: "rpool"}},
			[]component.KeyValue{{Key: rowRoot, Value: "rpool/ROOT/pve-1 zfs"}, used, {Key: "Pool rpool", Value: notKnown, State: component.ValueUnknown}}},
		{"a pool not ONLINE", "Disk", details.Report{Disk: details.Disk{Source: "r/d", Type: "zfs", Used: 1 << 30, Total: 2 << 30, Known: true, Pool: "r", Health: "FAULTED"}},
			[]component.KeyValue{{Key: rowRoot, Value: "r/d zfs"}, used, {Key: "Pool r", Value: "FAULTED", State: component.ValueWarn}}},
		{"no MemAvailable", "Memory", details.Report{Memory: details.Memory{Total: 2 << 30}}, []component.KeyValue{{Key: rowUsed, Value: notKnown, State: component.ValueUnknown}}},
		{"meminfo", "Memory", details.Report{Memory: details.Memory{Total: 2 << 30, Available: 1 << 30, AvailableKnown: true}}, []component.KeyValue{used}},
		{"lslogins unread", "Logins", details.Report{Logins: details.Logins{State: details.LoginsUnread, Unread: unread(details.SourceLslogins)}}, []component.KeyValue{cant(details.SourceLslogins)}},
		{"lslogins not there", "Logins", details.Report{Logins: details.Logins{State: details.LoginsNoTool}}, []component.KeyValue{{Key: rowLogins, Value: noLslogins, State: component.ValueUnknown}}},
	} {
		if got := rowsOf(tt.report, tt.section); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

// TestSDZFSDatasetIsRead: the reader asks zfs about the dataset mounted on /, never
// origin/main's hard-coded zroot/ROOT/os (details.go:85).
func TestSDZFSDatasetIsRead(t *testing.T) {
	h := hosts["pve9"]()
	play(h, "0")
	asked := false
	for _, c := range h.Calls() {
		if c.Op == "run" && c.Argv[0] == "zfs" {
			if c.Argv[len(c.Argv)-1] != pveDataset {
				t.Errorf("zfs asked about %q", c.Argv[len(c.Argv)-1])
			}
			asked = true
		}
	}
	if !asked {
		t.Error("zfs was not asked")
	}
}

// TestSDReadsAddrs: the rows take the forms the VMs print: Debian's and Ubuntu's with a
// metric, Alpine's with noprefixroute and none (vm-runs/s1/01-tools.log); a form it
// does not know is not read.
func TestSDReadsAddrs(t *testing.T) {
	want := component.KeyValue{Key: "eth0", Value: "192.0.2.20/24"}
	for name, addrs := range map[string]string{
		"debian13":  "2: eth0    inet 192.0.2.20/24 metric 100 brd 192.0.2.255 scope global dynamic eth0\\       valid_lft 7104sec preferred_lft 7104sec\n",
		"alpine323": "2: eth0    inet 192.0.2.20/24 brd 192.0.2.255 scope global dynamic noprefixroute eth0\\       valid_lft 5235sec preferred_lft 4411sec\n",
	} {
		h := hosts[name]()
		h.m.addrs = addrs
		if got := rowsOf(readOn(h), "Host"); len(got) != 2 || got[1] != want {
			t.Errorf("%s: %+v", name, got)
		}
	}
	h := hosts["debian13"]()
	h.m.addrs = "nonsense\n"
	if got := rowsOf(readOn(h), "Host"); len(got) != 2 || got[1].Key != "Can't read "+details.SourceIP {
		t.Errorf("a form not read says so, and not that there are none: %+v", got)
	}
}

package details_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/details"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// Nothing here runs a real command or reads the machine's own files: every host
// is hostfake's. The paths are written out rather than taken from the package,
// so that a wrong path in the code fails here. Each line is typed from the log
// named beside it, in
// _support/hardn-audit-2026-10-16/vm-runs/s1/ (vm-runs below), with synthetic
// values: alice, web-01, RFC 5737's and RFC 3849's addresses.
const (
	hostnameFile = "/proc/sys/kernel/hostname"
	uptimeFile   = "/proc/uptime"
	releaseFile  = "/proc/sys/kernel/osrelease"
	statusFile   = "/proc/self/status"
	cpuFile      = "/proc/cpuinfo"
	loadFile     = "/proc/loadavg"
	memFile      = "/proc/meminfo"
	mountsFile   = "/proc/self/mounts"
	passwdFile   = "/etc/passwd"
	groupFile    = "/etc/group"
	sudoersFile  = "/etc/sudoers"
	sudoersDir   = "/etc/sudoers.d"
	doasFile     = "/etc/doas.conf"
	doasDir      = "/etc/doas.d"
	wtmpFile     = "/var/log/wtmp"
	lastlogFile  = "/var/log/lastlog"
	lastlog2File = "/var/lib/lastlog/lastlog2.db"
)

// The real forms, typed from the logs.
const (
	ipDebian = "2: eth0    inet 192.0.2.135/24 metric 100 brd 192.0.2.255 scope global dynamic eth0\\       valid_lft 7104sec preferred_lft 7104sec\n"    // 01-tools.log:50, :115
	ipAlpine = "2: eth0    inet 192.0.2.138/24 brd 192.0.2.255 scope global dynamic noprefixroute eth0\\       valid_lft 5235sec preferred_lft 4411sec\n" // 01-tools.log:178
	ipInet6  = "2: eth0    inet6 2001:db8::20/64 scope global dynamic noprefixroute \\       valid_lft 86400sec preferred_lft 14400sec\n"                 // [I]: no VM has a global one

	dfHeader = "Filesystem     1024-blocks    Used Available Capacity Mounted on\n"                                                                       // [I]: every VM's log cut it off
	dfDebian = dfHeader + "/dev/sda1         10113424 1409984   8242784      15% /\n"                                                                     // 01-tools.log:51
	dfUbuntu = dfHeader + "/dev/sda1          8965896 3225916   5723596      37% /\n"                                                                     // 01-tools.log:116
	dfAlpine = "Filesystem           1024-blocks    Used Available Capacity Mounted on\n" + "/dev/sda               9819873    209418   9184786   2% /\n" // 01-tools.log:179

	mountsDebian = "sysfs /sys sysfs rw,nosuid 0 0\n/dev/sda1 / ext4 rw,relatime,discard,errors=remount-ro 0 0\n" // 01-tools.log:52
	mountsAlpine = "/dev/sda / ext4 rw,noatime 0 0\n"                                                             // 01-tools.log:180

	cpuAmd64 = "processor\t: 0\nvendor_id\t: GenuineIntel\nmodel name\t: QEMU Virtual CPU version 2.5+\nphysical id\t: 0\n\nprocessor\t: 1\nmodel name\t: QEMU Virtual CPU version 2.5+\nphysical id\t: 0\n" // 01-tools.log:53-56, the lines the VMs' grep kept
	// cpuArm64 is arm64's shape: no model name and no physical id [I, not run].
	cpuArm64 = "processor\t: 0\nBogoMIPS\t: 48.00\nFeatures\t: fp asimd evtstrm\nCPU implementer\t: 0x41\n\nprocessor\t: 1\nBogoMIPS\t: 48.00\n"

	memFull = "MemTotal:        2021528 kB\nMemFree:          725596 kB\nMemAvailable:    1551444 kB\nBuffers:           10000 kB\n" // 01-tools.log:57-59
	memOld  = "MemTotal:        2021528 kB\nMemFree:          725596 kB\n"

	loginAlice = "alice 2026-10-20T11:42:05+00:00 pts/0 192.0.2.10\n" // 02-login.log:6, 08-lslogins.log:24
	blankRoot  = "root   \n"                                          // 02-login.log:12, 08-lslogins.log:30
)

// lsloginsArgv is decision 3 (a)'s one call for names.
func lsloginsArgv(names string) []string {
	return []string{"lslogins", "--time-format=iso", "--noheadings", "--raw", "--output=USER,LAST-LOGIN,LAST-TTY,LAST-HOSTNAME", "-l", names}
}

// debianFacts and alpineFacts are detect's facts for those hosts.
func debianFacts() detect.Facts {
	return detect.Facts{
		OS:        detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie", PrettyName: "Debian GNU/Linux 13 (trixie)"},
		Platform:  detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Proxmox:   detect.Proxmox{Installed: detect.No},
		Elevation: detect.Elevation{Sudo: detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}, SudoFlavor: detect.SudoOriginal, Doas: detect.Tool{Present: detect.No}},
	}
}

func alpineFacts() detect.Facts {
	return detect.Facts{
		OS:        detect.OSRelease{ID: "alpine", VersionID: "3.23.6", PrettyName: "Alpine Linux v3.23"},
		Platform:  detect.Platform{Distro: detect.DistroAlpine, Version: "3.23", Support: detect.Supported},
		Proxmox:   detect.Proxmox{Installed: detect.No},
		Elevation: detect.Elevation{Sudo: detect.Tool{Present: detect.No}, Doas: detect.Tool{Present: detect.Yes, Path: "/usr/bin/doas"}},
	}
}

// fakeHost is the host each test starts from and changes: Debian 13 as root,
// alice in the sudo group, wtmp and lastlog holding records, an answer to each
// command. It adds the setuid bit Stat cannot show (hostfake has none) and the
// refusals a user other than root gets (hostfake acts as root).
type fakeHost struct {
	*hostfake.System
	refused map[string]error
	cmd     map[string]hostfake.Response // by argv joined with a space
}

func newHost(t *testing.T) *fakeHost {
	t.Helper()
	f := hostfake.New(host.Options{})
	h := &fakeHost{System: f, refused: map[string]error{}, cmd: map[string]hostfake.Response{}}
	for p, data := range map[string]string{
		hostnameFile:           "web-01\n",
		uptimeFile:             "278000.00 500000.00\n",
		releaseFile:            "6.12.113+deb13-cloud-amd64\n",
		statusFile:             "Name:\thardn\nUid:\t0\t0\t0\t0\nGid:\t0\t0\t0\t0\n",
		cpuFile:                cpuAmd64,
		loadFile:               "0.12 0.08 0.03 1/234 5678\n",
		memFile:                memFull,
		mountsFile:             mountsDebian,
		passwdFile:             "root:x:0:0:root:/root:/bin/bash\nsshd:x:109:65534::/run/sshd:/usr/sbin/nologin\nalice:x:1000:1000::/home/alice:/bin/bash\nnobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n",
		groupFile:              "root:x:0:\nsudo:x:27:alice\nalice:x:1000:\nnogroup:x:65534:\n",
		sudoersFile:            "root ALL=(ALL:ALL) ALL\n%sudo ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n",
		sudoersDir + "/README": "# a comment\n",
		wtmpFile:               "\x07\x00\x00\x00alice\x00pts/0\n",
		lastlogFile:            "\x01\x00\x00\x00pts/0\x00",
		"/usr/bin/sudo":        "",
	} {
		h.add(p, data)
	}
	h.AddDir(sudoersDir, 0o750, 0, 0)
	h.cmd["ip -o addr show scope global"] = hostfake.Response{Stdout: ipDebian}
	h.cmd["df -kP /"] = hostfake.Response{Stdout: dfDebian}
	h.cmd["visudo -c"] = hostfake.Response{Stdout: "/etc/sudoers: parsed OK\n"}
	h.cmd[strings.Join(lsloginsArgv("root,alice"), " ")] = hostfake.Response{Stdout: blankRoot + loginAlice}
	f.Handler = func(argv []string, _ []byte) hostfake.Response {
		if r, ok := h.cmd[strings.Join(argv, " ")]; ok {
			return r
		}
		return hostfake.Response{Err: fmt.Errorf("%s: %w", argv[0], hostfake.ErrUnscripted)}
	}
	return h
}

// add adds a file, or replaces one.
func (h *fakeHost) add(p, data string) { h.AddFile(p, []byte(data), 0o644, 0, 0) }

func (h *fakeHost) refuse(paths ...string) {
	for _, p := range paths {
		h.refused[p] = fs.ErrPermission
	}
}

func (h *fakeHost) err(op, name string) error {
	if err := h.refused[name]; err != nil {
		return &fs.PathError{Op: op, Path: name, Err: err}
	}
	return nil
}

func (h *fakeHost) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if err := h.err("open", name); err != nil {
		return nil, err
	}
	return h.System.ReadFile(ctx, name)
}

func (h *fakeHost) ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error) {
	if err := h.err("open", name); err != nil {
		return nil, nil, err
	}
	return h.System.ReadFileNoFollow(ctx, name)
}

func (h *fakeHost) ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error) {
	if err := h.err("open", name); err != nil {
		return nil, err
	}
	return h.System.ReadDir(ctx, name)
}

func (h *fakeHost) ScanLines(ctx context.Context, name string, yield func(host.Line) bool) (fs.FileInfo, error) {
	if err := h.err("open", name); err != nil {
		return nil, err
	}
	return h.System.ScanLines(ctx, name, yield)
}

func (h *fakeHost) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	fi, err := h.System.Stat(ctx, name)
	if err == nil && (name == "/usr/bin/sudo" || name == "/usr/bin/doas") {
		return setuid{fi}, nil
	}
	return fi, err
}

type setuid struct{ fs.FileInfo }

func (s setuid) Mode() fs.FileMode { return s.FileInfo.Mode() | fs.ModeSetuid }

// runs are the commands h was asked, each as one string.
func (h *fakeHost) runs() []string {
	var out []string
	for _, r := range h.Runs() {
		out = append(out, strings.Join(r, " "))
	}
	return out
}

// unreadOf is every section's Unread, by source, with the section's name.
func unreadOf(r details.Report) map[string][]string {
	out := map[string][]string{}
	add := func(name string, us []details.Unread) {
		for _, u := range us {
			out[name] = append(out[name], u.Source)
		}
	}
	add("os", r.OS.Unread)
	add("host", r.Host.Unread)
	add("users", r.Users.Unread)
	add("cpu", r.CPU.Unread)
	add("disk", r.Disk.Unread)
	add("memory", r.Memory.Unread)
	add("logins", r.Logins.Unread)
	return out
}

// wholeWith checks r has read all but the sections want names.
func wholeWith(t *testing.T, r details.Report, want map[string][]string) {
	t.Helper()
	if got := unreadOf(r); !reflect.DeepEqual(got, want) {
		t.Errorf("unread = %v, want %v", got, want)
	}
}

// TestRead: the whole report on Debian 13's forms, as root.
func TestRead(t *testing.T) {
	h := newHost(t)
	r := details.Read(context.Background(), h, debianFacts())
	wholeWith(t, r, map[string][]string{})
	if r.OS.OS.Name != "Debian" || r.OS.OS.Version != "13" || r.OS.OS.Codename != "trixie" {
		t.Errorf("OS = %+v", r.OS.OS)
	}
	if r.OS.Kernel != "6.12.113+deb13-cloud-amd64" || !r.OS.Uptime.Known || r.OS.Uptime.Duration != 278000*time.Second {
		t.Errorf("OS and Kernel = %+v", r.OS)
	}
	if want := []details.Address{{Interface: "eth0", Family: "inet", Address: "192.0.2.135/24"}}; r.Host.Hostname != "web-01" || !reflect.DeepEqual(r.Host.Addresses, want) {
		t.Errorf("Host = %+v", r.Host)
	}
	if len(r.Users.List) != 1 || r.Users.List[0].Name != "alice" || r.Users.Root != detect.Yes {
		t.Fatalf("Users = %+v", r.Users)
	}
	if tool, tri := r.Users.List[0].Admin(); tool != "sudo" || tri != detect.Yes {
		t.Errorf("alice's admin = %q %v", tool, tri)
	}
	if got, want := r.Users.List[0].Grants, []users.Grant{{Tool: "sudo", Grants: detect.Yes}, {Tool: "doas", Grants: detect.No}}; !slices.Equal(got, want) {
		t.Errorf("alice's grants = %v", got)
	}
	wantCPU := details.CPU{Model: "QEMU Virtual CPU version 2.5+", CPUs: 2, Sockets: 1, Load: [3]string{"0.12", "0.08", "0.03"}}
	if !reflect.DeepEqual(r.CPU, wantCPU) {
		t.Errorf("CPU = %+v", r.CPU)
	}
	wantDisk := details.Disk{Source: "/dev/sda1", Type: "ext4", Used: 1409984 * 1024, Total: 10113424 * 1024, Known: true}
	if !reflect.DeepEqual(r.Disk, wantDisk) {
		t.Errorf("Disk = %+v", r.Disk)
	}
	if used, ok := r.Memory.Used(); !ok || r.Memory.Total != 2021528*1024 || used != (2021528-1551444)*1024 {
		t.Errorf("Memory = %+v, used %d %v", r.Memory, used, ok)
	}
	at, err := time.Parse(time.RFC3339, "2026-10-20T11:42:05+00:00")
	if err != nil {
		t.Fatal(err)
	}
	wantLogins := []details.Login{{User: "root", State: details.LoginNone},
		{User: "alice", State: details.LoginAt, When: "2026-10-20T11:42:05+00:00", TTY: "pts/0", From: "192.0.2.10", At: at}}
	if r.Logins.State != details.LoginsRead || !reflect.DeepEqual(r.Logins.List, wantLogins) {
		t.Errorf("Logins = %+v", r.Logins)
	}
}

// TestOneSectionLeavesTheRest: each section read and unread, one source failing
// at a time, and the others kept whole (origin/main loses the report:
// gallery-system-details.md §2.3).
func TestOneSectionLeavesTheRest(t *testing.T) {
	failed := hostfake.Response{Stderr: "failed\n", ExitCode: 1}
	notFound := hostfake.Response{Err: fmt.Errorf("host: not in path: %w", host.ErrNotFound)}
	for _, tc := range []struct {
		name  string
		fail  func(h *fakeHost)
		want  map[string][]string
		check func(t *testing.T, r details.Report)
	}{
		{"kernel", func(h *fakeHost) { h.refuse(releaseFile) }, map[string][]string{"os": {releaseFile}}, func(t *testing.T, r details.Report) {
			if !r.OS.Uptime.Known || r.OS.OS.Name == "" {
				t.Errorf("the uptime or the OS went with the kernel: %+v", r.OS)
			}
		}},
		{"uptime", func(h *fakeHost) { h.refuse(uptimeFile) }, map[string][]string{"os": {uptimeFile}}, func(t *testing.T, r details.Report) {
			if r.OS.Kernel == "" {
				t.Errorf("the kernel went with the uptime: %+v", r.OS)
			}
		}},
		{"hostname", func(h *fakeHost) { h.refuse(hostnameFile) }, map[string][]string{"host": {hostnameFile}}, func(t *testing.T, r details.Report) {
			if len(r.Host.Addresses) != 1 {
				t.Errorf("the addresses went with the hostname: %+v", r.Host)
			}
		}},
		{"ip fails", func(h *fakeHost) { h.cmd["ip -o addr show scope global"] = failed }, map[string][]string{"host": {details.SourceIP}}, func(t *testing.T, r details.Report) {
			if r.Host.Hostname != "web-01" || r.Host.Addresses != nil {
				t.Errorf("Host = %+v", r.Host)
			}
		}},
		{"ip not there", func(h *fakeHost) { h.cmd["ip -o addr show scope global"] = notFound }, map[string][]string{"host": {details.SourceIP}}, nil},
		{"passwd", func(h *fakeHost) { h.refuse(passwdFile) }, map[string][]string{"users": {passwdFile}}, func(t *testing.T, r details.Report) {
			if r.Users.List != nil {
				t.Errorf("Users = %+v", r.Users)
			}
			if len(r.Logins.List) != 1 || r.Logins.List[0].User != "root" {
				t.Errorf("root's login went with the users: %+v", r.Logins)
			}
		}},
		{"cpuinfo", func(h *fakeHost) { h.refuse(cpuFile) }, map[string][]string{"cpu": {cpuFile}}, func(t *testing.T, r details.Report) {
			if r.CPU.Load[0] != "0.12" || r.CPU.CPUs != 0 {
				t.Errorf("CPU = %+v", r.CPU)
			}
		}},
		{"loadavg", func(h *fakeHost) { h.refuse(loadFile) }, map[string][]string{"cpu": {loadFile}}, func(t *testing.T, r details.Report) {
			if r.CPU.CPUs != 2 || r.CPU.Load[0] != "" {
				t.Errorf("CPU = %+v", r.CPU)
			}
		}},
		{"mounts", func(h *fakeHost) { h.refuse(mountsFile) }, map[string][]string{"disk": {mountsFile}}, func(t *testing.T, r details.Report) {
			if !r.Disk.Known || r.Disk.Source != "" {
				t.Errorf("Disk = %+v", r.Disk)
			}
		}},
		{"df", func(h *fakeHost) { h.cmd["df -kP /"] = failed }, map[string][]string{"disk": {details.SourceDF}}, func(t *testing.T, r details.Report) {
			if r.Disk.Known || r.Disk.Source != "/dev/sda1" {
				t.Errorf("Disk = %+v", r.Disk)
			}
		}},
		{"meminfo", func(h *fakeHost) { h.refuse(memFile) }, map[string][]string{"memory": {memFile}}, nil},
		{"lslogins fails", func(h *fakeHost) {
			h.cmd[strings.Join(lsloginsArgv("root,alice"), " ")] = failed
		}, map[string][]string{"logins": {details.SourceLslogins}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			tc.fail(h)
			if tc.name == "passwd" {
				h.cmd[strings.Join(lsloginsArgv("root"), " ")] = hostfake.Response{Stdout: blankRoot}
			}
			r := details.Read(context.Background(), h, debianFacts())
			wholeWith(t, r, tc.want)
			if tc.check != nil {
				tc.check(t, r)
			}
		})
	}
	t.Run("os-release", func(t *testing.T) {
		f := debianFacts()
		f.OS = detect.OSRelease{Err: fs.ErrPermission}
		r := details.Read(context.Background(), newHost(t), f)
		wholeWith(t, r, map[string][]string{"os": {"/etc/os-release"}})
		if !errors.Is(r.OS.Unread[0].Err, fs.ErrPermission) {
			t.Errorf("why = %v", r.OS.Unread[0].Err)
		}
	})
}

// TestAddresses: both real shapes, IPv4 and IPv6, none, and a line in no form
// known kept beside what was read.
func TestAddresses(t *testing.T) {
	addr := func(a, f string) details.Address { return details.Address{Interface: "eth0", Family: f, Address: a} }
	for _, tc := range []struct {
		name, out string
		want      []details.Address
		unread    bool
	}{
		{"Debian and Ubuntu, metric", ipDebian, []details.Address{addr("192.0.2.135/24", "inet")}, false},
		{"Alpine, noprefixroute", ipAlpine, []details.Address{addr("192.0.2.138/24", "inet")}, false},
		{"both families [I]", ipDebian + ipInet6, []details.Address{addr("192.0.2.135/24", "inet"), addr("2001:db8::20/64", "inet6")}, false},
		{"none", "", []details.Address{}, false},
		{"a line in no form known", "not a line\n" + ipDebian, []details.Address{addr("192.0.2.135/24", "inet")}, true},
		{"not an address", "2: eth0    inet nonsense brd 192.0.2.255\n", []details.Address{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			h.cmd["ip -o addr show scope global"] = hostfake.Response{Stdout: tc.out}
			r := details.Read(context.Background(), h, debianFacts())
			if !reflect.DeepEqual(r.Host.Addresses, tc.want) {
				t.Errorf("addresses = %#v", r.Host.Addresses)
			}
			if got := len(r.Host.Unread) > 0; got != tc.unread {
				t.Errorf("unread = %v", r.Host.Unread)
			}
			if tc.unread && !errors.Is(r.Host.Unread[0].Err, detect.ErrUnrecognised) {
				t.Errorf("why = %v", r.Host.Unread[0].Err)
			}
		})
	}
}

// TestUsers: who is listed, and what lets each administer: the rules decide, so
// a rule such as cloud-init's counts where a group would miss it, and the group
// alone says nothing the rules do not.
func TestUsers(t *testing.T) {
	admin := func(t *testing.T, r details.Report, name, wantTool string, wantTri detect.Tri) {
		t.Helper()
		for _, u := range r.Users.List {
			if u.Name == name {
				if tool, tri := u.Admin(); tool != wantTool || tri != wantTri {
					t.Errorf("%s: %q %v, want %q %v", name, tool, tri, wantTool, wantTri)
				}
				return
			}
		}
		t.Errorf("%s is not listed: %+v", name, r.Users.List)
	}
	ctx := context.Background()
	t.Run("cloud-init's rule, in no group", func(t *testing.T) {
		h := newHost(t)
		h.add(groupFile, "root:x:0:\nsudo:x:27:\nalice:x:1000:\n")
		h.add(sudoersDir+"/90-cloud-init-users", "alice ALL=(ALL) NOPASSWD:ALL\n")
		admin(t, details.Read(ctx, h, debianFacts()), "alice", "sudo", detect.Yes)
	})
	t.Run("in the sudo group, and no rule names it", func(t *testing.T) {
		h := newHost(t)
		h.add(sudoersFile, "root ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n")
		admin(t, details.Read(ctx, h, debianFacts()), "alice", "", detect.No)
	})
	t.Run("a rule in a form not read", func(t *testing.T) {
		h := newHost(t)
		h.add(sudoersDir+"/90-web", "alice ALL=(ALL) /usr/bin/systemctl\n")
		admin(t, details.Read(ctx, h, debianFacts()), "alice", "", detect.Unknown)
	})
	t.Run("doas", func(t *testing.T) {
		h := newHost(t)
		h.add(groupFile, "root:x:0:\nwheel:x:10:alice\nalice:x:1000:\n")
		h.add(doasFile, "permit persist :wheel\n")
		h.AddDir(doasDir, 0o750, 0, 0)
		h.add("/usr/bin/doas", "")
		h.cmd["doas -C /etc/doas.conf"] = hostfake.Response{}
		h.cmd["doas -C /etc/doas.d"] = hostfake.Response{}
		admin(t, details.Read(ctx, h, alpineFacts()), "alice", "doas", detect.Yes)
	})
	t.Run("uid 1000 and up, but nobody and nfsnobody, each name once", func(t *testing.T) {
		h := newHost(t)
		h.add(passwdFile, "root:x:0:0::/root:/bin/bash\nsshd:x:999:999::/:/bin/false\nalice:x:1000:1000::/home/alice:/bin/bash\nalice:x:1005:1000::/home/alice:/bin/bash\nsvc:x:1001:1001::/:/usr/sbin/nologin\nnobody:x:65534:65534::/:/bin/false\nnfsnobody:x:65535:65535::/:/bin/false\n")
		h.cmd[strings.Join(lsloginsArgv("root,alice,svc"), " ")] = hostfake.Response{Stdout: blankRoot}
		r := details.Read(ctx, h, debianFacts())
		var names []string
		for _, u := range r.Users.List {
			names = append(names, u.Name)
		}
		if want := []string{"alice", "svc"}; !slices.Equal(names, want) {
			t.Errorf("listed %v", names)
		}
	})
	t.Run("root alone reads no rule", func(t *testing.T) {
		h := newHost(t)
		h.add(passwdFile, "root:x:0:0::/root:/bin/bash\n")
		h.cmd[strings.Join(lsloginsArgv("root"), " ")] = hostfake.Response{Stdout: blankRoot}
		r := details.Read(ctx, h, debianFacts())
		if r.Users.List == nil || len(r.Users.List) != 0 || len(r.Users.Unread) != 0 {
			t.Errorf("Users = %+v", r.Users)
		}
		if slices.Contains(h.runs(), "visudo -c") {
			t.Errorf("ran %v", h.runs())
		}
	})
}

// TestNotRoot: as a user other than root, what could not be read is a fact the
// report carries, and each user's grant is not established. Modes as on the
// three VMs: /etc/sudoers.d drwxr-x---, Alpine's /etc/doas.conf 0640 and
// /etc/doas.d drwxr-x--- (01-tools.log:65, :130, :191-193); /etc/sudoers 0440
// [I]. How it knows it is not root is /proc/self/status's effective uid, [I] for
// the form: no VM printed it.
func TestNotRoot(t *testing.T) {
	ctx := context.Background()
	const asUser = "Name:\thardn\nUid:\t1000\t1000\t1000\t1000\nGid:\t1000\t1000\t1000\t1000\n"
	t.Run("sudo", func(t *testing.T) {
		h := newHost(t)
		h.add(statusFile, asUser)
		h.refuse(sudoersFile, sudoersDir)
		r := details.Read(ctx, h, debianFacts())
		if r.Users.Root != detect.No {
			t.Errorf("Root = %v", r.Users.Root)
		}
		if got, want := r.Users.NotRead(), []string{sudoersFile, sudoersDir}; !slices.Equal(got, want) {
			t.Errorf("not read: %v", got)
		}
		if tool, tri := r.Users.List[0].Admin(); tool != "" || tri != detect.Unknown {
			t.Errorf("alice's admin = %q %v", tool, tri)
		}
		if slices.Contains(h.runs(), "visudo -c") {
			t.Errorf("ran %v, where the read had already stopped", h.runs())
		}
		if r.Logins.State != details.LoginsRead {
			t.Errorf("Logins = %+v", r.Logins)
		}
	})
	t.Run("the drop-ins alone", func(t *testing.T) {
		h := newHost(t)
		h.add(statusFile, asUser)
		h.refuse(sudoersDir)
		r := details.Read(ctx, h, debianFacts())
		if got, want := r.Users.NotRead(), []string{sudoersDir}; !slices.Equal(got, want) {
			t.Errorf("not read: %v", got)
		}
	})
	t.Run("doas", func(t *testing.T) {
		h := newHost(t)
		h.add(statusFile, asUser)
		h.add(doasFile, "permit persist :wheel\n")
		h.AddDir(doasDir, 0o750, 0, 0)
		h.add("/usr/bin/doas", "")
		h.refuse(doasFile, doasDir)
		r := details.Read(ctx, h, alpineFacts())
		if got, want := r.Users.NotRead(), []string{doasFile, doasDir}; !slices.Equal(got, want) {
			t.Errorf("not read: %v", got)
		}
	})
	t.Run("root, refused", func(t *testing.T) {
		h := newHost(t)
		h.refuse(sudoersFile)
		r := details.Read(ctx, h, debianFacts())
		if r.Users.Root != detect.Yes || len(r.Users.NotRead()) != 1 {
			t.Errorf("Users = %+v", r.Users)
		}
	})
	for name, status := range map[string]string{"not read": "", "no Uid line": "Name:\thardn\n", "not numbers": "Uid:\ta\tb\tc\td\n"} {
		t.Run("whether root: "+name, func(t *testing.T) {
			h := newHost(t)
			h.add(statusFile, status)
			if status == "" {
				h.refuse(statusFile)
			}
			r := details.Read(ctx, h, debianFacts())
			if r.Users.Root != detect.Unknown || r.Users.RootErr == nil {
				t.Errorf("Root = %v, %v", r.Users.Root, r.Users.RootErr)
			}
			if len(r.Users.Unread) != 0 {
				t.Errorf("the rules went with it: %+v", r.Users.Unread)
			}
		})
	}
	t.Run("a rejected configuration names its file", func(t *testing.T) {
		h := newHost(t)
		h.cmd["visudo -c"] = hostfake.Response{Stderr: "syntax error\n", ExitCode: 1}
		r := details.Read(ctx, h, debianFacts())
		if got := unreadOf(r)["users"]; !slices.Equal(got, []string{sudoersFile}) {
			t.Errorf("unread %v", got)
		}
		if len(r.Users.NotRead()) != 0 {
			t.Errorf("not read: %v", r.Users.NotRead())
		}
	})
}

// TestCPU: arm64's shape [I, not run] makes up no model or socket count.
func TestCPU(t *testing.T) {
	for _, tc := range []struct {
		name, info, load string
		want             details.CPU
		unread           string
	}{
		{"amd64", cpuAmd64, "0.12 0.08 0.03 1/234 5678\n", details.CPU{Model: "QEMU Virtual CPU version 2.5+", CPUs: 2, Sockets: 1, Load: [3]string{"0.12", "0.08", "0.03"}}, ""},
		{"arm64 [I]", cpuArm64, "0.12 0.08 0.03 1/234 5678\n", details.CPU{CPUs: 2, Load: [3]string{"0.12", "0.08", "0.03"}}, ""},
		{"two sockets", "processor : 0\nphysical id : 0\nprocessor : 1\nphysical id : 1\n", "1.00 2.00 3.00 1/2 3\n", details.CPU{CPUs: 2, Sockets: 2, Load: [3]string{"1.00", "2.00", "3.00"}}, ""},
		{"no processor line", "vendor_id : x\nmodel name : y\nphysical id : 0\n", "1.00 2.00 3.00 1/2 3\n", details.CPU{Load: [3]string{"1.00", "2.00", "3.00"}}, cpuFile},
		{"load cut short", cpuArm64, "1.00 2.00\n", details.CPU{CPUs: 2}, loadFile},
		{"load not numbers", cpuArm64, "a b c 1/2 3\n", details.CPU{CPUs: 2}, loadFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			h.add(cpuFile, tc.info)
			h.add(loadFile, tc.load)
			r := details.Read(context.Background(), h, debianFacts()).CPU
			var src string
			if len(r.Unread) > 0 {
				src = r.Unread[0].Source
				if !errors.Is(r.Unread[0].Err, detect.ErrUnrecognised) {
					t.Errorf("why = %v", r.Unread[0].Err)
				}
			}
			r.Unread = nil
			if !reflect.DeepEqual(r, tc.want) || src != tc.unread {
				t.Errorf("CPU = %+v, unread %q", r, src)
			}
		})
	}
}

// TestDF: GNU's and BusyBox's lines, the header passed over.
func TestDF(t *testing.T) {
	for _, tc := range []struct {
		name, mounts, df string
		source           string
		used, total      uint64
	}{
		{"Debian 13", mountsDebian, dfDebian, "/dev/sda1", 1409984, 10113424},
		{"Ubuntu 26.04", mountsDebian, dfUbuntu, "/dev/sda1", 3225916, 8965896},
		{"Alpine 3.23", mountsAlpine, dfAlpine, "/dev/sda", 209418, 9819873},
		{"no header", mountsAlpine, "/dev/sda 9819873 209418 9184786 2% /\n", "/dev/sda", 209418, 9819873},
		{"the mount on top", "rootfs / rootfs rw 0 0\noverlay / overlay rw 0 0\n", dfDebian, "overlay", 1409984, 10113424},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			h.add(mountsFile, tc.mounts)
			h.cmd["df -kP /"] = hostfake.Response{Stdout: tc.df}
			r := details.Read(context.Background(), h, debianFacts()).Disk
			if r.Source != tc.source || !r.Known || r.Used != tc.used*1024 || r.Total != tc.total*1024 || len(r.Unread) != 0 {
				t.Errorf("Disk = %+v", r)
			}
		})
	}
	for name, out := range map[string]string{"header alone": dfHeader, "not numbers": "/dev/sda a b c 2% /\n", "too big": "/dev/sda 18446744073709551615 1 1 2% /\n", "empty": ""} {
		t.Run(name, func(t *testing.T) {
			h := newHost(t)
			h.cmd["df -kP /"] = hostfake.Response{Stdout: out}
			r := details.Read(context.Background(), h, debianFacts()).Disk
			if r.Known || len(r.Unread) != 1 || !errors.Is(r.Unread[0].Err, detect.ErrUnrecognised) {
				t.Errorf("Disk = %+v", r)
			}
		})
	}
	t.Run("no line for /", func(t *testing.T) {
		h := newHost(t)
		h.add(mountsFile, "sysfs /sys sysfs rw 0 0\n")
		r := details.Read(context.Background(), h, debianFacts()).Disk
		if !r.Known || len(r.Unread) != 1 || r.Unread[0].Source != mountsFile {
			t.Errorf("Disk = %+v", r)
		}
	})
}

// TestZFS: every form here is [unverified], typed from zpool(8) and zfs(8), as no
// ZFS VM has run them: the dataset is what the mount names, and with no zpool or
// zfs the section falls back to df, the pool's health not known.
func TestZFS(t *testing.T) {
	const (
		mounts = "rpool/ROOT/pve-1 / zfs rw,relatime,xattr,noacl,casesensitive 0 0\n"
		zpool  = "zpool list -H -o health rpool"
		zfs    = "zfs get -Hp -o value used,available rpool/ROOT/pve-1"
		sizes  = "4294967296\n36507222016\n"
	)
	failed := hostfake.Response{Stderr: "failed\n", ExitCode: 1}
	notFound := hostfake.Response{Err: fmt.Errorf("host: not in path: %w", host.ErrNotFound)}
	for _, tc := range []struct {
		name        string
		pool, sizes hostfake.Response
		health      string
		used, total uint64
		unread      []string
		runsDF      bool
	}{
		{name: "online", pool: hostfake.Response{Stdout: "ONLINE\n"}, sizes: hostfake.Response{Stdout: sizes}, health: "ONLINE", used: 4294967296, total: 40802189312},
		{name: "degraded", pool: hostfake.Response{Stdout: "DEGRADED\n"}, sizes: hostfake.Response{Stdout: sizes}, health: "DEGRADED", used: 4294967296, total: 40802189312},
		{name: "no zpool or zfs: df, the pool not known", pool: notFound, sizes: notFound, used: 1409984 * 1024, total: 10113424 * 1024, runsDF: true},
		{name: "no zpool: the sizes stay", pool: notFound, sizes: hostfake.Response{Stdout: sizes}, used: 4294967296, total: 40802189312},
		{name: "zpool fails", pool: failed, sizes: hostfake.Response{Stdout: sizes}, used: 4294967296, total: 40802189312, unread: []string{details.SourceZpool}},
		{name: "zfs fails: df", pool: hostfake.Response{Stdout: "ONLINE\n"}, sizes: failed, health: "ONLINE", used: 1409984 * 1024, total: 10113424 * 1024, unread: []string{details.SourceZFS}, runsDF: true},
		{name: "zpool's answer in no form known", pool: hostfake.Response{Stdout: "no such pool here\n"}, sizes: hostfake.Response{Stdout: sizes}, used: 4294967296, total: 40802189312, unread: []string{details.SourceZpool}},
		{name: "zfs's answer in no form known: df", pool: hostfake.Response{Stdout: "ONLINE\n"}, sizes: hostfake.Response{Stdout: "- -\n"}, health: "ONLINE", used: 1409984 * 1024, total: 10113424 * 1024, unread: []string{details.SourceZFS}, runsDF: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			h.add(mountsFile, mounts)
			h.cmd[zpool], h.cmd[zfs] = tc.pool, tc.sizes
			r := details.Read(context.Background(), h, debianFacts()).Disk
			var srcs []string
			for _, u := range r.Unread {
				srcs = append(srcs, u.Source)
			}
			if r.Source != "rpool/ROOT/pve-1" || r.Type != "zfs" || r.Pool != "rpool" || r.Health != tc.health || !r.Known || r.Used != tc.used || r.Total != tc.total || !slices.Equal(srcs, tc.unread) {
				t.Errorf("Disk = %+v", r)
			}
			if got := slices.Contains(h.runs(), "df -kP /"); got != tc.runsDF {
				t.Errorf("runs %v", h.runs())
			}
		})
	}
	t.Run("the dataset is the mount's", func(t *testing.T) {
		h := newHost(t)
		h.add(mountsFile, `tank/my\040root / zfs rw 0 0`+"\n")
		h.cmd["zpool list -H -o health tank"] = hostfake.Response{Stdout: "ONLINE\n"}
		h.cmd["zfs get -Hp -o value used,available tank/my root"] = hostfake.Response{Stdout: sizes}
		r := details.Read(context.Background(), h, debianFacts()).Disk
		if r.Source != "tank/my root" || r.Pool != "tank" || r.Health != "ONLINE" || !r.Known {
			t.Errorf("Disk = %+v", r)
		}
		for _, run := range h.runs() {
			if strings.Contains(run, "zroot") {
				t.Errorf("a hard-coded dataset: %s", run)
			}
		}
	})
	t.Run("a dataset that reads as an option", func(t *testing.T) {
		h := newHost(t)
		h.add(mountsFile, "-rpool/x / zfs rw 0 0\n")
		r := details.Read(context.Background(), h, debianFacts()).Disk
		if !r.Known || len(r.Unread) != 1 {
			t.Errorf("Disk = %+v", r)
		}
		for _, run := range h.runs() {
			if strings.HasPrefix(run, "z") {
				t.Errorf("ran %s", run)
			}
		}
	})
}

// TestMemory: no MemAvailable is a fact the report carries.
func TestMemory(t *testing.T) {
	for _, tc := range []struct {
		name, mem   string
		total, used uint64
		known       bool
		unread      bool
	}{
		{name: "the VMs' form", mem: memFull, total: 2021528 * 1024, used: (2021528 - 1551444) * 1024, known: true},
		{name: "no MemAvailable", mem: memOld, total: 2021528 * 1024},
		{name: "more available than there is", mem: "MemTotal: 10 kB\nMemAvailable: 11 kB\n", total: 10 * 1024},
		{name: "no MemTotal", mem: "MemAvailable: 11 kB\n", unread: true},
		{name: "nothing", mem: "", unread: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			h.add(memFile, tc.mem)
			m := details.Read(context.Background(), h, debianFacts()).Memory
			used, ok := m.Used()
			if m.Total != tc.total || used != tc.used || ok != tc.known || (len(m.Unread) > 0) != tc.unread {
				t.Errorf("Memory = %+v, used %d %v", m, used, ok)
			}
		})
	}
}

// loginsHost is newHost with lslogins' answer for root and alice.
func loginsHost(t *testing.T, answer hostfake.Response) *fakeHost {
	t.Helper()
	h := newHost(t)
	h.cmd[strings.Join(lsloginsArgv("root,alice"), " ")] = answer
	return h
}

// TestLogins: lslogins' lines by name, on the forms of 02-login.log and
// 08-lslogins.log. A failed run or a line that does not parse is the section
// unread, never no login; a listed name with no line is that account's Unknown.
func TestLogins(t *testing.T) {
	when, err := time.Parse(time.RFC3339, "2026-10-20T11:42:05+00:00")
	if err != nil {
		t.Fatal(err)
	}
	alice := details.Login{User: "alice", State: details.LoginAt, When: "2026-10-20T11:42:05+00:00", TTY: "pts/0", From: "192.0.2.10", At: when}
	none := func(name string) details.Login { return details.Login{User: name, State: details.LoginNone} }
	unknown := func(name string) details.Login { return details.Login{User: name} }
	for _, tc := range []struct {
		name string
		out  string
		want []details.Login
	}{
		{"a login and a blank", blankRoot + loginAlice, []details.Login{none("root"), alice}},
		{"the order asked, whatever the order answered", loginAlice + blankRoot, []details.Login{none("root"), alice}},
		{"a name it does not know has no line", loginAlice, []details.Login{unknown("root"), alice}},
		{"no line at all", "", []details.Login{unknown("root"), unknown("alice")}},
		{"at a console: no host [I]", blankRoot + "alice 2026-10-20T11:42:05+00:00 tty1 \n", []details.Login{none("root"),
			{User: "alice", State: details.LoginAt, When: "2026-10-20T11:42:05+00:00", TTY: "tty1", At: when}}},
		{"no newline at the end", blankRoot + strings.TrimSuffix(loginAlice, "\n"), []details.Login{none("root"), alice}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := loginsHost(t, hostfake.Response{Stdout: tc.out})
			l := details.Read(context.Background(), h, debianFacts()).Logins
			if l.State != details.LoginsRead || !reflect.DeepEqual(l.List, tc.want) || len(l.Unread) != 0 {
				t.Errorf("Logins = %+v", l)
			}
		})
	}
	for name, out := range map[string]string{
		"three fields":         "root  \n" + loginAlice,
		"five fields":          blankRoot + "alice 2026-10-20T11:42:05+00:00 pts/0 192.0.2.10 more\n",
		"no time":              blankRoot + "alice  pts/0 192.0.2.10\n",
		"not a time":           blankRoot + "alice 10:06 pts/0 192.0.2.10\n",
		"a name not asked":     blankRoot + loginAlice + "bob   \n",
		"a name twice":         blankRoot + loginAlice + "alice   \n",
		"an escaped name":      blankRoot + `al\x20ice   ` + "\n",
		"a line with no name":  "   \n",
		"words":                "lslogins: something\n",
		"a blank line between": blankRoot + "\n" + loginAlice,
	} {
		t.Run("does not parse: "+name, func(t *testing.T) {
			h := loginsHost(t, hostfake.Response{Stdout: out})
			r := details.Read(context.Background(), h, debianFacts())
			wholeWith(t, r, map[string][]string{"logins": {details.SourceLslogins}})
			if l := r.Logins; l.State != details.LoginsUnread || l.List != nil || !errors.Is(l.Unread[0].Err, detect.ErrUnrecognised) {
				t.Errorf("Logins = %+v", l)
			}
		})
	}
	t.Run("a failed run", func(t *testing.T) {
		h := loginsHost(t, hostfake.Response{Stdout: blankRoot + loginAlice, ExitCode: 1})
		l := details.Read(context.Background(), h, debianFacts()).Logins
		var xe *host.ExitError
		if l.State != details.LoginsUnread || l.List != nil || !errors.As(l.Unread[0].Err, &xe) {
			t.Errorf("Logins = %+v", l)
		}
	})
	t.Run("not installed", func(t *testing.T) {
		h := loginsHost(t, hostfake.Response{Err: fmt.Errorf("host: \"lslogins\": not in path: %w", host.ErrNotFound)})
		l := details.Read(context.Background(), h, debianFacts()).Logins
		if l.State != details.LoginsNoTool || l.List != nil || l.Unread[0].Source != details.SourceLslogins {
			t.Errorf("Logins = %+v", l)
		}
	})
	t.Run("one call, as decision 3 (a) has it", func(t *testing.T) {
		h := loginsHost(t, hostfake.Response{Stdout: blankRoot + loginAlice})
		details.Read(context.Background(), h, debianFacts())
		var calls [][]string
		for _, r := range h.Runs() {
			if r[0] == "lslogins" {
				calls = append(calls, r)
			}
		}
		if len(calls) != 1 || !slices.Equal(calls[0], lsloginsArgv("root,alice")) {
			t.Errorf("lslogins ran %v", calls)
		}
	})
	t.Run("a name it would split is not asked", func(t *testing.T) {
		h := newHost(t)
		h.add(passwdFile, "root:x:0:0::/root:/bin/bash\nalice:x:1000:1000::/h:/bin/bash\nal,ice:x:1001:1001::/h:/bin/bash\nb ob:x:1002:1002::/h:/bin/bash\n")
		h.cmd[strings.Join(lsloginsArgv("root,alice"), " ")] = hostfake.Response{Stdout: blankRoot + loginAlice}
		l := details.Read(context.Background(), h, debianFacts()).Logins
		want := []details.Login{none("root"), alice, unknown("al,ice"), unknown("b ob")}
		if !reflect.DeepEqual(l.List, want) {
			t.Errorf("Logins = %+v", l.List)
		}
	})
	t.Run("no one listed", func(t *testing.T) {
		h := newHost(t)
		h.add(passwdFile, "root:x:0:0::/root:/bin/bash\n")
		h.cmd[strings.Join(lsloginsArgv("root"), " ")] = hostfake.Response{Stdout: "root 2026-10-20T11:42:05+00:00 pts/0 192.0.2.10\n"}
		l := details.Read(context.Background(), h, debianFacts()).Logins
		if len(l.List) != 1 || l.List[0].State != details.LoginAt || l.List[0].User != "root" {
			t.Errorf("Logins = %+v", l.List)
		}
	})
}

// TestBlankGuard: lslogins answers blank, exit 0, when its sources are missing,
// empty or unreadable (09-lslogins-blank.log), so a blank is None only where a
// source can be read and holds records; otherwise Unknown. A real login reads as
// itself whatever the sources.
func TestBlankGuard(t *testing.T) {
	const (
		wtmpRecord = "\x07\x00\x00\x00\x10\x27\x00\x00pts/0\x00" // type 7, a pid, a line (05-wtmp-*.tail's fields, synthetic)
		llRecord   = "\x01\x00\x00\x00pts/0\x00"
	)
	// sparseLastlog is the VMs' shape: 1001 slots of 292 bytes, 292292 bytes
	// (01-tools.log:29), only uid 1000's holding anything.
	sparseLastlog := strings.Repeat("\x00", 292000) + llRecord + strings.Repeat("\x00", 292-len(llRecord))
	bare := func(h *fakeHost, paths ...string) {
		for _, p := range paths {
			if err := h.Remove(context.Background(), p); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		name    string
		out     string // lslogins' answer; blankRoot + blank alice when ""
		set     func(h *fakeHost)
		want    details.LoginState
		sources []details.SourceState // wtmp, lastlog2, lastlog; nil: none asked
	}{
		{name: "all readable, holding records, and a blank: None",
			set:     func(h *fakeHost) { h.add(lastlog2File, "SQLite format 3\x00...") },
			want:    details.LoginNone,
			sources: []details.SourceState{details.SourceRecords, details.SourceRecords, details.SourceRecords}},
		{name: "wtmp unreadable, lastlog empty, and a blank: Unknown",
			set:     func(h *fakeHost) { h.refuse(wtmpFile); h.add(lastlogFile, "") },
			want:    details.LoginUnknown,
			sources: []details.SourceState{details.SourceUnread, details.SourceMissing, details.SourceNoRecords}},
		{name: "a real login beside a blank",
			out:  blankRoot + loginAlice,
			set:  func(h *fakeHost) { h.refuse(wtmpFile); h.add(lastlogFile, "") },
			want: details.LoginAt, sources: []details.SourceState{details.SourceUnread, details.SourceMissing, details.SourceNoRecords}},
		{name: "all three missing",
			set:     func(h *fakeHost) { bare(h, wtmpFile, lastlogFile) },
			want:    details.LoginUnknown,
			sources: []details.SourceState{details.SourceMissing, details.SourceMissing, details.SourceMissing}},
		{name: "all three empty, as Alpine's wtmp is (08-lslogins.log:113)",
			set: func(h *fakeHost) {
				h.add(wtmpFile, "")
				h.add(lastlogFile, "")
				h.add(lastlog2File, "")
			},
			want:    details.LoginUnknown,
			sources: []details.SourceState{details.SourceNoRecords, details.SourceNoRecords, details.SourceNoRecords}},
		{name: "all unreadable",
			set:     func(h *fakeHost) { h.refuse(wtmpFile, lastlogFile, lastlog2File) },
			want:    details.LoginUnknown,
			sources: []details.SourceState{details.SourceUnread, details.SourceUnread, details.SourceUnread}},
		{name: "wtmp alone holds records",
			set:     func(h *fakeHost) { h.add(wtmpFile, wtmpRecord); bare(h, lastlogFile) },
			want:    details.LoginNone,
			sources: []details.SourceState{details.SourceRecords, details.SourceMissing, details.SourceMissing}},
		{name: "lastlog alone holds records: wtmp rotated away",
			set:     func(h *fakeHost) { h.add(wtmpFile, "") },
			want:    details.LoginNone,
			sources: []details.SourceState{details.SourceNoRecords, details.SourceMissing, details.SourceRecords}},
		{name: "the VMs' lastlog, its record past the first look",
			set:     func(h *fakeHost) { h.add(wtmpFile, ""); h.add(lastlogFile, sparseLastlog) },
			want:    details.LoginNone,
			sources: []details.SourceState{details.SourceNoRecords, details.SourceMissing, details.SourceRecords}},
		{name: "a lastlog of nothing but NUL",
			set:     func(h *fakeHost) { h.add(wtmpFile, ""); h.add(lastlogFile, strings.Repeat("\x00", 292*3)) },
			want:    details.LoginUnknown,
			sources: []details.SourceState{details.SourceNoRecords, details.SourceMissing, details.SourceNoRecords}},
		{name: "a long lastlog of nothing but NUL",
			set:     func(h *fakeHost) { h.add(wtmpFile, ""); h.add(lastlogFile, strings.Repeat("\x00", 292292)) },
			want:    details.LoginUnknown,
			sources: []details.SourceState{details.SourceNoRecords, details.SourceMissing, details.SourceNoRecords}},
		{name: "a link at a source is not read",
			set: func(h *fakeHost) {
				bare(h, wtmpFile, lastlogFile)
				h.add("/var/log/elsewhere", wtmpRecord)
				h.AddSymlink(wtmpFile, "/var/log/elsewhere")
			},
			want:    details.LoginUnknown,
			sources: []details.SourceState{details.SourceUnread, details.SourceMissing, details.SourceMissing}},
		{name: "a directory at a source is not read",
			set:     func(h *fakeHost) { bare(h, wtmpFile, lastlogFile); h.AddDir(wtmpFile, 0o755, 0, 0) },
			want:    details.LoginUnknown,
			sources: []details.SourceState{details.SourceUnread, details.SourceMissing, details.SourceMissing}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.out
			if out == "" {
				out = blankRoot + "alice   \n"
			}
			h := loginsHost(t, hostfake.Response{Stdout: out})
			tc.set(h)
			l := details.Read(context.Background(), h, debianFacts()).Logins
			if l.State != details.LoginsRead {
				t.Fatalf("Logins = %+v", l)
			}
			if got := l.List[1].State; got != tc.want {
				t.Errorf("alice = %+v", l.List[1])
			}
			var got []details.SourceState
			for _, s := range l.Sources {
				got = append(got, s.State)
			}
			if !slices.Equal(got, tc.sources) {
				t.Errorf("sources = %+v, want %v", l.Sources, tc.sources)
			}
			want := []string{wtmpFile, lastlog2File, lastlogFile}
			for i, s := range l.Sources {
				if s.Path != want[i] {
					t.Errorf("source %d is %s", i, s.Path)
				}
			}
		})
	}
	t.Run("no blank, nothing asked", func(t *testing.T) {
		h := loginsHost(t, hostfake.Response{Stdout: "root 2026-10-20T11:42:05+00:00 pts/0 192.0.2.10\n" + loginAlice})
		l := details.Read(context.Background(), h, debianFacts()).Logins
		if l.Sources != nil {
			t.Errorf("sources = %+v", l.Sources)
		}
		for _, c := range h.Calls() {
			if c.Op == "scan-lines" {
				t.Errorf("read %s", c.Path)
			}
		}
	})
}

// TestReadSourceRealFiles: the guard's read truly opens the file, on real files
// in a temporary directory: a mode check would pass mode 0000 to no one's benefit,
// and a FIFO must not block it.
func TestReadSourceRealFiles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	real := host.NewReal(host.Options{})
	dir := t.TempDir()
	// t.TempDir on macOS lives under a symlink; guarded reads want the real path.
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte, mode fs.FileMode) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	check := func(name, p string, want details.SourceState) {
		t.Helper()
		if got := details.ReadSource(ctx, real, p); got.State != want || got.Path != p {
			t.Errorf("%s: %+v, want state %d", name, got, want)
		}
	}
	check("records", write("wtmp", []byte("\x07\x00alice"), 0o644), details.SourceRecords)
	check("empty", write("empty", nil, 0o644), details.SourceNoRecords)
	check("missing", filepath.Join(dir, "nothing"), details.SourceMissing)
	sparse := append(make([]byte, 292000), 1, 0, 0, 0)
	check("sparse, past the first look", write("lastlog", sparse, 0o644), details.SourceRecords)
	check("all NUL, long", write("nul", make([]byte, 200<<10), 0o644), details.SourceNoRecords)
	check("past recordsLook", write("huge", append(make([]byte, 5<<20), 1), 0o644), details.SourceNoRecords)
	if os.Geteuid() != 0 {
		check("mode 0000", write("sealed", []byte("records"), 0), details.SourceUnread)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	got := details.ReadSource(ctx, real, fifo)
	if got.State != details.SourceUnread || !errors.Is(got.Err, fs.ErrInvalid) {
		t.Errorf("fifo: %+v", got)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(filepath.Join(dir, "wtmp"), link); err != nil {
		t.Fatal(err)
	}
	if got := details.ReadSource(ctx, real, link); got.State != details.SourceUnread || !errors.Is(got.Err, host.ErrSymlink) {
		t.Errorf("link: %+v", got)
	}
}

// deadlines records the deadline each command runs under.
type deadlines struct {
	*fakeHost
	left map[string]time.Duration
}

func (d *deadlines) Run(ctx context.Context, name string, args ...string) (host.Result, error) {
	at, ok := ctx.Deadline()
	if !ok {
		d.left[name] = -1
	} else {
		d.left[name] = time.Until(at)
	}
	return d.fakeHost.Run(ctx, name, args...)
}

// TestBounds: each command runs under its bound, those of the packages it reads
// through too: lslogins' 20 s (one wait for the password lock, 15 s by lslogins.c
// v2.41:831 and glibc, not run, and about five of the VMs' wtmp read, 0.9 s,
// 08-lslogins.log), and 5 s for the rest.
func TestBounds(t *testing.T) {
	if details.Bounds.Command != 5*time.Second || details.Bounds.Lslogins != 20*time.Second || details.Bounds.Grace != time.Second {
		t.Errorf("bounds = %+v", details.Bounds)
	}
	h := newHost(t)
	h.add(mountsFile, "rpool/ROOT/os / zfs rw 0 0\n")
	h.cmd["zpool list -H -o health rpool"] = hostfake.Response{Stdout: "ONLINE\n"}
	h.cmd["zfs get -Hp -o value used,available rpool/ROOT/os"] = hostfake.Response{Stderr: "failed\n", ExitCode: 1}
	d := &deadlines{fakeHost: h, left: map[string]time.Duration{}}
	details.Read(context.Background(), d, debianFacts())
	for name, limit := range map[string]time.Duration{"ip": details.Bounds.Command, "df": details.Bounds.Command, "zpool": details.Bounds.Command, "zfs": details.Bounds.Command, "visudo": details.Bounds.Command, "lslogins": details.Bounds.Lslogins} {
		left, ran := d.left[name]
		if !ran || left <= 0 || left > limit || left < limit-time.Second {
			t.Errorf("%s ran %v with %v left, bound %v", name, ran, left, limit)
		}
	}
}

// hung is a host whose named commands never end, whatever ctx says, as a process
// stuck in the kernel on a hung pool or mount: host.Real's Run could not return.
type hung struct {
	*fakeHost
	names   []string
	release chan struct{}
}

func (h *hung) Run(ctx context.Context, name string, args ...string) (host.Result, error) {
	if slices.Contains(h.names, name) {
		<-h.release
		return host.Result{}, ctx.Err()
	}
	return h.fakeHost.Run(ctx, name, args...)
}

// TestHungCommand: a command that never ends leaves its section unread, by
// source, with the deadline as its reason; the rest stays, and Read returns.
func TestHungCommand(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	for _, tc := range []struct {
		name  string
		names []string
		mount string
		want  map[string][]string
	}{
		{"df", []string{"df"}, "", map[string][]string{"disk": {details.SourceDF}}},
		{"lslogins", []string{"lslogins"}, "", map[string][]string{"logins": {details.SourceLslogins}}},
		{"ip", []string{"ip"}, "", map[string][]string{"host": {details.SourceIP}}},
		{"zpool and zfs, then df", []string{"zpool", "zfs"}, "rpool/ROOT/os / zfs rw 0 0\n", map[string][]string{"disk": {details.SourceZpool, details.SourceZFS}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &hung{fakeHost: newHost(t), names: tc.names, release: release}
			if tc.mount != "" {
				h.add(mountsFile, tc.mount)
			}
			start := time.Now()
			r := details.ReadBounded(context.Background(), h, debianFacts(), 20*time.Millisecond, 40*time.Millisecond, 10*time.Millisecond)
			if took := time.Since(start); took > 10*time.Second {
				t.Errorf("took %v", took)
			}
			wholeWith(t, r, tc.want)
			for _, us := range [][]details.Unread{r.Host.Unread, r.Disk.Unread, r.Logins.Unread} {
				for _, u := range us {
					if !errors.Is(u.Err, context.DeadlineExceeded) {
						t.Errorf("%s: %v", u.Source, u.Err)
					}
				}
			}
			if tc.name == "lslogins" && r.Logins.State != details.LoginsUnread {
				t.Errorf("Logins = %+v", r.Logins)
			}
		})
	}
}

// TestCommandEndsAtTheBound: a command that honours ctx, as host.Real's does by
// killing it, returns its own error at the bound, not the abandoned one's.
func TestCommandEndsAtTheBound(t *testing.T) {
	h := &ctxHost{fakeHost: newHost(t)}
	r := details.ReadBounded(context.Background(), h, debianFacts(), 20*time.Millisecond, 20*time.Millisecond, time.Minute)
	u := r.Disk.Unread
	if len(u) != 1 || !errors.Is(u[0].Err, context.DeadlineExceeded) || !strings.Contains(u[0].Err.Error(), "killed") {
		t.Errorf("Disk = %+v", r.Disk)
	}
}

type ctxHost struct{ *fakeHost }

func (h *ctxHost) Run(ctx context.Context, name string, args ...string) (host.Result, error) {
	if name == "df" {
		<-ctx.Done()
		return host.Result{}, fmt.Errorf("killed: %w", ctx.Err())
	}
	return h.fakeHost.Run(ctx, name, args...)
}

// TestReadWritesNothing: a Read writes nothing and runs nothing but its read-only
// list, on a host with every branch taken; its only lines in a run log are the
// host's own for each command; the reader adds none.
func TestReadWritesNothing(t *testing.T) {
	reads := []string{"read", "read-nofollow", "scan-lines", "stat", "lstat", "readdir"}
	lists := map[string]bool{
		"ip":         true, // -o addr show scope global; the header probe's -4 addr show dev
		"df":         true, // -kP /
		"zpool":      true, // list -H -o health
		"zfs":        true, // get -Hp -o value
		"visudo":     true, // -c
		"doas":       true, // -C
		"lslogins":   true,
		"dpkg-query": true, // the header probe's, --show
	}
	wantArgs := map[string]func([]string) bool{
		"ip":         func(a []string) bool { return a[0] == "-o" && a[1] == "addr" || a[0] == "-4" && a[1] == "addr" },
		"df":         func(a []string) bool { return slices.Equal(a, []string{"-kP", "/"}) },
		"zpool":      func(a []string) bool { return a[0] == "list" },
		"zfs":        func(a []string) bool { return a[0] == "get" },
		"visudo":     func(a []string) bool { return slices.Equal(a, []string{"-c"}) },
		"doas":       func(a []string) bool { return a[0] == "-C" },
		"lslogins":   func(a []string) bool { return strings.HasPrefix(a[0], "--time-format") },
		"dpkg-query": func(a []string) bool { return a[0] == "--show" },
	}
	for _, zfs := range []bool{false, true} {
		var runLog bytes.Buffer
		f := hostfake.New(host.Options{RunLog: &runLog})
		base := newHost(t)
		h := &fakeHost{System: f, refused: map[string]error{}, cmd: base.cmd}
		for _, p := range []string{hostnameFile, uptimeFile, releaseFile, statusFile, cpuFile, loadFile, memFile, mountsFile, passwdFile, groupFile, sudoersFile, sudoersDir + "/README", "/usr/bin/sudo"} {
			e, _ := base.Entry(p)
			h.add(p, string(e.Data))
		}
		h.AddDir(sudoersDir, 0o750, 0, 0)
		h.add(wtmpFile, "\x07")
		h.add(lastlogFile, "\x01")
		// A default route, so that the header probe runs its ip.
		h.add("/proc/net/route", "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\neth0\t00000000\t010200C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n")
		f.Handler = func(argv []string, _ []byte) hostfake.Response {
			if r, ok := h.cmd[strings.Join(argv, " ")]; ok {
				return r
			}
			return hostfake.Response{ExitCode: 1}
		}
		if zfs {
			h.add(mountsFile, "rpool/ROOT/os / zfs rw 0 0\n")
		}
		ctx := context.Background()
		before, ranIP := len(f.Calls()), false
		details.Read(ctx, h, debianFacts())
		for _, c := range f.Calls()[before:] {
			switch {
			case c.Op == "run":
				ranIP = ranIP || c.Argv[0] == "ip" && c.Argv[1] == "-4"
				if !lists[c.Argv[0]] || !wantArgs[c.Argv[0]](c.Argv[1:]) {
					t.Errorf("ran %v", c.Argv)
				}
			case !slices.Contains(reads, c.Op):
				t.Errorf("did %s", c)
			}
		}
		if !ranIP {
			t.Error("the header probe's ip did not run: this test no longer takes that branch")
		}
		for _, line := range strings.Split(strings.TrimSpace(runLog.String()), "\n") {
			if !strings.HasPrefix(line, "run ") {
				t.Errorf("a line of its own in the run log: %q", line)
			}
		}
	}
}

// TestReadNoRunLog: on a host with no run log, as a draw's, nothing is written
// and nothing fails.
func TestReadNoRunLog(t *testing.T) {
	h := newHost(t)
	wholeWith(t, details.Read(context.Background(), h, debianFacts()), map[string][]string{})
}

// TestDoneContext: a done ctx leaves every fact with ctx's error, and runs nothing.
func TestDoneContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := newHost(t)
	r := details.Read(ctx, h, debianFacts())
	for _, c := range h.Calls() {
		if c.Err == nil {
			t.Errorf("%s went through under a done ctx", c)
		}
	}
	for name, us := range map[string][]details.Unread{"os": r.OS.Unread, "host": r.Host.Unread, "users": r.Users.Unread, "cpu": r.CPU.Unread, "disk": r.Disk.Unread, "memory": r.Memory.Unread, "logins": r.Logins.Unread} {
		if len(us) == 0 {
			t.Errorf("%s has no unread", name)
		}
		for _, u := range us {
			if !errors.Is(u.Err, context.Canceled) {
				t.Errorf("%s: %s: %v", name, u.Source, u.Err)
			}
		}
	}
}

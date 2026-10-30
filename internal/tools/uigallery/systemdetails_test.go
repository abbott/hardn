package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/engine/details"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The System Details mocks (systemdetails.go; slice 1, unit (a)). Nothing
// here runs a command or writes a file outside the fake.

// listCells are a listing mock's golden cells: the core set and LANG=C's,
// leaving out those narrower than the mock needs.
func listCells(m listMock) []uitest.Cell {
	var cells []uitest.Cell
	for _, c := range append(uitest.Core(), asciiColour) {
		if c.Env.Width >= m.wide {
			cells = append(cells, c)
		}
	}
	return cells
}

// goldenListing pins each mock of l under prefix: the golden files are
// testdata/golden/<prefix>/<mock>.golden.
func goldenListing(t *testing.T, l listing) {
	t.Helper()
	for _, m := range l.mocks() {
		uitest.Golden(t, l.prefix+"/"+m.name, listCells(m), func(env term.Env) string {
			var out, errs bytes.Buffer
			args := append(lookArgs(t, env), m.name)
			if code := run(append([]string{l.prefix}, args...), nil, offTerminal, &out, &errs); code != exitOK || errs.Len() > 0 {
				t.Errorf("%s: exit %d, stderr %q", m.name, code, errs.String())
			}
			return out.String()
		})
	}
}

// TestSystemDetailsGolden pins each listing mock on the core set and under
// LANG=C: the screen on each host, plan only, the non-interactive forms and
// origin/main's report.
func TestSystemDetailsGolden(t *testing.T) { goldenListing(t, sdListing) }

// TestPlaySystemDetailsGolden plays the reading end to end and pins each
// transcript: from the main menu's 9, on Debian 13, Refresh and leaving;
// the same under plan only; on each release; one section unreadable; and the
// hosts' menu.
func TestPlaySystemDetailsGolden(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		lines []string
		cells []uitest.Cell
	}{
		{"system-details", nil, []string{"9", "0", "0"}, nil},
		{"system-details-refresh", []string{"system-details/debian13"}, []string{"1", "0"}, append(uitest.Core(), asciiColour)},
		{"system-details-plan-only", []string{"-sequence", "dry-run"}, []string{"9", "1", "0", "0"}, nil},
		{"system-details-sudo-rs", []string{"system-details/ubuntu2604"}, []string{"0"}, nil},
		{"system-details-alpine", []string{"system-details/alpine323"}, []string{"1", "0"}, nil},
		{"system-details-zfs", []string{"system-details/pve9"}, []string{"1", "0"}, nil},
		{"system-details-unreadable", []string{"system-details/unreadable"}, []string{"1", "q"}, nil},
		{"system-details-hosts", []string{"system-details"}, []string{"6", "0", "0"}, nil},
	}
	for _, tt := range tests {
		cells := tt.cells
		if cells == nil {
			cells = uitest.Core()
		}
		uitest.Golden(t, "play/"+tt.name, cells, func(env term.Env) string {
			args := append(lookArgs(t, env), tt.args...)
			code, out, errs, unread := playGallery(tt.lines, args...)
			if code != exitOK || errs != "" || unread != 0 {
				t.Errorf("%s, %v: exit %d, stderr %q, %d lines unread", tt.name, args, code, errs, unread)
			}
			return out
		})
	}
}

// unreadSections are the sections of r with anything not read.
func unreadSections(r detailsReading) []string {
	var out []string
	for _, s := range []struct {
		name   string
		unread []details.Unread
	}{
		{secOS, r.OS.Unread}, {secHost, r.Host.Unread}, {secUsers, r.Users.Unread}, {secCPU, r.CPU.Unread},
		{secDisk, r.Disk.Unread}, {secMemory, r.Memory.Unread}, {secLogin, r.Logins.Unread},
	} {
		if len(s.unread) > 0 {
			out = append(out, s.name)
		}
	}
	return out
}

// TestSDHosts holds each host's reading to what its note says: the release
// detect establishes, the kernel, the file system, ZFS's dataset as read,
// the users and their tool, how many logins it read, and which section is not
// read.
func TestSDHosts(t *testing.T) {
	tests := []struct {
		host, id, kernel, fs, source string
		health                       string
		tool                         string
		users, addrs, logins         int
		unread                       []string
	}{
		{host: "debian13", id: "debian", kernel: debianVMKernel, fs: "ext4", source: "/dev/vda1", tool: "sudo", users: 1, addrs: 1, logins: 2},
		{host: "ubuntu2604", id: "ubuntu", kernel: ubuntuVMKernel, fs: "ext4", source: "/dev/vda1", tool: "sudo", users: 1, addrs: 1, logins: 2},
		{host: "alpine323", id: "alpine", kernel: alpineVMKernel, fs: "ext4", source: "/dev/vda1", tool: "doas", users: 1, addrs: 1, unread: []string{secLogin}},
		{host: "pve9", id: "debian", kernel: "6.17.2-1-pve", fs: "zfs", source: pveDataset, health: "ONLINE", addrs: 1, logins: 1},
		{host: "zfs-degraded", id: "debian", kernel: "6.17.2-1-pve", fs: "zfs", source: pveDataset, health: "DEGRADED", addrs: 1, logins: 1},
		{host: "unreadable", id: "debian", kernel: debianVMKernel, fs: "ext4", source: "/dev/vda1", tool: "sudo", users: 1, addrs: 1, logins: 2, unread: []string{secMemory}},
		{host: "no-logins", id: "debian", kernel: debianVMKernel, fs: "ext4", source: "/dev/vda1", tool: "sudo", users: 1, addrs: 1, logins: 2},
		{host: "login-console", id: "debian", kernel: debianVMKernel, fs: "ext4", source: "/dev/vda1", tool: "sudo", users: 1, addrs: 1, logins: 2},
		{host: "many", id: "debian", kernel: debianVMKernel, fs: "ext4", source: "/dev/vda1", tool: "sudo", users: 14, addrs: 9, logins: 15},
	}
	if len(tests) != len(sdHosts()) {
		t.Fatalf("%d hosts tested, %d hosts", len(tests), len(sdHosts()))
	}
	for _, tt := range tests {
		r, err := readSDHost(context.Background(), tt.host)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case r.facts.OS.ID != tt.id || r.OS.Kernel != tt.kernel:
			t.Errorf("%s: os %q, kernel %q", tt.host, r.facts.OS.ID, r.OS.Kernel)
		case r.Disk.Type != tt.fs || r.Disk.Source != tt.source || r.Disk.Health != tt.health:
			t.Errorf("%s: disk %+v", tt.host, r.Disk)
		case len(r.Users.List) != tt.users || len(r.Host.Addresses) != tt.addrs:
			t.Errorf("%s: %d users, %d addresses", tt.host, len(r.Users.List), len(r.Host.Addresses))
		case r.Users.Root != detect.Yes:
			t.Errorf("%s: Root %v, as hardn runs as root on these hosts", tt.host, r.Users.Root)
		case len(r.Logins.List) != tt.logins:
			t.Errorf("%s: %d logins, want %d", tt.host, len(r.Logins.List), tt.logins)
		case !slices.Equal(unreadSections(r), tt.unread):
			t.Errorf("%s: unread %q, want %q", tt.host, unreadSections(r), tt.unread)
		}
		if tt.users > 0 {
			if tool, tri := r.Users.List[0].Admin(); tool != tt.tool || tri != detect.Yes {
				t.Errorf("%s: %s's tool %q %v, want %q", tt.host, r.Users.List[0].Name, tool, tri, tt.tool)
			}
		}
		if r.facts.Elevation.Doas.Present == detect.Unknown {
			t.Errorf("%s: doas not established", tt.host)
		}
	}
	if eh := sdHostNamed(t, "ubuntu2604"); eh.host().sudo == "" || !strings.Contains(eh.host().sudo, "sudo-rs") {
		t.Errorf("ubuntu2604: sudo %q, want sudo-rs", eh.host().sudo)
	}
}

// sdHostNamed is the System Details mocks' host named name.
func sdHostNamed(t *testing.T, name string) sdHost {
	t.Helper()
	sh, ok := findSDHost(name)
	if !ok {
		t.Fatalf("no system-details host %q", name)
	}
	return sh
}

// TestSDLastLogin holds the rows to lslogins' one answer: root first, then the
// users, whatever order it answers in; a login with a host, a dotless one
// included, and with none; another year's; blanks. A listed name with no line
// reads not known and the others stay. A line for a name not asked, a garbled
// one or a failure says it could not read it, never no login, and a host with no
// lslogins says that.
func TestSDLastLogin(t *testing.T) {
	const at = "2026-10-20T11:42:05+00:00"
	cant := []component.KeyValue{{Key: "Can't read lslogins", State: component.ValueWarn}}
	none := component.KeyValue{Key: "root", Value: noLogin, State: component.ValueNone}
	alice := func(value string) []component.KeyValue {
		return []component.KeyValue{none, {Key: "alice", Value: value}}
	}
	aliceUnknown := component.KeyValue{Key: "alice", Value: notKnown, State: component.ValueUnknown}
	root := noLoginLine("root") + "\n"
	for _, tt := range []struct {
		name string
		res  hostfake.Response
		want []component.KeyValue
	}{
		{"a host", hostfake.Response{Stdout: root + "alice " + at + " pts/0 192.0.2.10\n"}, alice("Oct 20 11:42 from 192.0.2.10")},
		{"a dotless host", hostfake.Response{Stdout: root + "alice " + at + " pts/0 bastion\n"}, alice("Oct 20 11:42 from bastion")},
		{"no host", hostfake.Response{Stdout: root + "alice " + at + " tty1 \n"}, alice("Oct 20 11:42 on tty1")},
		{"another year", hostfake.Response{Stdout: root + "alice 2025-12-31T23:59:00+00:00 pts/0 192.0.2.10\n"}, alice("Dec 31 2025 from 192.0.2.10")},
		{"all blank", hostfake.Response{Stdout: root + noLoginLine("alice") + "\n"}, []component.KeyValue{none, {Key: "alice", Value: noLogin, State: component.ValueNone}}},
		{"another order", hostfake.Response{Stdout: "alice " + at + " pts/0 192.0.2.10\n" + root}, alice("Oct 20 11:42 from 192.0.2.10")},
		{"a name with no line", hostfake.Response{Stdout: root}, []component.KeyValue{none, aliceUnknown}},
		{"no line at all", hostfake.Response{}, []component.KeyValue{{Key: "root", Value: notKnown, State: component.ValueUnknown}, aliceUnknown}},
		{"a name not asked", hostfake.Response{Stdout: root + "alice-long " + at + " pts/0 192.0.2.10\n"}, cant},
		{"garbled time", hostfake.Response{Stdout: root + "alice yesterday pts/0 192.0.2.10\n"}, cant},
		{"too few fields", hostfake.Response{Stdout: root + "alice " + at + " pts/0\n"}, cant},
		{"a terminal and no time", hostfake.Response{Stdout: root + "alice  pts/0 \n"}, cant},
		{"a blank line", hostfake.Response{Stdout: root + "\n" + noLoginLine("alice") + "\n"}, cant},
		{"a failure", hostfake.Response{Stderr: "lslogins: cannot open\n", ExitCode: 1}, cant},
		{"not found", hostfake.Response{Err: host.ErrNotFound}, []component.KeyValue{{Key: rowLogins, Value: noLslogins, State: component.ValueUnknown}}},
	} {
		sh := sdHostNamed(t, "debian13")
		h := sh.host()
		model := h.f.Handler
		h.f.Handler = func(argv []string, stdin []byte) hostfake.Response {
			if argv[0] == "lslogins" {
				return tt.res
			}
			return model(argv, stdin)
		}
		if got := readDetails(context.Background(), h.sys()).sectionRows(secLogin); !slices.Equal(got, tt.want) {
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
		r, err := readSDHost(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.sectionRows(secLogin); !slices.Equal(got, want) {
			t.Errorf("%s: %+v, want %+v", name, got, want)
		}
	}
	// A blank with no source to back it is not a fact (decision 3 (a)): with
	// wtmp gone, and no lastlog here, each blank reads not known, the login stays.
	m := vmModel()
	m.noWtmp = true
	h := withDetails(onVMKernel(debian13Platform), m, vmCPU, 4, 1)
	want := []component.KeyValue{{Key: "root", Value: notKnown, State: component.ValueUnknown}, {Key: "alice", Value: session}}
	if got := readDetails(context.Background(), h.sys()).sectionRows(secLogin); !slices.Equal(got, want) {
		t.Errorf("no source: %+v, want %+v", got, want)
	}
}

// TestSDLsloginsCall: every draw asks lslogins once, all the names in the one
// call, as its cost is per call; and the fake answers that form as the real one
// does, no line for a name it does not know, the others still answered, exit 0
// (vm-runs/s1/08-lslogins.log).
func TestSDLsloginsCall(t *testing.T) {
	h := sdHostNamed(t, "many").host()
	readDetails(context.Background(), h.sys())
	var calls [][]string
	for _, c := range h.f.Calls() {
		if c.Op == "run" && c.Argv[0] == "lslogins" {
			calls = append(calls, c.Argv)
		}
	}
	const names = "root,alice,bob,carol,dave,erin,frank,grace,heidi,ivan,judy,mallory,niaj,olivia,peggy"
	if want := append(append([]string{"lslogins"}, lsloginsArgs...), names); len(calls) != 1 || !slices.Equal(calls[0], want) {
		t.Errorf("lslogins called %q, want once, %q", calls, want)
	}
	res, err := vmDetails(debian13Platform)().f.Run(context.Background(), "lslogins", append(slices.Clone(lsloginsArgs), "root,nosuchaccount,alice")...)
	if want := noLoginLine("root") + "\n" + sessionLine("alice") + "\n"; err != nil || string(res.Stdout) != want {
		t.Errorf("%q, %v; want %q", res.Stdout, err, want)
	}
}

// TestSDReadsAddrs: the reader takes the forms the VMs print, Debian's and
// Ubuntu's with a metric, Alpine's with noprefixroute and none (vm-runs/s1,
// 01-tools.log), and the fixtures print those forms.
func TestSDReadsAddrs(t *testing.T) {
	for _, tt := range []struct {
		name string
		host func() *fakeHost
		line string
	}{
		{"debian13", vmDetails(debian13Platform), "2: eth0    inet 192.0.2.20/24 metric 100 brd 192.0.2.255 scope global dynamic eth0\\       valid_lft 7104sec preferred_lft 7104sec\n"},
		{"alpine323", vmDetails(alpine323Platform), "2: eth0    inet 192.0.2.20/24 brd 192.0.2.255 scope global dynamic noprefixroute eth0\\       valid_lft 5235sec preferred_lft 4411sec\n"},
	} {
		res, err := tt.host().f.Run(context.Background(), "ip", "-o", "addr", "show", "scope", "global")
		if err != nil || string(res.Stdout) != tt.line {
			t.Errorf("%s: %q, %v; want %q", tt.name, res.Stdout, err, tt.line)
		}
		r, err := readSDHost(context.Background(), tt.name)
		if err != nil {
			t.Fatal(err)
		}
		if want := (details.Address{Interface: "eth0", Family: "inet", Address: "192.0.2.20/24"}); len(r.Host.Addresses) != 1 || r.Host.Addresses[0] != want {
			t.Errorf("%s: %+v", tt.name, r.Host.Addresses)
		}
	}
}

// TestSDBlankCPU: where /proc/cpuinfo has no model name or physical id, as on
// arm64 [I, not run], the row says so and no socket count is made up.
func TestSDBlankCPU(t *testing.T) {
	r := detailsReading{Report: details.Report{CPU: details.CPU{CPUs: 4}}}
	got := r.sectionRows(secCPU)
	if got[0].Value != notKnown || got[0].State != component.ValueUnknown || got[1].Value != "4" {
		t.Errorf("%+v", got)
	}
}

// TestSDNotRootNamesTheRules: as a user other than root the one row names each
// of the tool's files refused, and a user's row reads not known, never a group's
// guess; the login section, which a user reads the same (vm-runs/s1/02-login.log),
// stays.
func TestSDNotRootNamesTheRules(t *testing.T) {
	for _, tt := range []struct {
		p    platform
		want string
	}{{debian13Platform, "Not root: /etc/sudoers, /etc/sudoers.d not read"}, {alpine323Platform, "Not root: /etc/doas.conf, /etc/doas.d not read"}} {
		r := readAsUser(context.Background(), tt.p)
		rows := r.sectionRows(secUsers)
		want := []component.KeyValue{{Key: "alice", Value: notKnown, State: component.ValueUnknown}, {Key: tt.want, State: component.ValueWarn}}
		if !slices.Equal(rows, want) {
			t.Errorf("%+v, want %+v", rows, want)
		}
		if r.Users.Root != detect.No {
			t.Errorf("Root = %v", r.Users.Root)
		}
	}
}

// TestSDUnreadableKeepsTheRest: one section not read leaves the other six,
// where origin/main loses the whole report (details.go:91-117): the section
// says which file.
func TestSDUnreadableKeepsTheRest(t *testing.T) {
	r, err := readSDHost(context.Background(), "unreadable")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.sectionRows(secMemory); len(got) != 1 || !strings.Contains(got[0].Key, "/proc/meminfo") {
		t.Errorf("Memory's rows: %+v", got)
	}
	for _, s := range sdSections {
		if s != secMemory && len(r.sectionRows(s)) == 0 {
			t.Errorf("%s has no row", s)
		}
	}
}

// TestSDRows: a fact keeps its section's others, and what is read but not known
// says so: each row from the report, as the sections map it until unit (c).
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
		{"the kernel not read", secOS, details.Report{OS: details.OSKernel{Unread: unread("/proc/sys/kernel/osrelease")}}, []component.KeyValue{cant("/proc/sys/kernel/osrelease")}},
		{"ip failed, the hostname stays", secHost, details.Report{Host: details.HostIPs{Hostname: "web-01", Unread: unread(details.SourceIP)}},
			[]component.KeyValue{{Key: rowHostname, Value: "web-01"}, cant(details.SourceIP)}},
		{"ip answered none", secHost, details.Report{Host: details.HostIPs{Hostname: "web-01", Addresses: []details.Address{}}},
			[]component.KeyValue{{Key: rowHostname, Value: "web-01"}, {Key: noAddresses, State: component.ValueNone}}},
		{"no account file", secUsers, details.Report{Users: details.Users{Unread: unread("/etc/passwd")}}, []component.KeyValue{cant("/etc/passwd")}},
		{"root alone", secUsers, details.Report{Users: details.Users{List: []details.User{}}}, []component.KeyValue{{Key: noUsers, State: component.ValueNone}}},
		{"as root, a rule the tool rejects", secUsers, details.Report{Users: details.Users{List: []details.User{{Name: "alice"}}, Root: detect.Yes, Unread: unread("/etc/sudoers")}},
			[]component.KeyValue{{Key: "alice", Value: noSudo}, cant("/etc/sudoers")}},
		{"the load not read, the model stays", secCPU, details.Report{CPU: details.CPU{Model: "m", CPUs: 2, Sockets: 2, Unread: unread(details.LoadAvgFile)}},
			[]component.KeyValue{{Key: rowModel, Value: "m"}, {Key: rowCPUs, Value: "2, 2 sockets"}, cant(details.LoadAvgFile)}},
		{"cpuinfo not read, the load stays", secCPU, details.Report{CPU: details.CPU{Load: [3]string{"1", "2", "3"}, Unread: unread(details.CPUInfoFile)}},
			[]component.KeyValue{{Key: rowLoad, Value: "1, 2, 3"}, cant(details.CPUInfoFile)}},
		{"df failed, the mount stays", secDisk, details.Report{Disk: details.Disk{Source: "/dev/vda1", Type: "ext4", Unread: unread(details.SourceDF)}},
			[]component.KeyValue{{Key: rowRoot, Value: "/dev/vda1 ext4"}, cant(details.SourceDF)}},
		{"no zpool, df's figures", secDisk, details.Report{Disk: details.Disk{Source: "rpool/ROOT/pve-1", Type: "zfs", Used: 1 << 30, Total: 2 << 30, Known: true, Pool: "rpool"}},
			[]component.KeyValue{{Key: rowRoot, Value: "rpool/ROOT/pve-1 zfs"}, used, {Key: "Pool rpool", Value: notKnown, State: component.ValueUnknown}}},
		{"a pool not ONLINE", secDisk, details.Report{Disk: details.Disk{Source: "r/d", Type: "zfs", Used: 1 << 30, Total: 2 << 30, Known: true, Pool: "r", Health: "FAULTED"}},
			[]component.KeyValue{{Key: rowRoot, Value: "r/d zfs"}, used, {Key: "Pool r", Value: "FAULTED", State: component.ValueWarn}}},
		{"no MemAvailable", secMemory, details.Report{Memory: details.Memory{Total: 2 << 30}}, []component.KeyValue{{Key: rowUsed, Value: notKnown, State: component.ValueUnknown}}},
		{"meminfo", secMemory, details.Report{Memory: details.Memory{Total: 2 << 30, Available: 1 << 30, AvailableKnown: true}}, []component.KeyValue{used}},
	} {
		if got := (detailsReading{Report: tt.report}).sectionRows(tt.section); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

// TestSDZFSDatasetIsRead: the reader asks zfs about the dataset mounted
// on /, never origin/main's hard-coded zroot/ROOT/os (details.go:85).
func TestSDZFSDatasetIsRead(t *testing.T) {
	h := sdHostNamed(t, "pve9").host()
	readDetails(context.Background(), h.sys())
	asked := false
	for _, c := range h.f.Calls() {
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

// TestSDJSONIsTheDocument: the JSON form is pkg/output's document, its nine
// fields as they are, and one more, details.
func TestSDJSONIsTheDocument(t *testing.T) {
	r, err := readSDHost(context.Background(), "unreadable")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonDrawing(r, offTerminalCtx())), &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"schemaVersion", "hardnVersion", "host", "mode", "result", "changes", "status", "findings", "errors", "details"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("no %s", k)
		}
	}
	if len(doc) != 10 {
		t.Errorf("%d top-level fields, want the nine and details", len(doc))
	}
	if !strings.Contains(string(doc["findings"]), "DETAILS_UNREADABLE") || !strings.Contains(string(doc["details"]), `"memory": null`) || !strings.Contains(string(doc["details"]), `"when": "2026-10-20T11:42:05+00:00"`) {
		t.Errorf("findings %s, details %s", doc["findings"], doc["details"])
	}
}

// TestSystemDetailsUsage: the listing and play refuse what they do not
// take, with status 2 and nothing on stdout; -list prints the mocks.
func TestSystemDetailsUsage(t *testing.T) {
	for _, args := range [][]string{
		{sdPrefix, "nope"},
		{sdPrefix, "screen-debian13", "-list"},
		{sdPrefix, "-width", "0"},
		{"play", "system-details/nope"},
		{"play", "-fail", "1", "system-details/debian13"},
		{"play", "-sequence", "failure", "system-details/debian13"},
	} {
		var out, errs bytes.Buffer
		read := false
		code := run(args, func() *prompt.Reader { read = true; return noInput() }, offTerminal, &out, &errs)
		if code != exitUsage || out.Len() != 0 || errs.Len() == 0 || read {
			t.Errorf("%v: exit %d, stdout %q, stderr %q, stdin read %t", args, code, out.String(), errs.String(), read)
		}
	}
	var out, errs bytes.Buffer
	if code := run([]string{sdPrefix, "-list"}, nil, offTerminal, &out, &errs); code != exitOK || !strings.Contains(out.String(), "screen-debian13\n") {
		t.Errorf("-list: exit %d, %q", code, out.String())
	}
}

// drawnLines is draw's lines at 80 columns and rows rows, with or without
// Unicode glyphs, in plain text.
func drawnLines(rows int, unicode bool, draw func(component.Ctx) string) []string {
	env := term.Fixed(80, colorprofile.TrueColor, unicode, true)
	env.Height = rows
	return strings.Split(ansi.Strip(draw(component.NewCtx(env))), "\n")
}

// TestSystemDetailsHeights: the screen on every host, at 80x24 and at 80x40,
// in Unicode and ASCII: each is as many lines at either height, the prompt on
// its last, longer than either height, its top scrolling off, as the main
// menu's does (DEV-052).
func TestSystemDetailsHeights(t *testing.T) {
	want := map[string]int{"debian13": 44, "ubuntu2604": 44, "alpine323": 43, "pve9": 44, "zfs-degraded": 44, "unreadable": 44,
		"no-logins": 44, "login-console": 44, "many": 78}
	if len(want) != len(sdHosts()) {
		t.Fatalf("%d heights, %d hosts", len(want), len(sdHosts()))
	}
	for _, sh := range sdHosts() {
		r, err := readSDHost(context.Background(), sh.name)
		if err != nil {
			t.Fatal(err)
		}
		for _, rows := range []int{24, 40} {
			for _, unicode := range []bool{true, false} {
				lines := drawnLines(rows, unicode, func(c component.Ctx) string { return r.subMenu().Render(c) })
				if len(lines) != want[sh.name] || !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
					t.Errorf("%s at 80x%d, unicode %v: %d lines, the last %q; want %d, the prompt", sh.name, rows, unicode, len(lines), lines[len(lines)-1], want[sh.name])
				}
			}
		}
	}
}

// readOnly are the commands the System Details and Logs readers run beyond
// those reads knows: they read and change nothing.
var readOnly = []string{"ip", "df", "zpool", "zfs", "lslogins"}

// assertOnlyReads fails t for anything h did but read: a file or directory
// changed, a command not known to read, or one the model does not answer.
func assertOnlyReads(t *testing.T, name string, h *fakeHost) {
	t.Helper()
	if got := writesAfter(h, 0); len(got) > 0 {
		t.Errorf("%s: changed the host: %q", name, got)
	}
	for _, c := range h.f.Calls() {
		if (c.Op == "run" || c.Op == "start-detached") && !reads(c.Argv) && !slices.Contains(readOnly, path.Base(c.Argv[0])) {
			t.Errorf("%s: ran %s", name, c)
		}
		if c.Op == "start-detached" || (c.Op == "run" && slices.Contains([]string{"less", "more", "most", "sh"}, path.Base(c.Argv[0]))) {
			t.Errorf("%s: ran a pager or started a process: %s", name, c)
		}
	}
	if u := h.unanswered(); len(u) > 0 {
		t.Errorf("%s: the host was asked what it does not model: %q", name, u)
	}
}

// TestSystemDetailsWritesNothing: on every host, live and under plan only,
// the screen drawn and Refresh chosen, write nothing and run no command but
// reads; no engine is built and no hold taken.
func TestSystemDetailsWritesNothing(t *testing.T) {
	for _, sh := range sdHosts() {
		for _, dryRun := range []bool{false, true} {
			h := sh.host()
			var lines []string
			for _, it := range readDetails(context.Background(), h.sys()).items() {
				lines = append(lines, it.Key)
			}
			lines = append(lines, "0")
			s, out, code := playWith(t, sdPrefix+"/"+sh.name, func(s *session) func() error {
				if dryRun {
					s.sequence = "dry-run"
				}
				return func() error { return s.sdScreen(context.Background(), sh, h) }
			}, lines...)
			if code != exitOK || len(s.engineRuns) != 0 || s.holds != 0 {
				t.Errorf("%s, plan only %t: exit %d, %d engines, the hold taken %d times:\n%s", sh.name, dryRun, code, len(s.engineRuns), s.holds, out)
			}
			assertOnlyReads(t, sh.name, h)
		}
	}
}

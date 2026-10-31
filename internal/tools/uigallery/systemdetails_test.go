package main

import (
	"bytes"
	"context"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine/details"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The System Details screen on the simulated hosts, and the listing's forms
// (systemdetails.go; slice 1, unit (c)). Nothing here runs a command or writes a file
// outside the fake. The screen's own tests are pkg/screens/systemdetails'.

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

// TestSystemDetailsGolden pins each listing form on the core set and under LANG=C:
// hardn system-details through the real Print, the JSON form and the flags.
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
		{"os", r.OS.Unread}, {"host", r.Host.Unread}, {"users", r.Users.Unread}, {"cpu", r.CPU.Unread},
		{"disk", r.Disk.Unread}, {"memory", r.Memory.Unread}, {"logins", r.Logins.Unread},
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
		{host: "alpine323", id: "alpine", kernel: alpineVMKernel, fs: "ext4", source: "/dev/vda1", tool: "doas", users: 1, addrs: 1, unread: []string{"logins"}},
		{host: "pve9", id: "debian", kernel: "6.17.2-1-pve", fs: "zfs", source: pveDataset, health: "ONLINE", addrs: 1, logins: 1},
		{host: "zfs-degraded", id: "debian", kernel: "6.17.2-1-pve", fs: "zfs", source: pveDataset, health: "DEGRADED", addrs: 1, logins: 1},
		{host: "unreadable", id: "debian", kernel: debianVMKernel, fs: "ext4", source: "/dev/vda1", tool: "sudo", users: 1, addrs: 1, logins: 2, unread: []string{"memory"}},
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

// TestSystemDetailsUsage: the listing and play refuse what they do not
// take, with status 2 and nothing on stdout; -list prints the mocks.
func TestSystemDetailsUsage(t *testing.T) {
	for _, args := range [][]string{
		{sdPrefix, "nope"},
		{sdPrefix, "cli-terminal", "-list"},
		{sdPrefix, "screen-debian13"},
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
	if code := run([]string{sdPrefix, "-list"}, nil, offTerminal, &out, &errs); code != exitOK || !strings.Contains(out.String(), "cli-terminal\n") {
		t.Errorf("-list: exit %d, %q", code, out.String())
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
		lessCheck := c.Op == "run" && slices.Equal(c.Argv, []string{"less", "--version"}) // logview.Pager's check, which shows nothing
		if lessCheck {
			continue
		}
		if (c.Op == "run" || c.Op == "start-detached") && !reads(c.Argv) && !slices.Contains(readOnly, path.Base(c.Argv[0])) {
			t.Errorf("%s: ran %s", name, c)
		}
		if c.Op == "start-detached" || c.Op == "run-interactive" || (c.Op == "run" && slices.Contains([]string{"less", "more", "most", "sh"}, path.Base(c.Argv[0]))) {
			t.Errorf("%s: ran a pager or started a process: %s", name, c)
		}
	}
	if u := h.unanswered(); len(u) > 0 {
		t.Errorf("%s: the host was asked what it does not model: %q", name, u)
	}
}

// TestSystemDetailsWritesNothing: on every host, live and under plan only,
// the screen drawn, Refresh chosen, write nothing and run no command but reads; no
// engine is built and no hold taken.
func TestSystemDetailsWritesNothing(t *testing.T) {
	for _, sh := range sdHosts() {
		for _, dryRun := range []bool{false, true} {
			h := sh.host()
			s, out, code := playWith(t, sdPrefix+"/"+sh.name, func(s *session) func() error {
				if dryRun {
					s.sequence = "dry-run"
				}
				return func() error { return s.sdScreen(context.Background(), sh, h) }
			}, "1", "0")
			if code != exitOK || len(s.engineRuns) != 0 || s.holds != 0 {
				t.Errorf("%s, plan only %t: exit %d, %d engines, the hold taken %d times:\n%s", sh.name, dryRun, code, len(s.engineRuns), s.holds, out)
			}
			assertOnlyReads(t, sh.name, h)
		}
	}
}

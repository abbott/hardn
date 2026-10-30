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
// leaving out those narrower than the mock needs, with a cell of its own
// for one wider than any of them.
func listCells(m listMock) []uitest.Cell {
	var cells []uitest.Cell
	for _, c := range append(uitest.Core(), asciiColour) {
		if c.Env.Width >= m.wide {
			cells = append(cells, c)
		}
	}
	if m.wide > term.DefaultWidth {
		cells = append(cells, uitest.Cell{Name: "truecolor-dark-unicode-132", Env: term.Fixed(m.wide, colorprofile.TrueColor, true, true)})
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
// LANG=C: the screen on each host and in each treatment, each treatment's
// item opened, plan only, the non-interactive forms and origin/main's
// report.
func TestSystemDetailsGolden(t *testing.T) { goldenListing(t, sdListing) }

// TestPlaySystemDetailsGolden plays the reading end to end and pins each
// transcript: from the main menu's 9, on Debian 13, Refresh and leaving;
// the same under plan only; on each release; one section unreadable; the
// sections treatment, opening one; the pager treatment, its item saying
// what it would run; each Export treatment, under plan only too; and the
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
		{"system-details-sections", []string{"system-details/report-sections"}, []string{"5", "8", "0"}, nil},
		{"system-details-pager", []string{"system-details/report-pager"}, []string{"1", "0"}, nil},
		{"system-details-export-command", []string{"system-details/export-command"}, []string{"2", "0"}, nil},
		{"system-details-export-file", []string{"system-details/export-file"}, []string{"2", "0"}, nil},
		{"system-details-export-file-plan-only", []string{"-sequence", "dry-run", "system-details/export-file"}, []string{"2", "0"}, nil},
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
	for _, tt := range tests {
		r, err := readSDHost(context.Background(), tt.host)
		if err != nil {
			t.Fatal(err)
		}
		var unread []string
		for s := range r.unread {
			unread = append(unread, s)
		}
		switch {
		case r.facts.OS.ID != tt.id || r.kernel != tt.kernel:
			t.Errorf("%s: os %q, kernel %q", tt.host, r.facts.OS.ID, r.kernel)
		case r.disk.Type != tt.fs || r.disk.Source != tt.source || r.disk.Health != tt.health:
			t.Errorf("%s: disk %+v", tt.host, r.disk)
		case len(r.users) != tt.users || len(r.addrs) != tt.addrs:
			t.Errorf("%s: %d users, %d addresses", tt.host, len(r.users), len(r.addrs))
		case tt.users > 0 && r.users[0].Tool != tt.tool:
			t.Errorf("%s: %s's tool %q, want %q", tt.host, r.users[0].Name, r.users[0].Tool, tt.tool)
		case len(r.logins) != tt.logins:
			t.Errorf("%s: %d logins, want %d", tt.host, len(r.logins), tt.logins)
		case !slices.Equal(unread, tt.unread):
			t.Errorf("%s: unread %q, want %q", tt.host, unread, tt.unread)
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

// TestSDLastLogin holds the rows to lslogins' answers: root first, then the
// users; a login with a host, a dotless one included, and with none; another
// year's; blanks. An answer for another name, a garbled one or a failure says it
// could not read it, never no login, and a host with no lslogins says that.
func TestSDLastLogin(t *testing.T) {
	const at = "2026-10-20T11:42:05+00:00"
	cant := []component.KeyValue{{Key: "Can't read lslogins", State: component.ValueWarn}}
	none := component.KeyValue{Key: "root", Value: noLogin, State: component.ValueNone}
	alice := func(value string) []component.KeyValue {
		return []component.KeyValue{none, {Key: "alice", Value: value}}
	}
	for _, tt := range []struct {
		name string
		res  hostfake.Response
		want []component.KeyValue
	}{
		{"a host", hostfake.Response{Stdout: "alice " + at + " pts/0 192.0.2.10\n"}, alice("Oct 20 11:42 from 192.0.2.10")},
		{"a dotless host", hostfake.Response{Stdout: "alice " + at + " pts/0 bastion\n"}, alice("Oct 20 11:42 from bastion")},
		{"no host", hostfake.Response{Stdout: "alice " + at + " tty1 \n"}, alice("Oct 20 11:42 on tty1")},
		{"all blank", hostfake.Response{Stdout: "alice   \n"}, []component.KeyValue{none, {Key: "alice", Value: noLogin, State: component.ValueNone}}},
		{"another year", hostfake.Response{Stdout: "alice 2025-12-31T23:59:00+00:00 pts/0 192.0.2.10\n"}, alice("Dec 31 2025 from 192.0.2.10")},
		{"another name", hostfake.Response{Stdout: "alice-long " + at + " pts/0 192.0.2.10\n"}, cant},
		{"garbled time", hostfake.Response{Stdout: "alice yesterday pts/0 192.0.2.10\n"}, cant},
		{"too few fields", hostfake.Response{Stdout: "alice " + at + " pts/0\n"}, cant},
		{"a terminal and no time", hostfake.Response{Stdout: "alice  pts/0 \n"}, cant},
		{"nothing", hostfake.Response{}, cant},
		{"a failure", hostfake.Response{Stderr: "lslogins: cannot open\n", ExitCode: 1}, cant},
		{"not found", hostfake.Response{Err: host.ErrNotFound}, []component.KeyValue{{Key: rowLogins, Value: noLslogins, State: component.ValueUnknown}}},
	} {
		sh := sdHostNamed(t, "debian13")
		h := sh.host()
		model := h.f.Handler
		h.f.Handler = func(argv []string, stdin []byte) hostfake.Response {
			if argv[0] == "lslogins" && argv[len(argv)-1] == "alice" {
				return tt.res
			}
			return model(argv, stdin)
		}
		if got := readDetails(context.Background(), h.sys(), sh).sectionRows(secLogin); !slices.Equal(got, tt.want) {
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
}

// TestSDReadsAddrs: readAddrs takes the forms the VMs print, Debian's and
// Ubuntu's with a metric, Alpine's with noprefixroute and none (vm-runs/s1,
// 01-tools.log), and the fixtures print those forms.
func TestSDReadsAddrs(t *testing.T) {
	for _, tt := range []struct {
		name string
		host func() *fakeHost
		line string
	}{
		{"debian13", details(debian13Platform), "2: eth0    inet 192.0.2.20/24 metric 100 brd 192.0.2.255 scope global dynamic eth0\\       valid_lft 7104sec preferred_lft 7104sec\n"},
		{"alpine323", details(alpine323Platform), "2: eth0    inet 192.0.2.20/24 brd 192.0.2.255 scope global dynamic noprefixroute eth0\\       valid_lft 5235sec preferred_lft 4411sec\n"},
	} {
		res, err := tt.host().f.Run(context.Background(), "ip", "-o", "addr", "show", "scope", "global")
		if err != nil || string(res.Stdout) != tt.line {
			t.Errorf("%s: %q, %v; want %q", tt.name, res.Stdout, err, tt.line)
		}
		r, err := readSDHost(context.Background(), tt.name)
		if err != nil {
			t.Fatal(err)
		}
		if want := (detailAddr{Iface: "eth0", Family: "inet", Addr: "192.0.2.20/24"}); len(r.addrs) != 1 || r.addrs[0] != want {
			t.Errorf("%s: %+v", tt.name, r.addrs)
		}
	}
}

// TestSDBlankCPU: where /proc/cpuinfo has no model name or physical id, as on
// arm64 [I, not run], the row says so and no socket count is made up.
func TestSDBlankCPU(t *testing.T) {
	r := detailsReading{cpu: detailCPU{CPUs: 4}}
	got := r.sectionRows(secCPU)
	if got[0].Value != notKnown || got[0].State != component.ValueUnknown || got[1].Value != "4" {
		t.Errorf("%+v", got)
	}
}

// TestSDNotRootNamesTheRules: the row names the file the host's tool reads.
func TestSDNotRootNamesTheRules(t *testing.T) {
	for p, want := range map[*platform]string{&debian13Platform: "/etc/sudoers.d", &alpine323Platform: "/etc/doas.conf"} {
		sh := sdHost{name: "not-root", host: details(*p), notRoot: true}
		rows := readDetails(context.Background(), sh.host().sys(), sh).sectionRows(secUsers)
		if got := rows[len(rows)-1].Key; !strings.Contains(got, want) {
			t.Errorf("%s: %q", want, got)
		}
	}
}

// TestSDUnreadableKeepsTheRest: one section not read leaves the other six,
// where origin/main loses the whole report (details.go:91-117): the section
// says which file, and under export-file Export is not offered, as origin/
// main's is not (system_details_menu.go:75-79).
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
	r.sh.export = exportFile
	if slices.ContainsFunc(r.items(), func(it sdItem) bool { return it.action == itemExport }) {
		t.Error("export-file offers Export with a section unread")
	}
	r.sh.export = exportCommand
	if !slices.ContainsFunc(r.items(), func(it sdItem) bool { return it.action == itemExport }) {
		t.Error("export-command does not offer Export with a section unread")
	}
}

// TestSDZFSDatasetIsRead: the stand-in asks zfs about the dataset mounted
// on /, never origin/main's hard-coded zroot/ROOT/os (details.go:85).
func TestSDZFSDatasetIsRead(t *testing.T) {
	sh := sdHostNamed(t, "pve9")
	h := sh.host()
	readDetails(context.Background(), h.sys(), sh)
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

// TestSDJSONIsTheDocument: the proposed JSON form is pkg/output's document,
// its nine fields as they are, and one more, details.
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

// TestSystemDetailsHeights: the screen on every host and in each treatment,
// at 80x24 and at 80x40, in Unicode and ASCII: each is as many lines at
// either height, the prompt on its last. Treatment report-whole is longer
// than either height, its top scrolling off, as the main menu's does
// (DEV-052); report-sections and report-pager fit 24 rows.
func TestSystemDetailsHeights(t *testing.T) {
	want := map[string]int{"debian13": 44, "ubuntu2604": 44, "alpine323": 43, "pve9": 44, "zfs-degraded": 44, "unreadable": 44,
		"no-logins": 44, "login-console": 44, "many": 78, reportSections: 16, reportPager: 20, exportCommand: 45, exportFile: 45}
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
				fits := sh.report == "" || len(lines) <= 24
				if len(lines) != want[sh.name] || !fits || !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
					t.Errorf("%s at 80x%d, unicode %v: %d lines, the last %q; want %d, the prompt", sh.name, rows, unicode, len(lines), lines[len(lines)-1], want[sh.name])
				}
			}
		}
	}
}

// readOnly are the commands the System Details and Logs stand-ins run beyond
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
// the screen drawn and every item chosen, Refresh and Export among them,
// write nothing and run no command but reads; no engine is built and no hold
// taken. Export's treatment export-file says what it would write and the
// gallery does not write it; a pager is said, not run.
func TestSystemDetailsWritesNothing(t *testing.T) {
	for _, sh := range sdHosts() {
		for _, dryRun := range []bool{false, true} {
			h := sh.host()
			var lines []string
			for _, it := range readDetails(context.Background(), h.sys(), sh).items() {
				lines = append(lines, it.item.Key)
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

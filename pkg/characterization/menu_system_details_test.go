package characterization

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	hardncmd "github.com/abbott/hardn/pkg/cmd"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/infrastructure"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/system"
)

// The System Details screen (main menu 9), refactor plan slice 1's
// evidence: each of its options on a script, in a child process of this test binary, as
// TestLegacyMenuBackup and TestLegacyMenuEnvironment ran theirs. The
// report has three sources, and each is handled as it can be:
//
//   - The HostInfoManager is on the seams: the mocks, recorded, with the
//     host of TestHostInfoGolden.
//   - What pkg/system runs itself (nproc, lscpu, free, lastlog, zfs,
//     zpool) comes from PATH, which holds only stand-ins: this test binary
//     under each name (init below), recording how it was run and answering
//     as the case says. A tool the case does not give is not on PATH.
//   - The rest has no seam and reads the machine the test runs on:
//     /etc/os-release (pkg/system/utils.go:16), os.Hostname and
//     net.Interfaces (os_host_info_repository.go:123, :210), user.Current
//     (collectors.go:65, :498), gopsutil's load.Avg (collectors.go:278).
//     Those lines are masked, whole and said to be (hostMask), so the
//     goldens hold no value from this machine. Nothing here shows what they
//     read on a real host: the renderer's cases give all of them
//     (system_details_render*), on a synthetic SystemDetails.
//
// The screen is built as main_menu.go:712-714 builds it, the constructor
// with the same arguments; which screen key 9 opens is
// pkg/menu's main_menu_layout_test.go's.

// The tools' stand-in: in a child's PATH, this test binary under a tool's
// name, which, with toolLog set, appends its argv there and answers from
// toolAnswers, before the testing package parses its arguments.
const (
	toolLog     = "HARDN_CHARACTERIZATION_TOOL_LOG"
	toolAnswers = "HARDN_CHARACTERIZATION_TOOL_ANSWERS"
	sdChild     = "HARDN_CHARACTERIZATION_SYSTEM_DETAILS"
	sdFlags     = "HARDN_CHARACTERIZATION_SYSTEM_DETAILS_FLAGS"
)

// toolAnswer is what a stand-in prints and how it exits. Answers are keyed
// by the whole command line, else by the tool's name.
type toolAnswer struct {
	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`
	Exit   int    `json:"exit,omitempty"`
}

// standInTools are the names the stand-in answers to.
var standInTools = []string{"nproc", "lscpu", "lastlog", "free", "zfs", "zpool", "less"}

func init() {
	name := filepath.Base(os.Args[0])
	if os.Getenv(toolLog) == "" || !slices.Contains(standInTools, name) {
		return
	}
	line := formatArgv(append([]string{name}, os.Args[1:]...))
	f, err := os.OpenFile(os.Getenv(toolLog), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = fmt.Fprintln(f, line)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(99)
	}
	answers := map[string]toolAnswer{}
	if err := json.Unmarshal([]byte(os.Getenv(toolAnswers)), &answers); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(99)
	}
	a, ok := answers[strings.Join(append([]string{name}, os.Args[1:]...), " ")]
	if !ok {
		a = answers[name]
	}
	fmt.Print(a.Stdout)
	fmt.Fprint(os.Stderr, a.Stderr)
	os.Exit(a.Exit)
}

// sdTools is the host every case starts from, but what over changes: a nil
// answer takes the tool off PATH. ZFS is not there.
func sdTools(over map[string]*toolAnswer) map[string]toolAnswer {
	tools := map[string]toolAnswer{
		"nproc":   {Stdout: "2\n"},
		"lscpu":   {Stdout: "Architecture:        x86_64\nSocket(s):           1\nHypervisor vendor:   KVM\n"},
		"lastlog": {Stdout: "Username         Port     From             Latest\nalice            pts/0    192.0.2.10       Thu Oct 29 08:12:44 +0000 2026\n"},
	}
	for k, a := range over {
		if a == nil {
			delete(tools, k)
		} else {
			tools[k] = *a
		}
	}
	return tools
}

// sdCase is one run of the System Details screen, or of its renderer.
type sdCase struct {
	name, screen, script string
	tools                map[string]*toolAnswer
	env                  map[string]string // set in the child
	seeds                map[string]string // files in the working directory before
	noMeminfo, dryRun    bool
	render               *system.SystemDetails // the renderer alone, on this
	notes, defects       []string
}

// sdCases are the screen's options: open, 0, q, 1, 2 (the default name, a
// name given, over a file there, a write that fails, in dry run), an
// invalid key; the collector error, and 2 under it; the tools the report
// reads missing or answering otherwise; and the renderer on a synthetic
// report.
func sdCases() []sdCase {
	const (
		recurse    = "Each option but 0 and q draws the screen again by calling itself (system_details_menu.go:102, :115, :128), one call deeper each time."
		clears     = "The screen clears itself on every draw (system_details_menu.go:38; utils.go:24-25), the report and all above it gone from the terminal's screen."
		noFreq     = "\"CPU Freq: 0.00 GHz\": collectCPUInfo sets the frequency only where the host-info service gave no CPU model or nproc failed (collectors.go:199-205, :231, :246-252), so with both, the usual case, it is 0.00 on every host [I, not run on a host]."
		memFree    = "\"Memory: 3.89/7.78 GiB\" counts MemFree as all that is free (used = total - free, collectors.go:329), with MemAvailable 5.83 GiB in this /proc/meminfo (host_info/debian.golden holds MemoryFree): page cache reads as used."
		gbNotGB    = "Disk sizes are bytes / 1024^3 labelled \"GB\" (collectors.go:430-432, display.go:104), where memory, the same division, says \"GiB\" (:370, :112)."
		worldRead  = "The export holds the users and addresses, mode 0644 (utils.go:139): as root, a file anyone can read, in hardn's working directory."
		relative   = "The name is relative to the working directory (system_details_menu.go:137-139, :231): no prompt says which directory that is; the message repeats the name as typed."
		noExportAs = "The export is its own text, built apart from the screen's (system_details_menu.go:142-222; display.go:42-125), so the two differ: the export has headings, the root partition and the free sizes; the screen has the bars."
		reads      = "Everything the screen reads is read again on each draw, four GetHostInfo's among them: collectOSInfo, collectCPUInfo, collectMemoryInfo, collectDiskInfo each call it (collectors.go:35, :198, :324, :380), counted below."
		zfsDefault = "The dataset is the hard-coded default zroot/ROOT/os (details.go:85), asked of zfs below; nothing reads the host's root dataset or a setting."
		zfsHealthy = "zpool status -x with no pool named says \"all pools are healthy\" on a healthy host [I, zpool(8), not run here]: the check looks for \"is healthy\" (collectors.go:386), which that text does not hold, so a healthy ZFS host reads as no ZFS, as this golden holds."
		zfsReach   = "The stand-in answers \"pool 'zroot' is healthy\", words zpool prints for a pool named on its command line [I, zpool(8), not run here], not for this one's, to reach the branch: by zfsHealthy it may never run on a real host (system_details_zfs_all_healthy)."
		errOne     = "One collector's error loses the report: GenerateSystemStatus returns nil at the first (details.go:91-117), so everything already read is dropped, here all of OS, network, users and CPU for want of memory. collectOSInfo and collectCPUInfo never return one, what they fail to read left blank (collectors.go:26, :48, :66, :279); the network, user, memory, disk and login collectors can (collectors.go:99, :340, :451, :500; user_info.go:24)."
		errReach   = "/proc/meminfo unreadable and free not on PATH give the error (collectors.go:337-341). The same run with free there has it answer."
	)
	long := &toolAnswer{Stdout: "all pools are healthy\n"}
	named := &toolAnswer{Stdout: "pool 'zroot' is healthy\n"}
	render := system.SystemDetails{
		OSName: "Debian", OSVersion: "13 Trixie", Kernel: "Linux 6.12.0-example-amd64",
		Hostname: "web-01", Domain: "example.internal", CurrentUser: "root",
		ClientIP: "192.0.2.10", IPAddresses: []string{"192.0.2.20", "198.51.100.20"}, DNSServers: []string{"192.0.2.53", "192.0.2.54"},
		Users:    []model.User{{Username: "alice", HasSudo: true}, {Username: "bob"}},
		CPUModel: "Example Corporation Example Processor 9000 64-Core Processor @ 2.00GHz", CPUCores: 64, CPUSockets: 2, CPUFrequency: 2, CPUHypervisor: "Bare Metal",
		LoadAvg1: 3.2, LoadAvg5: 1.6, LoadAvg15: 0.8,
		LoadAvg1Graph: "█░░░░░░░░░░░░░░░░░░░", LoadAvg5Graph: "░░░░░░░░░░░░░░░░░░░░", LoadAvg15Graph: "░░░░░░░░░░░░░░░░░░░░",
		MemoryTotalGB: 62.5, MemoryUsedGB: 31.25, MemoryFreeGB: 31.25, MemoryPercent: 50, MemoryGraphUsed: "██████████░░░░░░░░░░",
		RootPartition: "/", RootTotalGB: 80, RootUsedGB: 20, RootFreeGB: 60, DiskPercent: 25, DiskGraphUsed: "█████░░░░░░░░░░░░░░░",
		LastLoginPresent: true, LastLoginTime: "Oct 29 08:12:44 +0000", LastLoginIP: "192.0.2.10", UptimeLongFormat: "3 days, 4 hours, 5 minutes",
	}
	zfs := render
	zfs.ZFSPresent, zfs.ZFSFilesystem, zfs.ZFSHealth = true, "zroot/ROOT/os", "HEALTH O.K."
	zfs.ZFSUsedGB, zfs.ZFSAvailableGB, zfs.ZFSPercent, zfs.DiskGraphUsed = 12, 88, 12, "██░░░░░░░░░░░░░░░░░░"
	return []sdCase{
		{name: "system_details_open", screen: "System Details (main menu 9), SSH_CLIENT set, q", script: "q\n",
			env:     map[string]string{"SSH_CLIENT": "192.0.2.10 50000 22"},
			defects: []string{noFreq, memFree, gbNotGB},
			notes:   []string{clears, reads, "No section headings: blank lines alone part the report (display.go:46-49 and so on), two of them each time. The title prints twice, the box's top line and its shade (display.go:36; boxes.go:194-292)."}},
		{name: "system_details_return", screen: "System Details (main menu 9), 0", script: "0\n"},
		{name: "system_details_refresh", screen: "System Details (main menu 9), 1 (Refresh Information), then q", script: "1\nq\n",
			notes: []string{recurse, clears, reads}},
		{name: "system_details_invalid", screen: "System Details (main menu 9), 7 (no such option), then q", script: "7\n\nq\n",
			notes: []string{recurse}},
		{name: "system_details_export_default", screen: "System Details (main menu 9), 2 (Export Information), the default name, then q", script: "2\n\n\nq\n",
			defects: []string{relative, worldRead},
			notes:   []string{noExportAs, recurse}},
		{name: "system_details_export_named", screen: "System Details (main menu 9), 2, the name reports/web-01.txt, then q", script: "2\nreports/web-01.txt\n\nq\n",
			notes: []string{"The directory the name needs is created (utils.go:131-136), mode 0755.", relative}},
		{name: "system_details_export_overwrite", screen: "System Details (main menu 9), 2, the default name with a file by that name there, then q", script: "2\n\n\nq\n",
			seeds:   map[string]string{"system_status.txt": "alice's notes\n"},
			defects: []string{"A file by that name is overwritten with no question and no backup (system_details_menu.go:231; utils.go:139)."}},
		{name: "system_details_export_fails", screen: "System Details (main menu 9), 2, a name whose directory is a file, then q", script: "2\nreports/web-01.txt\n\nq\n",
			seeds: map[string]string{"reports": "a file\n"},
			notes: []string{"The write's failure is printed and the screen goes on (system_details_menu.go:232-235): nothing is retried and the name is not asked again."}},
		{name: "system_details_export_dry_run", screen: "System Details (main menu 9) in dry-run mode, 2, the default name, then q", script: "2\n\n\nq\n", dryRun: true,
			notes: []string{"Under dry run the export prints this line and writes nothing (system_details_menu.go:225-228; docs/engine/gallery-dryrun.md:61)."}},
		{name: "system_details_error", screen: "System Details (main menu 9), the memory reader failing, q", script: "q\n", noMeminfo: true,
			defects: []string{errOne, "The error is printed twice, above the box and in it; the box says to check permissions, whatever the error (system_details_menu.go:43-44, :59-62)."},
			notes:   []string{errReach, "Item 2 is not offered (system_details_menu.go:75-79); 1 is."}},
		{name: "system_details_error_export", screen: "System Details (main menu 9), the memory reader failing, 2 (not offered), then q", script: "2\n\nq\n", noMeminfo: true,
			notes: []string{"2 is still taken: the refusal is its own message (system_details_menu.go:109-114).", errReach}},
		{name: "system_details_no_lastlog", screen: "System Details (main menu 9), lastlog not on PATH, q", script: "q\n",
			tools:   map[string]*toolAnswer{"lastlog": nil},
			defects: []string{"With no lastlog, \"Last Login:\" is left blank: collectLoginInfo drops the error (collectors.go:505-507). shadow's lastlog is deprecated and gone from some current distributions [I, not run here]; pkg/adapter/secondary/last_command_adapter.go, on last, is not what this report uses (details.go:87)."}},
		{name: "system_details_never_logged_in", screen: "System Details (main menu 9), lastlog says Never logged in, q", script: "q\n",
			tools: map[string]*toolAnswer{"lastlog": {Stdout: "Username         Port     From             Latest\nalice                                      **Never logged in**\n"}}},
		{name: "system_details_login_hostname", screen: "System Details (main menu 9), lastlog's From a host name with no dot, q", script: "q\n",
			tools:   map[string]*toolAnswer{"lastlog": {Stdout: "Username         Port     From             Latest\nalice            pts/0    laptop           Thu Oct 29 08:12:44 +0000 2026\n"}},
			defects: []string{"A From with no dot or colon is taken for the date's first word: the parse fails (lastlog_command_adapter.go:91-96, :111-122), and \"Last Login:\" is left blank as for no lastlog."}},
		{name: "system_details_no_lscpu", screen: "System Details (main menu 9), lscpu not on PATH, q", script: "q\n",
			tools:   map[string]*toolAnswer{"lscpu": nil},
			defects: []string{"With no lscpu the hypervisor reads \"Bare Metal\": not known reads as a fact (collectors.go:273-275); sockets read 1 the same way (:314-316). lscpu is util-linux's, not in a stock Alpine [I, not run here]."}},
		{name: "system_details_zfs_all_healthy", screen: "System Details (main menu 9), zfs and zpool on PATH, zpool saying all pools are healthy, q", script: "q\n",
			tools:   map[string]*toolAnswer{"zfs": {}, "zpool": long},
			defects: []string{zfsHealthy}},
		{name: "system_details_zfs_dataset", screen: "System Details (main menu 9), zpool saying zroot is healthy, the default dataset there, q", script: "q\n",
			tools: map[string]*toolAnswer{"zpool": named,
				"zfs get -Hp available zroot/ROOT/os": {Stdout: "zroot/ROOT/os\tavailable\t94489280512\t-\n"},
				"zfs get -Hp used zroot/ROOT/os":      {Stdout: "zroot/ROOT/os\tused\t12884901888\t-\n"}},
			defects: []string{zfsDefault, "\"Volume: 12.00/88.00 GB\" is used/available, not used/total as without ZFS (display.go:99-100, :104-105)."},
			notes:   []string{zfsReach}},
		{name: "system_details_zfs_other_dataset", screen: "System Details (main menu 9), zpool saying rpool is healthy, the default dataset not there, q", script: "q\n",
			tools:   map[string]*toolAnswer{"zpool": {Stdout: "pool 'rpool' is healthy\n"}, "zfs": {Stderr: "cannot open 'zroot/ROOT/os': dataset does not exist\n", Exit: 1}},
			defects: []string{zfsDefault + " With another dataset the errors are dropped (collectors.go:393, :404): 0.00/0.00 GB and \"HEALTH O.K.\", the root volume not read (:420)."},
			notes:   []string{zfsReach}},
		{name: "system_details_export_zfs", screen: "System Details (main menu 9), zpool saying zroot is healthy, 2, the default name, then q", script: "2\n\n\nq\n",
			tools: map[string]*toolAnswer{"zpool": named,
				"zfs get -Hp available zroot/ROOT/os": {Stdout: "zroot/ROOT/os\tavailable\t94489280512\t-\n"},
				"zfs get -Hp used zroot/ROOT/os":      {Stdout: "zroot/ROOT/os\tused\t12884901888\t-\n"}},
			notes: []string{zfsReach, "The export says \"ZFS Filesystem: zroot/ROOT/os\", the default (system_details_menu.go:204)."}},
		{name: "system_details_render", screen: "system.DisplayMachineStatus on a synthetic report (pkg/system/display.go:30)", render: &render,
			defects: []string{"The box is fixed at 50 columns (display.go:32) whatever the terminal: a longer line runs past it, its padding dropped (boxes.go:560-563), here the Processor line, and nothing wraps or cuts it.", gbNotGB},
			notes:   []string{"DisplayMachineStatus prints (fmt.Println, boxes.go:584) as it builds: it returns nothing a caller could place or test.", "The bars are 20 wide (display.go:14-25), and a load above the core count is cut at full (display.go:135-137). Here they are as generateGraphs would make them: it is unexported, so the fixture holds the strings."}},
		{name: "system_details_render_zfs", screen: "system.DisplayMachineStatus on a synthetic report with ZFS (pkg/system/display.go:98-102)", render: &zfs,
			notes: []string{"\"Volume\" is used/available here (display.go:99-100), used/total without ZFS (:104-105): 12.00/88.00 reads as 13.6%, beside the 12.00% it prints."}},
	}
}

// hostMask replaces, whole, each line that holds a value from the machine
// the test runs on, in a screen or an export: OS, Hostname, User, the Load
// lines and the list of addresses (one line for however many); Domain, which
// is there only where the machine's name has a dot, goes.
func hostMask(s string) string {
	const masked = "<read from the host>"
	var out []string
	inAddresses := false
	for _, line := range strings.Split(s, "\n") {
		text := strings.TrimRight(line, " ")
		if inAddresses {
			if strings.HasPrefix(text, "- ") {
				continue
			}
			inAddresses = false
		}
		switch {
		case text == "IP Addresses:":
			out = append(out, line, "- "+masked)
			inAddresses = true
		case strings.HasPrefix(text, "OS:"):
			out = append(out, "OS: "+masked)
		case strings.HasPrefix(text, "Hostname:"):
			out = append(out, "Hostname: "+masked)
		case strings.HasPrefix(text, "User:"):
			out = append(out, "User: "+masked)
		case strings.HasPrefix(text, "Domain:"):
		case strings.HasPrefix(text, "Load 1m:"), strings.HasPrefix(text, "Load 5m:"), strings.HasPrefix(text, "Load 15m:"):
			label, _, _ := strings.Cut(text, ":")
			out = append(out, label+": "+masked)
		default:
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// listDir describes the files under dir, each with its mode and, masked,
// its lines; "(none)" when there are none. The directories are named too.
func listDir(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			fmt.Fprintf(&b, "#   %s/ mode=%s\n", rel, mode(info.Mode()))
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "#   %s mode=%s\n", rel, mode(info.Mode()))
		for _, line := range strings.Split(strings.TrimSuffix(hostMask(string(data)), "\n"), "\n") {
			b.WriteString("#     | " + line + "\n")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.Len() == 0 {
		return "#   (none)\n"
	}
	return b.String()
}

// TestLegacyMenuSystemDetails records origin/main's System Details screen,
// each option in a child process of this test binary whose stdin is the
// script, not a terminal, and whose working directory and HOME are temp
// dirs, as TestLegacyMenuDryRun does. Each golden holds the screen as
// printed (ESC as \x1b), the mocks' and the tools' calls, and the working
// directory as the screen left it. The pauses say "Press Enter", where
// origin/main says "Press any key" (DEV-050); nothing else differs from
// origin/main in system_details_menu.go, pkg/system and the adapters they
// use.
func TestLegacyMenuSystemDetails(t *testing.T) {
	if name := os.Getenv(sdChild); name != "" {
		runSystemDetailsChild(name)
		os.Exit(0)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range sdCases() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin, cwd, logPath := filepath.Join(dir, "bin"), filepath.Join(dir, "cwd"), filepath.Join(dir, "tools.log")
			for _, d := range []string{bin, cwd} {
				if err := os.Mkdir(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for name, content := range tc.seeds {
				seed := filepath.Join(cwd, name)
				if err := os.WriteFile(seed, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				// The golden records the seed's mode, which the runner's umask
				// would otherwise decide.
				if err := os.Chmod(seed, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			before := listDir(t, cwd)

			tools := sdTools(tc.tools)
			answers, err := json.Marshal(tools)
			if err != nil {
				t.Fatal(err)
			}
			var onPath []string
			for key := range tools {
				name, _, _ := strings.Cut(key, " ")
				if !slices.Contains(onPath, name) {
					onPath = append(onPath, name)
				}
			}
			sort.Strings(onPath)
			for _, name := range onPath {
				if err := os.Symlink(exe, filepath.Join(bin, name)); err != nil {
					t.Fatal(err)
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestLegacyMenuSystemDetails$")
			var env []string
			for _, kv := range os.Environ() {
				k, _, _ := strings.Cut(kv, "=")
				switch k {
				case "PATH", "HOME", "TZ", "SSH_CLIENT", "SSH_CONNECTION", "SUDO_USER", "SUDO_UID", "HARDN_CONFIG", "DOAS_USER":
				default:
					env = append(env, kv)
				}
			}
			for k, v := range tc.env {
				env = append(env, k+"="+v)
			}
			cmd.Env = append(env, sdChild+"="+tc.name, toolLog+"="+logPath, toolAnswers+"="+string(answers),
				"PATH="+bin, "HOME="+filepath.Join(dir, "home"), "TZ=UTC", "NO_COLOR=", "RUNEWIDTH_EASTASIAN=0")
			cmd.Dir = cwd
			cmd.Stdin = strings.NewReader(tc.script)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("child: %v\n%s", err, out)
			}
			screen, trailer, _ := strings.Cut(string(out), childTrailer)
			asked, err := os.ReadFile(logPath)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				t.Fatal(err)
			}

			var b strings.Builder
			b.WriteString(baselineHeader + "\n")
			b.WriteString("# Generated by TestLegacyMenuSystemDetails (pkg/characterization); regenerate with -update. Do not edit.\n")
			if tc.render != nil {
				fmt.Fprintf(&b, "# Renderer: %s, run in a child process\n", tc.screen)
				sgr := regexp.MustCompile(`\x1b\[[0-9;]*m`)
				widest := 0
				for _, line := range strings.Split(screen, "\n") {
					widest = max(widest, utf8.RuneCountInString(sgr.ReplaceAllString(line, "")))
				}
				fmt.Fprintf(&b, "# Widest line printed: %d columns\n", widest)
			} else {
				fmt.Fprintf(&b, "# Screen: %s (pkg/menu), run in a child process\n", tc.screen)
				fmt.Fprintf(&b, "# Script: stdin %q, not a terminal, so nothing typed is echoed\n", tc.script)
				fmt.Fprintf(&b, "# Tools on PATH (stand-ins): %s\n", strings.Join(onPath, ", "))
				fmt.Fprintf(&b, "# dryRun %t; TZ=UTC\n", tc.dryRun)
				b.WriteString("# Masked, read from the machine the test runs on, no seam: the OS, Hostname, User and Load lines, the addresses (one line for however many), and Domain, dropped. Their padding goes with them.\n")
				fmt.Fprintf(&b, "# Pauses: %d (\"Press Enter to ...\"; origin/main says \"Press any key to ...\", DEV-050)\n", strings.Count(screen, "Press Enter to"))
			}
			for _, d := range tc.defects {
				fmt.Fprintf(&b, "# KNOWN-DEFECT: %s\n", d)
			}
			for _, n := range tc.notes {
				fmt.Fprintf(&b, "# NOTE: %s\n", n)
			}
			b.WriteString("# The screen as printed, ESC as the four characters \\x1b, each line ending in $; <no-newline> marks text with no final newline.\n")
			b.WriteString("# ----\n")
			if tc.render != nil {
				b.WriteString(visible(screen))
			} else {
				b.WriteString(visible(hostMask(screen)))
			}
			b.WriteString("# ----\n")
			if tc.render == nil {
				b.WriteString(trailer)
				b.WriteString("# The tools the screen ran itself, in order:\n")
				if len(asked) == 0 {
					b.WriteString("#   (none)\n")
				}
				for _, line := range strings.Split(strings.TrimSuffix(string(asked), "\n"), "\n") {
					if line != "" {
						b.WriteString("#   " + strings.Replace(line, "lastlog -u "+current.Username, "lastlog -u <the test's user>", 1) + "\n")
					}
				}
				b.WriteString("# The working directory before:\n" + before)
				b.WriteString("# The working directory afterwards:\n" + listDir(t, cwd))
			}
			got := b.String()
			for _, d := range []string{dir, cwd} {
				if strings.Contains(got, d) {
					t.Fatalf("golden would contain the temp dir %s", d)
				}
			}
			assertGolden(t, "menu/"+tc.name+".golden", got)
		})
	}
}

// runSystemDetailsChild runs the screen of case name, or its renderer, in
// the child process: hardn's defaults for alice on port 2230, the
// HostInfoManager as infrastructure.ServiceFactory builds it, over the
// mocks of TestHostInfoGolden. Then it reports, after childTrailer, what
// the mocks were asked.
func runSystemDetailsChild(name string) {
	var tc sdCase
	for _, c := range sdCases() {
		if c.name == name {
			tc = c
		}
	}
	if tc.name == "" {
		fmt.Fprintf(os.Stderr, "no system details case %q\n", name)
		os.Exit(2)
	}
	syscall.Umask(0o022)
	if tc.render != nil {
		system.DisplayMachineStatus(tc.render)
		return
	}

	fsys, mc := interfaces.NewMockFileSystem(), interfaces.NewMockCommander()
	fsys.Files["/etc/os-release"] = []byte("PRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\nNAME=\"Debian GNU/Linux\"\nVERSION_ID=\"13\"\nVERSION=\"13 (trixie)\"\nVERSION_CODENAME=trixie\nID=debian\n")
	fsys.Files["/proc/uptime"] = []byte("273900.25 31000000.00\n")
	fsys.Files["/proc/cpuinfo"] = []byte("processor\t: 0\nvendor_id\t: ExampleVendor\nmodel name\t: Example CPU @ 2.00GHz\n")
	if !tc.noMeminfo {
		fsys.Files["/proc/meminfo"] = []byte("MemTotal:        8155528 kB\nMemFree:         4077764 kB\nMemAvailable:    6116646 kB\n")
	}
	fsys.Files["/etc/passwd"] = []byte("root:x:0:0:root:/root:/bin/bash\nnobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\nalice:x:1000:1000:Alice Example,,,:/home/alice:/bin/bash\n")
	fsys.Files["/etc/group"] = []byte("root:x:0:\nsudo:x:27:alice\nalice:x:1000:\n")
	fsys.Files["/etc/resolv.conf"] = []byte("domain lan\nsearch lan\nnameserver 192.0.2.53\nnameserver 192.0.2.54\n")
	mc.CommandOutputs["uname -r"] = []byte("6.12.0-example-amd64\n")
	mc.CommandOutputs["df -k"] = []byte("Filesystem     1K-blocks    Used Available Use% Mounted on\n/dev/vda1       41251136 6291456  34959680  16% /\n")

	log := &trace{}
	provider := &interfaces.Provider{
		FS:        recordingFS{inner: fsys, log: log},
		Commander: &recordingCommander{inner: mc, log: log},
		Network:   interfaces.NewMockNetworkOperations(),
	}
	osInfo := &osdetect.OSInfo{OsType: "debian", OsVersion: "13", OsCodename: "trixie"}
	factory := infrastructure.NewServiceFactory(provider, osInfo)
	factory.SetConfig(fixtureConfig(false))
	cfg := config.DefaultConfig()
	cfg.Username, cfg.SshPort, cfg.DryRun = "alice", 2230, tc.dryRun

	menu.NewSystemDetailsMenu(cfg, osInfo, factory.CreateHostInfoManager()).Show()

	fmt.Print(childTrailer)
	fmt.Print("# What the host-info service asked of the mocks, each call once with its count (hostname and domainname left out: asked only where the machine's name has no dot):\n")
	seen := map[string]int{}
	var order []string
	for _, e := range log.entries {
		if strings.HasPrefix(e.text, "exec   domainname") || strings.HasPrefix(e.text, "exec   hostname") {
			continue
		}
		if seen[e.text] == 0 {
			order = append(order, e.text)
		}
		seen[e.text]++
	}
	for _, text := range order {
		fmt.Printf("#   %dx %s\n", seen[text], text)
	}
}

// flagRun is one command line for `hardn system-details`, and what the
// file it might export would be called.
var flagRuns = [][]string{
	{"-E", "out.txt"}, {"--export=out.txt"}, {"-S", "network"}, {"--section", "storage"},
	{"-O", "json"}, {"-O", "yaml"}, {"-O", "nonsense"}, {"--format=json"}, {"--json"}, {"--yaml"},
	{"--json", "--yaml", "-S", "users", "-E", "out.txt"},
}

// TestLegacySystemDetailsFlags records that the five flags of `hardn
// system-details` (pkg/cmd/system_details_cmd.go:38-42) change nothing, two
// ways. The file's syntax tree, comments apart, has each flag's variable
// bound to the flag and read by nothing that prints. And
// hardncmd.SystemDetailsCmd runs in a child process for each command line,
// PATH empty, compared with the same run with no flags, numbers aside: the
// same output and no file written.
//
// Where the run reaches depends on the machine: runSystemDetails calls
// osdetect.DetectOS, which has no seam and reads /etc/os-release
// (osdetect.go:53-55), so on a Mac every run ends there, at "failed to
// detect OS", and on Debian or Ubuntu it reads that host, read only, and
// prints its report. So the golden holds the comparison, not the output:
// on a Mac the comparison reaches DetectOS alone, and the syntax tree is
// what shows the flags do nothing after it.
func TestLegacySystemDetailsFlags(t *testing.T) {
	if args := os.Getenv(sdFlags); args != "" {
		var argv []string
		if err := json.Unmarshal([]byte(args), &argv); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		cmd := hardncmd.SystemDetailsCmd()
		cmd.SetArgs(argv)
		if err := cmd.Execute(); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Numbers and the bars aside, and the two lines whose words move with
	// them: on Linux the run reads a live host, whose load and uptime change.
	numbers := regexp.MustCompile(`[0-9]+(\.[0-9]+)?|[█░]+`)
	normal := func(s string) string {
		var out []string
		for _, line := range strings.Split(numbers.ReplaceAllString(s, "#"), "\n") {
			if !strings.HasPrefix(line, "Uptime:") {
				out = append(out, line)
			}
		}
		return strings.Join(out, "\n")
	}
	run := func(argv []string) (out string, code int, wrote []string) {
		t.Helper()
		dir := t.TempDir()
		encoded, err := json.Marshal(append([]string{}, argv...))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestLegacySystemDetailsFlags$")
		var env []string
		for _, kv := range os.Environ() {
			k, _, _ := strings.Cut(kv, "=")
			if k != "PATH" && k != "HOME" {
				env = append(env, kv)
			}
		}
		cmd.Env = append(env, sdFlags+"="+string(encoded), "PATH="+filepath.Join(dir, "no-such-dir"), "HOME="+dir, "NO_COLOR=", "RUNEWIDTH_EASTASIAN=0")
		cmd.Dir = dir
		var both bytes.Buffer
		cmd.Stdout, cmd.Stderr = &both, &both
		runErr := cmd.Run()
		var exit *exec.ExitError
		switch {
		case runErr == nil:
		case errors.As(runErr, &exit):
			code = exit.ExitCode()
		default:
			t.Fatal(runErr)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			wrote = append(wrote, e.Name())
		}
		return strings.ReplaceAll(both.String(), dir, "$CWD"), code, wrote
	}

	plain, plainCode, plainWrote := run(nil)
	if len(plainWrote) != 0 {
		t.Fatalf("no flags wrote %v", plainWrote)
	}

	var b strings.Builder
	b.WriteString(baselineHeader + "\n")
	b.WriteString("# Generated by TestLegacySystemDetailsFlags (pkg/characterization); regenerate with -update. Do not edit.\n")
	b.WriteString("# Operation: hardn system-details (pkg/cmd/system_details_cmd.go), hardncmd.SystemDetailsCmd run in a child process, PATH empty, stdout a pipe\n")
	b.WriteString("# KNOWN-DEFECT: -E/--export, -S/--section, -O/--format, --json and --yaml are accepted and change nothing: the code using them is commented out (system_details_cmd.go:95-121). -O nonsense is no error; the export it names is not written. No V-number holds this (plan slice 1's row; proposals P-17).\n")
	b.WriteString("# NOTE: How far each run gets is the machine's (osdetect.go:53-55): on a machine with no /etc/os-release, this one when recorded, every run ends at DetectOS, so the comparison below reaches that far alone and the syntax tree is what shows the rest; on Debian or Ubuntu it reads the host, read only [unverified: not run on Linux].\n")
	b.WriteString("# NOTE: `hardn system-details` has no root check of its own (system_details_cmd.go:28-36; the root command's is cmd/hardn/main.go:148-152, on its own Run). What it reads as another user is not recorded here.\n")
	fmt.Fprintf(&b, "# NOTE: The report's output begins with the clear-screen sequence even with stdout a pipe (DetectOS calls utils.PrintHeader, osdetect.go:50; utils.go:24-31): %t here.\n", strings.HasPrefix(plain, "\x1b[H\x1b[2J"))
	b.WriteString("# ----\n")
	b.WriteString("# The flags' variables in the file's syntax tree (comments aside), with what each use does:\n")
	for _, line := range flagVariableUses(t) {
		b.WriteString("#   " + line + "\n")
	}
	b.WriteString("# Each command line against the same run with no flags, numbers aside:\n")
	for _, argv := range flagRuns {
		out, code, wrote := run(argv)
		same := normal(out) == normal(plain) && code == plainCode
		fmt.Fprintf(&b, "#   %s: same output and status %t; files written: %d\n", formatArgv(argv), same, len(wrote))
		if !same {
			t.Errorf("%v: output differs from no flags:\n%s\n----\n%s", argv, out, plain)
		}
	}
	assertGolden(t, "menu/system_details_flags.golden", b.String())
}

// flagVariableUses walks pkg/cmd/system_details_cmd.go's syntax tree and
// says, for each flag's variable, each line it is bound, assigned or read.
func flagVariableUses(t *testing.T) []string {
	t.Helper()
	const file = "../cmd/system_details_cmd.go"
	fset := token.NewFileSet()
	tree, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"exportFile", "sectionFilter", "outputFormat", "formatAsJson", "formatAsYaml"}
	uses := map[string][]string{}
	var walk func(n ast.Node, parents []ast.Node)
	walk = func(n ast.Node, parents []ast.Node) {
		ast.Inspect(n, func(c ast.Node) bool {
			if c == nil || c == n {
				return true
			}
			if id, ok := c.(*ast.Ident); ok && slices.Contains(names, id.Name) {
				how := "read"
				switch p := n.(type) {
				case *ast.UnaryExpr:
					how = "bound to its flag"
				case *ast.AssignStmt:
					for _, lhs := range p.Lhs {
						if lhs == c {
							how = "assigned"
						}
					}
				case *ast.ValueSpec:
					how = "declared"
				}
				uses[id.Name] = append(uses[id.Name], fmt.Sprintf("%s :%d", how, fset.Position(id.Pos()).Line))
				return false
			}
			walk(c, append(parents, n))
			return false
		})
	}
	walk(tree, nil)
	var lines []string
	for _, name := range names {
		lines = append(lines, name+": "+strings.Join(uses[name], ", "))
	}
	return lines
}

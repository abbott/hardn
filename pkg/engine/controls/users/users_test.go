package users_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
)

// Nothing here runs a command or reads the machine: every host is
// hostfake's, loaded from testdata (its README says what the files are)
// with what each case adds. Synthetic values only: alice, bob, george.

// platform is a supported release's stock host.
type platform struct {
	dir   string // testdata/<dir>
	facts detect.Facts
}

var (
	yes = detect.Tool{Present: detect.Yes}
	no  = detect.Tool{Present: detect.No}
)

func debian13() platform {
	return platform{dir: "debian13", facts: detect.Facts{
		Platform:  detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Proxmox:   detect.Proxmox{Installed: detect.No},
		Elevation: detect.Elevation{Sudo: yes, SudoFlavor: detect.SudoOriginal, Doas: no},
	}}
}

func ubuntu2604() platform {
	p := debian13()
	p.dir = "ubuntu2604"
	p.facts.Platform = detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04", Support: detect.Supported}
	p.facts.Elevation.SudoFlavor = detect.SudoRS
	return p
}

func alpine324() platform {
	return platform{dir: "alpine324", facts: detect.Facts{
		Platform:  detect.Platform{Distro: detect.DistroAlpine, Version: "3.24", Support: detect.Supported},
		Proxmox:   detect.Proxmox{Installed: detect.No},
		Elevation: detect.Elevation{Sudo: no, Doas: yes},
	}}
}

func pve9() platform {
	p := debian13()
	p.dir = "pve9"
	p.facts.Platform = detect.Platform{Distro: detect.DistroProxmox, Version: "9", Support: detect.Supported}
	p.facts.Proxmox = detect.Proxmox{Installed: detect.Yes, Version: "9.0.10"}
	p.facts.Elevation.Sudo = no
	return p
}

// with is p with its facts changed.
func with(p platform, f func(*detect.Facts)) platform {
	f(&p.facts)
	return p
}

// site is a host: the platform's files, with a case's accounts and files.
type site struct {
	plat    platform
	users   []string          // lines added to /etc/passwd
	members map[string]string // group name → its member list
	files   map[string]string // path → content, added or replacing; "" removes
	fail    map[string]error  // "op path" → the error the host returns
	reject  string            // the file the tool's check rejects: sudoers.MainConfig for visudo -c, a doas file for doas -C
}

// stock maps testdata's files to their paths.
var stock = map[string]string{
	"passwd": "/etc/passwd", "group": "/etc/group", "sudoers": sudoers.MainConfig,
	"README": sudoers.DropInDir + "/README", "doas.conf": sudoers.DoasConfig, "20-wheel.conf": sudoers.DoasDir + "/20-wheel.conf",
}

func (s site) build(t *testing.T) (host.System, *hostfake.System) {
	t.Helper()
	files := map[string]string{}
	for name, p := range stock {
		data, err := os.ReadFile(filepath.Join("testdata", s.plat.dir, name))
		switch {
		case err == nil:
			files[p] = string(data)
		case !errors.Is(err, fs.ErrNotExist):
			t.Fatal(err)
		}
	}
	files["/etc/passwd"] += strings.Join(append(s.users, ""), "\n")
	for g, m := range s.members {
		var out []string
		for _, l := range strings.Split(files["/etc/group"], "\n") {
			if f := strings.Split(l, ":"); f[0] == g && len(f) == 4 {
				l = strings.Join(append(f[:3], m), ":")
			}
			out = append(out, l)
		}
		files["/etc/group"] = strings.Join(out, "\n")
	}
	for p, data := range s.files {
		files[p] = data
	}
	h := hostfake.New(host.Options{})
	for _, d := range []string{"/etc", "/bin", "/usr/sbin", "/sbin"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	for _, sh := range []string{"/bin/bash", "/bin/sh", "/usr/sbin/nologin", "/sbin/nologin", "/bin/sync"} {
		h.AddFile(sh, nil, 0o755, 0, 0)
	}
	if _, ok := files[sudoers.MainConfig]; ok {
		h.AddDir(sudoers.DropInDir, 0o750, 0, 0)
	}
	if _, ok := files[sudoers.DoasConfig]; ok {
		h.AddDir(sudoers.DoasDir, 0o750, 0, 0)
	}
	for p, data := range files {
		if data != "" {
			h.AddFile(p, []byte(data), 0o440, 0, 0)
		}
	}
	// visudo -c and doas -C accept every file but s.reject, as
	// sudoers.CheckInstalled runs them.
	h.Handler = func(argv []string, _ []byte) hostfake.Response {
		switch {
		case slices.Equal(argv, []string{"visudo", "-c"}) && s.reject != sudoers.MainConfig:
			return hostfake.Response{Stdout: sudoers.MainConfig + ": parsed OK\n"}
		case len(argv) == 3 && argv[0] == "doas" && argv[1] == "-C" && argv[2] != s.reject:
			return hostfake.Response{}
		}
		return hostfake.Response{Stderr: "syntax error at line 1\n", ExitCode: 1}
	}
	if len(s.fail) > 0 {
		return faulty{h, s.fail}, h
	}
	return h, h
}

// faulty fails the reads s.fail names.
type faulty struct {
	*hostfake.System
	fail map[string]error
}

func (f faulty) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if err := f.fail["read "+name]; err != nil {
		return nil, &fs.PathError{Op: "read", Path: name, Err: err}
	}
	return f.System.ReadFile(ctx, name)
}

func (f faulty) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	if err := f.fail["stat "+name]; err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
	}
	return f.System.Stat(ctx, name)
}

type row struct {
	name  string
	site  site
	state engine.State
	want  string   // the detail
	codes []string // the findings' codes, in order
	err   string   // the error's code (pkg/output), "" for none
}

func run(t *testing.T, r row) engine.Status {
	t.Helper()
	sys, h := r.site.build(t)
	st, err := users.Admin{}.Check(context.Background(), sys, r.site.plat.facts)
	if code := codeOf(err); code != r.err {
		t.Errorf("error %v: code %q, want %q", err, code, r.err)
	}
	if err != nil && st.State != engine.Unknown {
		t.Errorf("an error with %s %q: a failed read is Unknown", st.State, st.Detail)
	}
	if st.State != r.state || st.Detail != r.want {
		t.Errorf("%s %q, want %s %q (evidence %q, findings %v)", st.State, st.Detail, r.state, r.want, st.Evidence, st.Findings)
	}
	var codes []string
	for _, fd := range st.Findings {
		codes = append(codes, fd.Code)
		if e, ok := output.Lookup(fd.Code); !ok || e.Kind != output.KindFinding {
			t.Errorf("finding %s: not a registered finding code", fd.Code)
		}
	}
	if !slices.Equal(codes, r.codes) {
		t.Errorf("findings %v, want codes %v", st.Findings, r.codes)
	}
	readOnly(t, h)
	return st
}

// readOnly fails for any call that changes the fake, and any command but
// the tools' own checks.
func readOnly(t *testing.T, h *hostfake.System) {
	t.Helper()
	for _, c := range h.Calls() {
		switch c.Op {
		case "read", "read-nofollow", "stat", "lstat", "readdir":
		case "run":
			if a := c.Argv; !slices.Equal(a, []string{"visudo", "-c"}) && (len(a) != 3 || a[0] != "doas" || a[1] != "-C") {
				t.Errorf("ran %q", a)
			}
		default:
			t.Errorf("a call that is not a read: %v", c)
		}
	}
}

func codeOf(err error) string {
	if err == nil {
		return ""
	}
	return output.CodeOf(err)
}

func runAll(t *testing.T, rows []row) {
	t.Helper()
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) { run(t, r) })
	}
}

const (
	alice    = "alice:x:1000:1000:Alice,,,:/home/alice:/bin/bash"
	bob      = "bob:x:1001:1001::/home/bob:/bin/bash"
	noPass   = users.CodeNoPassword
	notRead  = users.CodeRuleNotRead
	uid0     = users.CodeUID0
	hardnPw  = sudoers.Header + "alice ALL=(ALL) ALL\n"
	hardnNo  = sudoers.Header + "alice ALL=(ALL) NOPASSWD: ALL\n"
	grantDir = sudoers.DropInDir + "/"
)

var sudoGroup = map[string]string{"sudo": "alice"}

// TestSudo: every sudo line of check-users.md §5 on Debian 13, sudo's
// reading of its rules, and the accounts that count.
func TestSudo(t *testing.T) {
	d := debian13()
	runAll(t, []row{
		{name: "stock, root only", site: site{plat: d}, state: engine.NotConfigured, want: "root only"},
		{name: "in sudo", site: site{plat: d, users: []string{alice}, members: sudoGroup}, state: engine.Configured, want: "non-root, sudo"},
		{name: "sudo as primary group", site: site{plat: d, users: []string{"alice:x:1000:27::/home/alice:/bin/bash"}}, state: engine.Configured, want: "non-root, sudo"},
		{name: "not in sudo", site: site{plat: d, users: []string{alice}}, state: engine.Partial, want: "non-root, no sudo"},
		{name: "one of two", site: site{plat: d, users: []string{bob, alice}, members: sudoGroup}, state: engine.Configured, want: "non-root, sudo"},
		{name: "hardn's grant", site: site{plat: d, users: []string{alice}, files: map[string]string{grantDir + "hardn-grant-alice": hardnPw,
			grantDir + "hardn-env-alice": sudoers.Header + "Defaults:alice env_keep += \"HARDN_CONFIG\"\n"}}, state: engine.Configured, want: "non-root, sudo"},
		{name: "hardn's grant, no password", site: site{plat: d, users: []string{alice}, files: map[string]string{grantDir + "hardn-grant-alice": hardnNo}},
			state: engine.Configured, want: "non-root, sudo", codes: []string{noPass}},
		{name: "cloud-init's", site: site{plat: d, users: []string{alice}, members: sudoGroup, files: map[string]string{grantDir + "90-cloud-init-users": "# Created by cloud-init\n\n# User rules for alice\nalice ALL=(ALL) NOPASSWD:ALL\n"}},
			state: engine.Configured, want: "non-root, sudo", codes: []string{noPass}},
		{name: "a rule not read before the grant", site: site{plat: d, users: []string{alice}, files: map[string]string{grantDir + "10-site": "User_Alias ADMINS = bob\nADMINS ALL=(ALL) ALL\n", grantDir + "hardn-grant-alice": hardnPw}},
			state: engine.Configured, want: "non-root, sudo"},
		{name: "a rule not read after the grant", site: site{plat: d, users: []string{alice}, members: sudoGroup, files: map[string]string{grantDir + "zz-site": "alice ALL=(ALL) !ALL\n"}},
			state: engine.Unknown, want: "can't tell who has sudo", codes: []string{notRead}},
		{name: "a user ID after the includedir", site: site{plat: d, users: []string{alice}, members: sudoGroup, files: map[string]string{sudoers.MainConfig: readStock(t, d, "sudoers") + "#1000 ALL=(ALL) !ALL\n"}},
			state: engine.Unknown, want: "can't tell who has sudo", codes: []string{notRead}},
		{name: "a continued line", site: site{plat: d, users: []string{alice}, members: sudoGroup, files: map[string]string{grantDir + "zz-site": "alice ALL=(ALL) \\\n  NOPASSWD: ALL\n"}},
			state: engine.Unknown, want: "can't tell who has sudo", codes: []string{notRead}},
		{name: "files sudo skips", site: site{plat: d, users: []string{alice}, members: sudoGroup, files: map[string]string{grantDir + "zz.conf": "alice ALL=(ALL) !ALL\n", grantDir + "zz~": "alice ALL=(ALL) !ALL\n"}},
			state: engine.Configured, want: "non-root, sudo"},
		{name: "nologin", site: site{plat: d, users: []string{"alice:x:1000:27::/home/alice:/usr/sbin/nologin"}}, state: engine.NotConfigured, want: "root only"},
		{name: "shell not installed", site: site{plat: d, users: []string{"alice:x:1000:27::/home/alice:/bin/zsh"}}, state: engine.NotConfigured, want: "root only"},
		{name: "a system account", site: site{plat: d, users: []string{"backup:x:34:27::/var/backups:/bin/bash"}}, state: engine.NotConfigured, want: "root only"},
		{name: "a second root", site: site{plat: d, users: []string{"george:x:0:0::/root:/bin/bash"}}, state: engine.NotConfigured, want: "root only", codes: []string{uid0}},
		{name: "a second line for a name", site: site{plat: d, users: []string{"alice:x:0:0::/root:/bin/bash", alice}, members: sudoGroup}, state: engine.NotConfigured, want: "root only", codes: []string{uid0}},
		{name: "sudo not installed", site: site{plat: with(d, func(f *detect.Facts) { f.Elevation.Sudo = no }), users: []string{alice}, members: sudoGroup}, state: engine.Partial, want: "non-root, no sudo"},
	})
}

func readStock(t *testing.T, p platform, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", p.dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestPlatforms: the stock lines of Ubuntu 26.04, Alpine 3.24 and Proxmox
// VE 9, and doas's reading of its rules.
func TestPlatforms(t *testing.T) {
	u, a, p := ubuntu2604(), alpine324(), pve9()
	wheel := map[string]string{"wheel": "root,alice"}
	doasFile := func(rule string) map[string]string {
		return map[string]string{sudoers.DoasDir + "/hardn-alice.conf": sudoers.Header + rule + "\n"}
	}
	runAll(t, []row{
		{name: "ubuntu, installer's user", site: site{plat: u, users: []string{alice}, members: sudoGroup}, state: engine.Configured, want: "non-root, sudo"},
		{name: "ubuntu, sudo-rs's own file", site: site{plat: u, users: []string{alice}, members: sudoGroup,
			files: map[string]string{sudoers.SudoersRS: "root ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n"}}, state: engine.Partial, want: "non-root, no sudo"},
		{name: "alpine, setup-alpine's user", site: site{plat: a, users: []string{"alice:x:1000:1000:alice:/home/alice:/bin/sh"}, members: wheel}, state: engine.Configured, want: "non-root, doas"},
		{name: "alpine, not in wheel", site: site{plat: a, users: []string{"alice:x:1000:1000::/home/alice:/bin/sh"}}, state: engine.Partial, want: "non-root, no doas"},
		{name: "alpine, hardn's rule", site: site{plat: a, users: []string{"alice:x:1000:1000::/home/alice:/bin/sh"}, files: doasFile("permit persist setenv { HARDN_CONFIG } alice")},
			state: engine.Configured, want: "non-root, doas"},
		{name: "alpine, no password", site: site{plat: a, users: []string{"alice:x:1000:1000::/home/alice:/bin/sh"}, files: doasFile("permit nopass alice as root")},
			state: engine.Configured, want: "non-root, doas", codes: []string{noPass}},
		{name: "alpine, denied after wheel", site: site{plat: a, users: []string{"alice:x:1000:1000::/home/alice:/bin/sh"}, members: wheel, files: doasFile("deny alice")},
			state: engine.Partial, want: "non-root, no doas"},
		{name: "alpine, a command rule", site: site{plat: a, users: []string{"alice:x:1000:1000::/home/alice:/bin/sh"}, members: wheel, files: doasFile("deny alice cmd reboot")},
			state: engine.Unknown, want: "can't tell who has doas", codes: []string{notRead}},
		{name: "alpine, doas and sudo", site: site{plat: with(a, func(f *detect.Facts) { f.Elevation.Sudo, f.Elevation.SudoFlavor = yes, detect.SudoOriginal }),
			users: []string{"alice:x:1000:1000::/home/alice:/bin/sh"}, members: wheel, files: map[string]string{sudoers.MainConfig: "@includedir /etc/sudoers.d\n"}},
			state: engine.Configured, want: "non-root, doas"},
		{name: "alpine, neither grants", site: site{plat: with(a, func(f *detect.Facts) { f.Elevation.Sudo, f.Elevation.SudoFlavor = yes, detect.SudoOriginal }),
			users: []string{"alice:x:1000:1000::/home/alice:/bin/sh"}, files: map[string]string{sudoers.MainConfig: "@includedir /etc/sudoers.d\n"}},
			state: engine.Partial, want: "non-root, no sudo or doas"},
		{name: "proxmox, stock", site: site{plat: p}, state: engine.NotConfigured, want: "root only"},
		{name: "proxmox, a user, no sudo", site: site{plat: p, users: []string{alice}, members: sudoGroup}, state: engine.Partial, want: "non-root, no sudo"},
		{name: "proxmox, sudo installed", site: site{plat: with(p, func(f *detect.Facts) { f.Elevation.Sudo = yes }), users: []string{alice}, members: sudoGroup,
			files: map[string]string{sudoers.MainConfig: readStock(t, debian13(), "sudoers")}}, state: engine.Configured, want: "non-root, sudo"},
	})
}

// TestUnknown: every read that fails, and every fact not established,
// where the state depends on it; and states established without them.
func TestUnknown(t *testing.T) {
	d, a := debian13(), alpine324()
	denied := func(op, path string) map[string]error { return map[string]error{op + " " + path: fs.ErrPermission} }
	sudoUnknown := with(d, func(f *detect.Facts) { f.Elevation.Sudo = detect.Tool{Err: fs.ErrPermission} })
	doasOnDebian := with(d, func(f *detect.Facts) { f.Elevation.Doas = yes })
	runAll(t, []row{
		{name: "passwd unreadable", site: site{plat: d, fail: denied("read", "/etc/passwd")}, state: engine.Unknown, want: "can't read", err: "PERMISSION_DENIED"},
		{name: "group malformed", site: site{plat: d, files: map[string]string{"/etc/group": "root:x:0:\nsudo:x:27\n"}}, state: engine.Unknown, want: "can't read", err: "ACCOUNTS_MALFORMED"},
		{name: "sudoers unreadable", site: site{plat: d, users: []string{alice}, fail: denied("read", sudoers.MainConfig)}, state: engine.Unknown, want: "can't read", err: "PERMISSION_DENIED"},
		{name: "sudoers unreadable, root only", site: site{plat: d, fail: denied("read", sudoers.MainConfig)}, state: engine.NotConfigured, want: "root only"},
		{name: "a drop-in unreadable", site: site{plat: d, users: []string{alice}, fail: denied("read", grantDir+"README")}, state: engine.Unknown, want: "can't read", err: "PERMISSION_DENIED"},
		{name: "no includedir", site: site{plat: d, users: []string{alice}, members: sudoGroup, files: map[string]string{sudoers.MainConfig: "%sudo ALL=(ALL:ALL) ALL\n"}},
			state: engine.Unknown, want: "can't read", err: "SUDOERS_NOT_INCLUDED"},
		{name: "shell unreadable", site: site{plat: d, users: []string{alice}, members: sudoGroup, fail: denied("stat", "/bin/bash")}, state: engine.Unknown, want: "can't read", err: "PERMISSION_DENIED"},
		{name: "shell unreadable, no other login", site: site{plat: d, users: []string{alice}, fail: denied("stat", "/bin/bash")}, state: engine.Unknown, want: "can't read", err: "PERMISSION_DENIED"},
		{name: "shell unreadable, no grant", site: site{plat: d, users: []string{alice, "bob:x:1001:1001::/home/bob:/bin/sh"}, fail: denied("stat", "/bin/bash")},
			state: engine.Partial, want: "non-root, no sudo"},
		{name: "sudo not established", site: site{plat: sudoUnknown, users: []string{alice}, members: sudoGroup}, state: engine.Unknown, want: "can't tell if sudo installed", err: "PERMISSION_DENIED"},
		{name: "sudo not established, root only", site: site{plat: sudoUnknown}, state: engine.NotConfigured, want: "root only"},
		{name: "doas off Alpine", site: site{plat: doasOnDebian, users: []string{alice}}, state: engine.Unknown, want: "can't tell who has doas", err: "SUDOERS_DOAS_UNSUPPORTED"},
		{name: "doas off Alpine, sudo grants", site: site{plat: doasOnDebian, users: []string{alice}, members: sudoGroup}, state: engine.Configured, want: "non-root, sudo"},
		{name: "doas.conf missing", site: site{plat: a, users: []string{"alice:x:1000:1000::/home/alice:/bin/sh"}, members: map[string]string{"wheel": "alice"}, files: map[string]string{sudoers.DoasConfig: ""}},
			state: engine.Unknown, want: "can't read", err: "SUDOERS_CHECK_FAILED"},
		{name: "visudo rejects", site: site{plat: d, users: []string{alice}, members: sudoGroup, reject: sudoers.MainConfig}, state: engine.Unknown, want: "sudo config invalid", err: "SUDOERS_INVALID_BEFORE"},
		{name: "visudo rejects, root only", site: site{plat: d, reject: sudoers.MainConfig}, state: engine.NotConfigured, want: "root only"},
		{name: "doas -C rejects", site: site{plat: a, users: []string{"alice:x:1000:1000::/home/alice:/bin/sh"}, members: map[string]string{"wheel": "alice"}, reject: sudoers.DoasDir + "/20-wheel.conf"},
			state: engine.Unknown, want: "doas config invalid", err: "SUDOERS_INVALID_BEFORE"},
	})
}

// TestEvidence: what each state was read from, one line per source.
func TestEvidence(t *testing.T) {
	st := run(t, row{site: site{plat: debian13(), users: []string{alice, bob}, members: sudoGroup,
		files: map[string]string{grantDir + "90-cloud-init-users": "alice ALL=(ALL) NOPASSWD:ALL\n"}},
		state: engine.Configured, want: "non-root, sudo", codes: []string{noPass}})
	want := []string{
		"doas: not installed",
		"passwd: alice, uid 1000, /bin/bash",
		"sudo: alice by /etc/sudoers.d/90-cloud-init-users:1: alice ALL=(ALL) NOPASSWD:ALL",
		"passwd: bob, uid 1001, /bin/bash",
		"sudo: no rule for bob",
	}
	if !slices.Equal(st.Evidence, want) {
		t.Errorf("evidence\n%q\nwant\n%q", st.Evidence, want)
	}
	if f := st.Findings[0]; f.Detail != "alice: sudo without password" || f.Source != "/etc/sudoers.d/90-cloud-init-users:1" {
		t.Errorf("finding %+v", f)
	}
	st = run(t, row{site: site{plat: alpine324(), users: []string{"alice:x:1000:1000::/home/alice:"}, members: map[string]string{"wheel": "root,alice"},
		files: map[string]string{sudoers.DoasDir + "/zz.conf": "deny alice\npermit :wheel cmd apk\n"}},
		state: engine.Unknown, want: "can't tell who has doas", codes: []string{notRead}})
	want = []string{
		"sudo: not installed",
		"passwd: alice, uid 1000, /bin/sh",
		"doas: alice not established, /etc/doas.d/zz.conf:2: permit :wheel cmd apk",
	}
	if !slices.Equal(st.Evidence, want) {
		t.Errorf("evidence\n%q\nwant\n%q", st.Evidence, want)
	}
	if f := st.Findings[0]; f.Detail != "may decide doas for alice" || f.Source != "/etc/doas.d/zz.conf:2" {
		t.Errorf("finding %+v", f)
	}
	st = run(t, row{site: site{plat: debian13()}, state: engine.NotConfigured, want: "root only"})
	if want := []string{"passwd: no account with uid 1000 or more and a login shell"}; !slices.Equal(st.Evidence, want) {
		t.Errorf("evidence %q, want %q", st.Evidence, want)
	}
}

// TestEngineCheck: Engine.Check over Controls(), Figma's one row.
func TestEngineCheck(t *testing.T) {
	check := func(s site) engine.Report {
		sys, _ := s.build(t)
		e := engine.New(sys, engine.Options{Detect: func(context.Context, host.System) (detect.Facts, error) { return s.plat.facts, nil }})
		reps, err := e.Check(context.Background(), users.Controls())
		if err != nil || len(reps) != 1 {
			t.Fatalf("reports %+v, %v", reps, err)
		}
		return reps[0]
	}
	r := check(site{plat: debian13(), users: []string{alice}, members: sudoGroup})
	if r.Control != "users.admin" || r.Title != "User" || r.Err != nil || r.Status.State != engine.Configured || r.Status.Detail != "non-root, sudo" {
		t.Errorf("report %+v", r)
	}
	r = check(site{plat: debian13(), fail: map[string]error{"read /etc/passwd": fs.ErrPermission}})
	if r.Status.State != engine.Unknown || output.CodeOf(r.Err) != "PERMISSION_DENIED" {
		t.Errorf("a failed read: %+v", r)
	}
}

// TestCheckOnly: Plan is ErrNotMigrated, and a plan with the control is
// refused with it (CONTROL_NOT_MIGRATED), changing nothing.
func TestCheckOnly(t *testing.T) {
	s := site{plat: debian13(), users: []string{alice}, members: sudoGroup}
	sys, h := s.build(t)
	cs, err := users.Admin{}.Plan(context.Background(), sys, engine.Input{Facts: s.plat.facts})
	if !errors.Is(err, engine.ErrNotMigrated) || cs != nil {
		t.Errorf("Plan %v, %v", cs, err)
	}
	h.AddDir("/run", 0o755, 0, 0)
	e := engine.New(sys, engine.Options{Detect: func(context.Context, host.System) (detect.Facts, error) { return s.plat.facts, nil }})
	p, err := e.Plan(context.Background(), users.Controls(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Empty() || !errors.Is(p.Err(), engine.ErrNotMigrated) || len(p.Items) != 1 || output.CodeOf(p.Items[0].Err) != "CONTROL_NOT_MIGRATED" {
		t.Fatalf("plan: %d changes, items %+v", len(p.Changes), p.Items)
	}
	for _, c := range h.Calls() {
		if c.Op != "mkdir" && c.Op != "removeall" && c.Op != "lstat" && c.Op != "stat" {
			t.Errorf("plan: %v", c)
		}
	}
}

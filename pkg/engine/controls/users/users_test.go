package users_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
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
	sudoYes = detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}
	doasYes = detect.Tool{Present: detect.Yes, Path: "/usr/bin/doas"}
	no      = detect.Tool{Present: detect.No}
)

func debian13() platform {
	return platform{dir: "debian13", facts: detect.Facts{
		Platform:  detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Proxmox:   detect.Proxmox{Installed: detect.No},
		Elevation: detect.Elevation{Sudo: sudoYes, SudoFlavor: detect.SudoOriginal, Doas: no},
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
		Init:      detect.Init{Kind: detect.OpenRC, Running: detect.Yes},
		SSH:       detect.SSH{Mode: detect.SSHOpenRC, OpenRC: detect.OpenRCService{Started: detect.Yes, InRunlevel: detect.Yes}},
		Proxmox:   detect.Proxmox{Installed: detect.No},
		Elevation: detect.Elevation{Sudo: no, Doas: doasYes},
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
	runErr  error             // visudo -c cannot run: Run returns it
	bare    string            // a tool's path whose setuid bit Stat does not show
	seed    func(*hostfake.System)
	// locks are the password fields of /etc/shadow by account
	// (next/p4-locked); every other account of /etc/passwd has "*", not
	// locked. files[accounts.ShadowFile] replaces the whole file.
	locks map[string]string
	// sshd is sshd -T's answer (sshdPAM, sshdNoPAM), with an
	// sshd_config; "" is no sshd: sshd -T is not answered. failT, when
	// set, is sshd -T's answer instead.
	sshd  string
	failT *hostfake.Response
	// pamG is Alpine's /usr/sbin/sshd.pam -G answer, which installs it;
	// "" is not installed. failG makes that -G exit 255.
	pamG  string
	failG bool
	// noUseradd: no /usr/sbin/useradd, as on Alpine without shadow
	// (Create's UseraddRefusal); every other host has it.
	noUseradd bool
}

// sshd -T's answers (next/p4-locked): a build with PAM and UsePAM yes
// (Debian's, Ubuntu's), and Alpine's /usr/sbin/sshd, built without PAM,
// which prints no usepam line. Shaped like OpenSSH's, not read from a
// binary.
const (
	sshdBase  = "port 22\nlistenaddress [::]:22\nlistenaddress 0.0.0.0:22\n"
	sshdRest  = "permitrootlogin prohibit-password\npubkeyauthentication yes\npasswordauthentication no\nkbdinteractiveauthentication no\n"
	sshdPAM   = sshdBase + "usepam yes\n" + sshdRest
	sshdNoPAM = sshdBase + sshdRest
)

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
	var shadow strings.Builder
	for _, l := range strings.Split(files["/etc/passwd"], "\n") {
		if name, _, ok := strings.Cut(l, ":"); ok {
			field, set := s.locks[name]
			if !set {
				field = "*"
			}
			shadow.WriteString(name + ":" + field + ":20000:0:99999:7:::\n")
		}
	}
	files[accounts.ShadowFile] = shadow.String()
	if s.sshd != "" {
		files[sshdConfig] = "PasswordAuthentication no\n"
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
	if !s.noUseradd {
		h.AddFile("/usr/sbin/useradd", nil, 0o755, 0, 0)
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
	// The tools detect found, setuid root as installed: hostfake has no
	// setuid bit, so Stat adds it (suid) but at s.bare.
	suid := map[string]bool{}
	for _, tl := range []detect.Tool{s.plat.facts.Elevation.Sudo, s.plat.facts.Elevation.Doas} {
		if tl.Present == detect.Yes {
			h.AddFile(tl.Path, nil, 0o755, 0, 0)
			suid[tl.Path] = tl.Path != s.bare
		}
	}
	if s.pamG != "" {
		h.AddFile(pamG[0], []byte("ELF"), 0o755, 0, 0)
	}
	if s.seed != nil {
		s.seed(h)
	}
	// visudo -c and doas -C accept every file but s.reject, as
	// sudoers.CheckInstalled runs them.
	h.Handler = func(argv []string, _ []byte) hostfake.Response {
		switch {
		case s.failT != nil && sshdT(argv):
			return *s.failT
		case s.sshd != "" && sshdT(argv):
			return hostfake.Response{Stdout: s.sshd}
		case s.failG && slices.Equal(argv, pamG):
			return hostfake.Response{Stderr: "sshd: no hostkeys available -- exiting.\n", ExitCode: 255}
		case s.pamG != "" && slices.Equal(argv, pamG):
			return hostfake.Response{Stdout: s.pamG}
		case slices.Equal(argv, []string{"visudo", "-c"}) && s.runErr != nil:
			return hostfake.Response{Err: s.runErr}
		case slices.Equal(argv, []string{"visudo", "-c"}) && s.reject != sudoers.MainConfig:
			return hostfake.Response{Stdout: sudoers.MainConfig + ": parsed OK\n"}
		case len(argv) == 3 && argv[0] == "visudo" && argv[1] == "-cf" && s.reject != sudoers.MainConfig: // a plan's copy (sudoers.Apply)
			return hostfake.Response{Stdout: argv[2] + ": parsed OK\n"}
		case len(argv) == 3 && argv[0] == "doas" && argv[1] == "-C" && argv[2] != s.reject:
			return hostfake.Response{}
		}
		return hostfake.Response{Stderr: "syntax error at line 1\n", ExitCode: 1}
	}
	return faulty{h, s.fail, suid}, h
}

// faulty fails the reads fail names, and shows the setuid bit of the
// paths suid says.
type faulty struct {
	*hostfake.System
	fail map[string]error
	suid map[string]bool
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
	fi, err := f.System.Stat(ctx, name)
	if err == nil && f.suid[name] {
		fi = setuid{fi}
	}
	return fi, err
}

func (f faulty) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	if err := f.fail["write "+name]; err != nil {
		return &fs.PathError{Op: "write", Path: name, Err: err}
	}
	return f.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

// setuid is a file's information with its setuid bit set.
type setuid struct{ fs.FileInfo }

func (i setuid) Mode() fs.FileMode { return i.FileInfo.Mode() | fs.ModeSetuid }

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

// sshdConfig is sshd's main file, which sshd -T reads.
const sshdConfig = "/etc/ssh/sshd_config"

// sshdT is sshd -T of sshd's main file, by the name on the path or
// Alpine's /usr/sbin/sshd (reload.OpenRCFlavour's), as ssh.ReadPAM runs
// it.
func sshdT(argv []string) bool {
	return (argv[0] == "sshd" || argv[0] == "/usr/sbin/sshd") && slices.Equal(argv[1:], []string{"-T", "-f", sshdConfig})
}

// pamG is the -G of Alpine's sshd.pam that ssh.ReadPAM runs where which
// build runs is not established (an Alpine container).
var pamG = []string{"/usr/sbin/sshd.pam", "-f", sshdConfig, "-G"}

// readOnly fails for any call that changes the fake, and any command but
// the tools' own checks and sshd -T.
func readOnly(t *testing.T, h *hostfake.System) {
	t.Helper()
	for _, c := range h.Calls() {
		switch c.Op {
		case "read", "read-nofollow", "stat", "lstat", "readdir":
		case "run":
			if a := c.Argv; !slices.Equal(a, []string{"visudo", "-c"}) && (len(a) != 3 || a[0] != "doas" || a[1] != "-C") && !sshdT(a) && !slices.Equal(a, pamG) {
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
	aliceSh  = "alice:x:1000:1000::/home/alice:/bin/sh"
	noPass   = users.CodeNoPassword
	notRead  = users.CodeRuleNotRead
	uid0     = users.CodeUID0
	grantDir = sudoers.DropInDir + "/"
)

var sudoGroup = map[string]string{"sudo": "alice"}

// written is what sudoers' writer plans for pol on p's host, path to
// content: hardn's own rules come from the writer, so a change there that
// this reader does not read fails here.
func written(t *testing.T, p platform, pol sudoers.Policy) map[string]string {
	t.Helper()
	sys, _ := site{plat: p}.build(t)
	want := sudoers.ToolSudo
	if p.facts.Elevation.Sudo.Present != detect.Yes {
		want = sudoers.ToolDoas
	}
	st, err := sudoers.Read(context.Background(), sys, sudoers.Choose(p.facts, want), []string{pol.User})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := sudoers.PlanFor(st, []sudoers.Policy{pol})
	if err != nil || len(plan.Changes) == 0 {
		t.Fatalf("plan %+v, %v", plan, err)
	}
	out := map[string]string{}
	for _, c := range plan.Changes {
		out[c.Path] = string(c.Data)
	}
	return out
}

// merge is the files of ms, later ones replacing earlier.
func merge(ms ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range ms {
		maps.Copy(out, m)
	}
	return out
}

// TestSudo: every sudo line of check-users.md §5 on Debian 13, sudo's
// reading of its rules, and the accounts that count.
func TestSudo(t *testing.T) {
	d := debian13()
	grant := written(t, d, sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword, Env: []string{"HARDN_CONFIG"}})
	grantNo := written(t, d, sudoers.Policy{User: "alice", Grant: sudoers.GrantNoPassword})
	dropIn := func(rules string) map[string]string { return map[string]string{grantDir + "zz-site": rules} }
	inSudo := func(name string, files map[string]string, state engine.State, want string, codes ...string) row {
		return row{name: name, site: site{plat: d, users: []string{alice}, members: sudoGroup, files: files}, state: state, want: want, codes: codes}
	}
	mainWith := func(top string) map[string]string {
		return map[string]string{sudoers.MainConfig: top + readStock(t, d, "sudoers")}
	}
	runAll(t, []row{
		{name: "stock, root only", site: site{plat: d}, state: engine.NotConfigured, want: "root only"},
		{name: "in sudo", site: site{plat: d, users: []string{alice}, members: sudoGroup}, state: engine.Configured, want: "non-root, sudo"},
		{name: "sudo as primary group", site: site{plat: d, users: []string{"alice:x:1000:27::/home/alice:/bin/bash"}}, state: engine.Configured, want: "non-root, sudo"},
		{name: "not in sudo", site: site{plat: d, users: []string{alice}}, state: engine.Partial, want: "non-root, no sudo"},
		{name: "one of two", site: site{plat: d, users: []string{bob, alice}, members: sudoGroup}, state: engine.Configured, want: "non-root, sudo"},
		{name: "hardn's grant", site: site{plat: d, users: []string{alice}, files: grant}, state: engine.Configured, want: "non-root, sudo"},
		{name: "hardn's grant, no password", site: site{plat: d, users: []string{alice}, files: grantNo}, state: engine.Configured, want: "non-root, sudo", codes: []string{noPass}},
		inSudo("cloud-init's", map[string]string{grantDir + "90-cloud-init-users": "# Created by cloud-init\n\n# User rules for alice\nalice ALL=(ALL) NOPASSWD:ALL\n"},
			engine.Configured, "non-root, sudo", noPass),
		// hardn's grant is "(ALL)": an earlier rule not read may still
		// decide "sudo -g", a later "(ALL:ALL)" covers it (TestOtherGrants).
		{name: "a rule not read before hardn's grant", site: site{plat: d, users: []string{alice}, files: merge(grant, map[string]string{grantDir + "10-site": "User_Alias ADMINS = bob\nADMINS ALL=(ALL) ALL\n"})},
			state: engine.Configured, want: "non-root, sudo", codes: []string{notRead}},
		{name: "a rule not read before a grant with any group", site: site{plat: d, users: []string{alice}, files: map[string]string{grantDir + "10-site": "User_Alias ADMINS = bob\nADMINS ALL=(ALL) ALL\n", grantDir + "zz-site": "alice ALL=(ALL:ALL) ALL\n"}},
			state: engine.Configured, want: "non-root, sudo"},
		inSudo("a rule not read after the grant", dropIn("alice ALL=(ALL) !ALL\n"), engine.Unknown, "can't tell who has sudo", notRead),
		inSudo("an alias after the grant", dropIn("User_Alias ADMINS = alice\nADMINS ALL=(ALL) !ALL\n"), engine.Unknown, "can't tell who has sudo", notRead),
		inSudo("every user after the grant", dropIn("ALL ALL=(ALL) !ALL\n"), engine.Unknown, "can't tell who has sudo", notRead),
		inSudo("a list after the grant", dropIn("bob, alice ALL=(ALL) !ALL\n"), engine.Unknown, "can't tell who has sudo", notRead),
		inSudo("another account's rule after", dropIn("bob ALL=(root) NOPASSWD: /usr/bin/systemctl restart app\n"), engine.Configured, "non-root, sudo"),
		inSudo("another group's rule after", dropIn("%adm ALL=(root) /usr/bin/journalctl\n"), engine.Configured, "non-root, sudo"),
		inSudo("a group /etc/group lacks", dropIn("%ldap-admins ALL=(ALL) !ALL\n"), engine.Unknown, "can't tell who has sudo", notRead),
		inSudo("the name in another case", dropIn("Alice ALL=(ALL) !ALL\n"), engine.Unknown, "can't tell who has sudo", notRead),
		{name: "a grant with a comment", site: site{plat: d, users: []string{alice}, files: dropIn("alice ALL=(ALL:ALL) ALL # admin\n")}, state: engine.Configured, want: "non-root, sudo"},
		{name: "a user ID after the includedir", site: site{plat: d, users: []string{alice}, members: sudoGroup, files: map[string]string{sudoers.MainConfig: readStock(t, d, "sudoers") + "#1000 ALL=(ALL) !ALL\n"}},
			state: engine.Unknown, want: "can't tell who has sudo", codes: []string{notRead}},
		inSudo("a continued line", dropIn("alice ALL=(ALL) \\\n  NOPASSWD: ALL\n"), engine.Unknown, "can't tell who has sudo", notRead),
		inSudo("a continued comment", dropIn("# admins \\\nbob ALL=(ALL) ALL\n"), engine.Unknown, "can't tell who has sudo", notRead),
		inSudo("files sudo skips", map[string]string{grantDir + "zz.conf": "alice ALL=(ALL) !ALL\n", grantDir + "zz~": "alice ALL=(ALL) !ALL\n"}, engine.Configured, "non-root, sudo"),
		inSudo("root's password asked, before the grant", mainWith("Defaults rootpw\n"), engine.Unknown, "can't tell who has sudo", notRead),
		inSudo("the target's password asked", dropIn("Defaults targetpw\n"), engine.Unknown, "can't tell who has sudo", notRead),
		inSudo("the runas password asked, for alice", dropIn("Defaults:alice runaspw\n"), engine.Unknown, "can't tell who has sudo", notRead),
		inSudo("no password asked, for alice", dropIn("Defaults:alice !authenticate\n"), engine.Unknown, "can't tell who has sudo", notRead),
		inSudo("no password asked, for a group", dropIn("Defaults:%sudo authenticate, !!authenticate\n"), engine.Unknown, "can't tell who has sudo", notRead),
		inSudo("root's password asked, for bob", dropIn("Defaults:bob rootpw\n"), engine.Configured, "non-root, sudo"),
		inSudo("the defaults as shipped", dropIn("Defaults !rootpw, authenticate\nDefaults env_keep += \"rootpw\" # rootpw\n"), engine.Configured, "non-root, sudo"),
		inSudo("another file included", mainWith("@include /etc/sudoers.local\n"), engine.Unknown, "can't tell who has sudo", notRead),
		{name: "nologin", site: site{plat: d, users: []string{"alice:x:1000:27::/home/alice:/usr/sbin/nologin"}}, state: engine.NotConfigured, want: "root only"},
		{name: "true", site: site{plat: d, users: []string{"alice:x:1000:27::/home/alice:/bin/true"}}, state: engine.NotConfigured, want: "root only"},
		{name: "git-shell", site: site{plat: d, users: []string{"alice:x:1000:27::/home/alice:/usr/bin/git-shell"}}, state: engine.NotConfigured, want: "root only"},
		{name: "shell not installed", site: site{plat: d, users: []string{"alice:x:1000:27::/home/alice:/bin/zsh"}}, state: engine.NotConfigured, want: "root only"},
		{name: "a system account", site: site{plat: d, users: []string{"backup:x:34:27::/var/backups:/bin/bash"}}, state: engine.NotConfigured, want: "root only"},
		{name: "a second root", site: site{plat: d, users: []string{"george:x:0:0::/root:/bin/bash"}}, state: engine.NotConfigured, want: "root only", codes: []string{uid0}},
		{name: "a second line for a name", site: site{plat: d, users: []string{"alice:x:0:0::/root:/bin/bash", alice}, members: sudoGroup}, state: engine.NotConfigured, want: "root only", codes: []string{uid0}},
		{name: "sudo not installed", site: site{plat: with(d, func(f *detect.Facts) { f.Elevation.Sudo = no }), users: []string{alice}, members: sudoGroup}, state: engine.Partial, want: "non-root, no sudo"},
		{name: "sudo not setuid", site: site{plat: d, users: []string{alice}, members: sudoGroup, bare: sudoYes.Path}, state: engine.Partial, want: "non-root, no sudo"},
		{name: "sudo not root's", site: site{plat: d, users: []string{alice}, members: sudoGroup, seed: func(h *hostfake.System) { h.AddFile(sudoYes.Path, nil, 0o755, 1000, 0) }},
			state: engine.Partial, want: "non-root, no sudo"},
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
		return map[string]string{sudoers.DoasDir + "/zz-site.conf": rule + "\n"}
	}
	both := with(a, func(f *detect.Facts) { f.Elevation.Sudo, f.Elevation.SudoFlavor = sudoYes, detect.SudoOriginal })
	inWheel := func(name string, files map[string]string, state engine.State, want string, codes ...string) row {
		return row{name: name, site: site{plat: a, users: []string{aliceSh}, members: wheel, files: files}, state: state, want: want, codes: codes}
	}
	runAll(t, []row{
		{name: "ubuntu, installer's user", site: site{plat: u, users: []string{alice}, members: sudoGroup}, state: engine.Configured, want: "non-root, sudo"},
		{name: "ubuntu, sudo-rs's own file", site: site{plat: u, users: []string{alice}, members: sudoGroup,
			files: map[string]string{sudoers.SudoersRS: "root ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n"}}, state: engine.Partial, want: "non-root, no sudo"},
		{name: "alpine, setup-alpine's user", site: site{plat: a, users: []string{"alice:x:1000:1000:alice:/home/alice:/bin/sh"}, members: wheel}, state: engine.Configured, want: "non-root, doas"},
		{name: "alpine, not in wheel", site: site{plat: a, users: []string{aliceSh}}, state: engine.Partial, want: "non-root, no doas"},
		{name: "alpine, hardn's rule", site: site{plat: a, users: []string{aliceSh}, files: written(t, a, sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword, Env: []string{"HARDN_CONFIG"}})},
			state: engine.Configured, want: "non-root, doas"},
		{name: "alpine, no password", site: site{plat: a, users: []string{aliceSh}, files: doasFile("permit nopass alice as root")},
			state: engine.Configured, want: "non-root, doas", codes: []string{noPass}},
		{name: "alpine, a rule with a comment", site: site{plat: a, users: []string{aliceSh}, files: doasFile("permit persist alice # admin, \"wheel\" later")},
			state: engine.Configured, want: "non-root, doas"},
		inWheel("alpine, denied after wheel", doasFile("deny alice"), engine.Partial, "non-root, no doas"),
		inWheel("alpine, as root after nopass", doasFile("permit nopass alice\npermit alice as root"), engine.Configured, "non-root, doas", noPass),
		inWheel("alpine, every target after nopass as root", doasFile("permit nopass alice as root\npermit alice"), engine.Configured, "non-root, doas"),
		inWheel("alpine, a command rule", doasFile("deny alice cmd reboot"), engine.Unknown, "can't tell who has doas", notRead),
		inWheel("alpine, another account's command rule", doasFile("deny bob cmd reboot"), engine.Configured, "non-root, doas"),
		inWheel("alpine, a quoted rule", doasFile(`permit setenv { PS1="# " } bob`), engine.Unknown, "can't tell who has doas", notRead),
		{name: "alpine, a second account of the uid", site: site{plat: a, users: []string{aliceSh, "bob:x:1000:1000::/home/bob:/bin/sh"}, members: wheel, files: doasFile("deny bob")},
			state: engine.Unknown, want: "can't tell who has doas", codes: []string{notRead, notRead}},
		{name: "alpine, doas and sudo", site: site{plat: both, users: []string{aliceSh}, members: wheel, files: map[string]string{sudoers.MainConfig: "@includedir /etc/sudoers.d\n"}},
			state: engine.Configured, want: "non-root, doas"},
		{name: "alpine, neither grants", site: site{plat: both, users: []string{aliceSh}, files: map[string]string{sudoers.MainConfig: "@includedir /etc/sudoers.d\n"}},
			state: engine.Partial, want: "non-root, no sudo or doas"},
		{name: "alpine, neither installed", site: site{plat: with(a, func(f *detect.Facts) { f.Elevation.Doas = no }), users: []string{aliceSh}, members: wheel},
			state: engine.Partial, want: "non-root, no doas"},
		{name: "proxmox, stock", site: site{plat: p}, state: engine.NotConfigured, want: "root only"},
		{name: "proxmox, a user, no sudo", site: site{plat: p, users: []string{alice}, members: sudoGroup}, state: engine.Partial, want: "non-root, no sudo"},
		{name: "proxmox, sudo installed", site: site{plat: with(p, func(f *detect.Facts) { f.Elevation.Sudo = sudoYes }), users: []string{alice}, members: sudoGroup,
			files: map[string]string{sudoers.MainConfig: readStock(t, debian13(), "sudoers")}}, state: engine.Configured, want: "non-root, sudo"},
	})
}

// TestUnknown: every read that fails, and every fact not established,
// where the state depends on it; and states established without them.
func TestUnknown(t *testing.T) {
	d, a := debian13(), alpine324()
	denied := func(op, path string) map[string]error { return map[string]error{op + " " + path: fs.ErrPermission} }
	sudoUnknown := with(d, func(f *detect.Facts) { f.Elevation.Sudo = detect.Tool{Err: fs.ErrPermission} })
	doasUnknown := with(a, func(f *detect.Facts) { f.Elevation.Doas = detect.Tool{Err: fs.ErrPermission} })
	doasOnDebian := with(d, func(f *detect.Facts) { f.Elevation.Doas = doasYes })
	distroUnknown := with(a, func(f *detect.Facts) { f.Platform = detect.Platform{} })
	wheel := map[string]string{"wheel": "alice"}
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
		{name: "sudo's mode unreadable", site: site{plat: d, users: []string{alice}, members: sudoGroup, fail: denied("stat", sudoYes.Path)}, state: engine.Unknown, want: "can't read", err: "PERMISSION_DENIED"},
		{name: "doas not established", site: site{plat: doasUnknown, users: []string{aliceSh}, members: wheel}, state: engine.Unknown, want: "can't tell if doas installed", err: "PERMISSION_DENIED"},
		{name: "doas off Alpine", site: site{plat: doasOnDebian, users: []string{alice}}, state: engine.Unknown, want: "can't tell who has doas", err: "SUDOERS_DOAS_UNSUPPORTED"},
		{name: "doas off Alpine, sudo grants", site: site{plat: doasOnDebian, users: []string{alice}, members: sudoGroup}, state: engine.Configured, want: "non-root, sudo", codes: []string{notRead}},
		{name: "doas, distribution not established", site: site{plat: distroUnknown, users: []string{aliceSh}, members: wheel}, state: engine.Unknown, want: "can't tell who has doas", err: "DETECT_NO_SIGNAL"},
		{name: "doas.conf missing", site: site{plat: a, users: []string{aliceSh}, members: wheel, files: map[string]string{sudoers.DoasConfig: ""}},
			state: engine.Unknown, want: "can't read", err: "SUDOERS_CHECK_FAILED"},
		{name: "visudo cannot run", site: site{plat: d, users: []string{alice}, members: sudoGroup, runErr: host.ErrNotFound}, state: engine.Unknown, want: "can't read", err: "SUDOERS_CHECK_FAILED"},
		{name: "visudo rejects", site: site{plat: d, users: []string{alice}, members: sudoGroup, reject: sudoers.MainConfig}, state: engine.Unknown, want: "sudo config invalid", err: "SUDOERS_INVALID_BEFORE"},
		{name: "visudo rejects, root only", site: site{plat: d, reject: sudoers.MainConfig}, state: engine.NotConfigured, want: "root only"},
		{name: "doas -C rejects", site: site{plat: a, users: []string{aliceSh}, members: wheel, reject: sudoers.DoasDir + "/20-wheel.conf"},
			state: engine.Unknown, want: "doas config invalid", err: "SUDOERS_INVALID_BEFORE"},
	})
}

// TestEvidence: what each state was read from, one line per source; an
// error the check returns is not repeated there, any other is short.
func TestEvidence(t *testing.T) {
	cases := []struct {
		row  row
		want []string
		find string // the first finding's detail and source
	}{{
		row: row{site: site{plat: debian13(), users: []string{alice, bob}, members: sudoGroup,
			files: map[string]string{grantDir + "90-cloud-init-users": "alice ALL=(ALL) NOPASSWD:ALL\n"}},
			state: engine.Configured, want: "non-root, sudo", codes: []string{noPass}},
		want: []string{
			"doas: not installed",
			"passwd: alice, uid 1000, /bin/bash",
			"sudo: alice by /etc/sudoers.d/90-cloud-init-users:1: alice ALL=(ALL) NOPASSWD:ALL",
			"passwd: bob, uid 1001, /bin/bash",
			"sudo: no rule for bob",
		},
		find: "alice: sudo without password /etc/sudoers.d/90-cloud-init-users:1",
	}, {
		row: row{site: site{plat: alpine324(), users: []string{"alice:x:1000:1000::/home/alice:"}, members: map[string]string{"wheel": "root,alice"},
			files: map[string]string{sudoers.DoasDir + "/zz.conf": "deny alice\npermit :wheel cmd apk\n"}},
			state: engine.Unknown, want: "can't tell who has doas", codes: []string{notRead}},
		want: []string{
			"sudo: not installed",
			"passwd: alice, uid 1000, /bin/sh",
			"doas: alice may be decided by /etc/doas.d/zz.conf:2: permit :wheel cmd apk",
		},
		find: "may decide doas for alice /etc/doas.d/zz.conf:2",
	}, {
		row:  row{site: site{plat: debian13()}, state: engine.NotConfigured, want: "root only"},
		want: []string{"passwd: no account with uid 1000 or more and a login shell"},
	}, {
		row: row{site: site{plat: with(debian13(), func(f *detect.Facts) { f.Elevation.Doas = doasYes }), users: []string{alice}, members: sudoGroup},
			state: engine.Configured, want: "non-root, sudo", codes: []string{notRead}},
		want: []string{
			"doas: not read: doas drop-ins are established on Alpine only",
			"passwd: alice, uid 1000, /bin/bash",
			"sudo: alice by /etc/sudoers:22: %sudo ALL=(ALL:ALL) ALL",
		},
	}, {
		row: row{site: site{plat: debian13(), users: []string{alice}, members: sudoGroup, reject: sudoers.MainConfig},
			state: engine.Unknown, want: "sudo config invalid", err: "SUDOERS_INVALID_BEFORE"},
		want: []string{"sudo: not read", "doas: not installed", "passwd: alice, uid 1000, /bin/bash"},
	}, {
		row: row{site: site{plat: with(alpine324(), func(f *detect.Facts) { f.Elevation.Sudo, f.Elevation.SudoFlavor = sudoYes, detect.SudoOriginal }),
			users: []string{aliceSh}, members: map[string]string{"wheel": "alice"}, files: map[string]string{sudoers.MainConfig: "@includedir /etc/sudoers.d\n"}, reject: sudoers.MainConfig},
			state: engine.Configured, want: "non-root, doas", codes: []string{notRead}},
		want: []string{
			"sudo: not read: visudo -c: syntax error at line 1",
			"passwd: alice, uid 1000, /bin/sh",
			"doas: alice by /etc/doas.d/20-wheel.conf:1: permit persist :wheel",
		},
	}, {
		row: row{site: site{plat: debian13(), users: []string{alice}, members: sudoGroup, bare: sudoYes.Path},
			state: engine.Partial, want: "non-root, no sudo"},
		want: []string{"sudo: /usr/bin/sudo not setuid root", "doas: not installed", "passwd: alice, uid 1000, /bin/bash"},
	}, {
		row: row{site: site{plat: debian13(), users: []string{alice, "bob:x:1001:1001::/home/bob:/bin/sh"}, fail: map[string]error{"stat /bin/bash": fs.ErrPermission}},
			state: engine.Partial, want: "non-root, no sudo"},
		want: []string{
			"doas: not installed",
			"passwd: alice, uid 1000, /bin/bash, shell not read: stat /bin/bash: permission denied",
			"sudo: no rule for alice",
			"passwd: bob, uid 1001, /bin/sh",
			"sudo: no rule for bob",
		},
	}}
	for _, c := range cases {
		st := run(t, c.row)
		if !slices.Equal(st.Evidence, c.want) {
			t.Errorf("evidence\n%q\nwant\n%q", st.Evidence, c.want)
		}
		if c.find != "" && (len(st.Findings) == 0 || st.Findings[0].Detail+" "+st.Findings[0].Source != c.find) {
			t.Errorf("findings %+v, want %q", st.Findings, c.find)
		}
	}
}

// aliceHash is a made-up password hash: no run of four of its characters
// may leave the check (next/p4-locked).
const aliceHash = "$6$Rt5uWq2x$Lm8nBv3Cz6Xa1Sd4Fg7Hj0Kp9Qw"

// TestLocked: an account whose password is locked ("!") cannot log in
// where sshd does not use PAM (auth.c allowed_user), so the row does not
// count it (check-users.md §7): the Alpine VM (root "!", the login
// account "*"), an Alpine cloud image without the maintainer's snippet
// (the login account "!"), Debian and Ubuntu with PAM, UsePAM no on a
// build with PAM, and each lock and read that is not established.
func TestLocked(t *testing.T) {
	d, a, u, p := debian13(), alpine324(), ubuntu2604(), pve9()
	wheel := map[string]string{"wheel": "root,alice"}
	nopass := map[string]string{sudoers.DoasDir + "/wheel.conf": "permit nopass :wheel\n"}
	lock := func(field string) map[string]string { return map[string]string{"root": "!", "alice": field} }
	vm := func(locks map[string]string) site {
		return site{plat: a, users: []string{aliceSh}, members: wheel, files: nopass, locks: locks, sshd: sshdNoPAM}
	}
	twice := sshdPAM + "usepam no\n"
	// An Alpine container: no OpenRC, so which sshd build runs is not
	// established, and sshd.pam's -G is asked (check-ssh.md §2.11).
	container := a
	container.facts.Init = detect.Init{Err: errors.New("detect: init: no signal")}
	container.facts.SSH = detect.SSH{Err: errors.New("detect: ssh: init system not established")}
	container.facts.Container = detect.Container{Detected: detect.Yes, Signal: "/.dockerenv"}
	inContainer := func(pamG string) site {
		s := vm(lock("!"))
		s.plat, s.pamG = container, pamG
		return s
	}
	// Ubuntu after a boot, before the first SSH connection: ssh.socket
	// listens, ssh.service has not started, /run/sshd is not there.
	socket := u
	socket.facts.SSH = detect.SSH{Mode: detect.SSHSocket, Socket: detect.UnitState{Enabled: "enabled", Active: "active"},
		Service: detect.UnitState{Enabled: "disabled", Active: "inactive"}}
	noRunDir := &hostfake.Response{Stderr: "Missing privilege separation directory: /run/sshd\r\n", ExitCode: 255}
	runAll(t, []row{
		{name: "alpine: the VM, root !, alice *", site: vm(lock("*")), state: engine.Configured, want: "non-root, doas", codes: []string{noPass}},
		{name: "alpine: a cloud image, alice !", site: vm(lock("!")), state: engine.NotConfigured, want: "non-root, locked", codes: []string{users.CodeLocked}},
		{name: "alpine: !!", site: vm(lock("!!")), state: engine.NotConfigured, want: "non-root, locked", codes: []string{users.CodeLocked}},
		{name: "alpine: ! before a hash", site: vm(lock("!" + aliceHash)), state: engine.NotConfigured, want: "non-root, locked", codes: []string{users.CodeLocked}},
		{name: "alpine: a hash", site: vm(lock(aliceHash)), state: engine.Configured, want: "non-root, doas", codes: []string{noPass}},
		{name: "alpine: an empty field", site: vm(lock("")), state: engine.Configured, want: "non-root, doas", codes: []string{noPass}},
		{name: "alpine: alice locked, bob may log in", site: site{plat: a, users: []string{aliceSh, "bob:x:1001:1001::/home/bob:/bin/sh"},
			members: map[string]string{"wheel": "root,alice,bob"}, locks: lock("!"), sshd: sshdNoPAM},
			state: engine.Configured, want: "non-root, doas", codes: []string{users.CodeLocked}},
		{name: "alpine: alice locked, bob not in wheel", site: site{plat: a, users: []string{aliceSh, "bob:x:1001:1001::/home/bob:/bin/sh"}, members: wheel, locks: lock("!"), sshd: sshdNoPAM},
			state: engine.Partial, want: "non-root, no doas", codes: []string{users.CodeLocked}},
		{name: "alpine: alice locked, not in wheel", site: site{plat: a, users: []string{aliceSh}, locks: lock("!"), sshd: sshdNoPAM},
			state: engine.NotConfigured, want: "non-root, locked", codes: []string{users.CodeLocked}},
		{name: "alpine: no shadow line, passwd's field locked", site: site{plat: a, users: []string{"alice:!:1000:1000::/home/alice:/bin/sh"}, members: wheel, sshd: sshdNoPAM,
			files: map[string]string{accounts.ShadowFile: "root:!:20000:0:99999:7:::\n"}},
			state: engine.NotConfigured, want: "non-root, locked", codes: []string{users.CodeLocked}},
		{name: "alpine: shadow unreadable", site: site{plat: a, users: []string{aliceSh}, members: wheel, sshd: sshdNoPAM, fail: map[string]error{"read " + accounts.ShadowFile: fs.ErrPermission}},
			state: engine.Unknown, want: "can't read", err: "PERMISSION_DENIED"},
		{name: "alpine: a malformed shadow", site: site{plat: a, users: []string{aliceSh}, members: wheel, sshd: sshdNoPAM, files: map[string]string{accounts.ShadowFile: "alice:!\n"}},
			state: engine.Unknown, want: "can't read", err: "ACCOUNTS_MALFORMED"},
		{name: "alpine: /etc/tcb read first", site: site{plat: a, users: []string{aliceSh}, members: wheel, sshd: sshdNoPAM, files: map[string]string{"/etc/tcb/alice/shadow": "alice:*:20000:0:99999:7:::\n"}},
			state: engine.Unknown, want: "can't read", err: "ACCOUNTS_TCB"},
		{name: "shadow unreadable, root only", site: site{plat: a, fail: map[string]error{"read " + accounts.ShadowFile: fs.ErrPermission}},
			state: engine.NotConfigured, want: "root only"},
		// With PAM sshd skips the lock: the row as before.
		{name: "debian: UsePAM yes, alice !", site: site{plat: d, users: []string{alice}, members: sudoGroup, locks: lock("!"), sshd: sshdPAM},
			state: engine.Configured, want: "non-root, sudo"},
		{name: "ubuntu: UsePAM yes, alice !", site: site{plat: u, users: []string{alice}, members: sudoGroup, locks: lock("!"), sshd: sshdPAM},
			state: engine.Configured, want: "non-root, sudo"},
		{name: "debian: UsePAM yes, shadow unreadable", site: site{plat: d, users: []string{alice}, members: sudoGroup, sshd: sshdPAM, fail: map[string]error{"read " + accounts.ShadowFile: fs.ErrPermission}},
			state: engine.Configured, want: "non-root, sudo"},
		{name: "debian: UsePAM no, alice !", site: site{plat: d, users: []string{alice}, members: sudoGroup, locks: lock("!"), sshd: strings.Replace(sshdPAM, "usepam yes", "usepam no", 1)},
			state: engine.NotConfigured, want: "non-root, locked", codes: []string{users.CodeLocked}},
		{name: "debian: usepam printed twice, alice !", site: site{plat: d, users: []string{alice}, members: sudoGroup, locks: lock("!"), sshd: twice},
			state: engine.Unknown, want: "can't read", codes: []string{"SSHD_EFFECTIVE_UNKNOWN"}},
		{name: "debian: usepam printed twice, alice *", site: site{plat: d, users: []string{alice}, members: sudoGroup, sshd: twice},
			state: engine.Configured, want: "non-root, sudo"},
		// sshd -T failing: the SSH rows' words for it.
		{name: "debian: no sshd_config, alice !", site: site{plat: d, users: []string{alice}, members: sudoGroup, locks: lock("!")},
			state: engine.Unknown, want: "no sshd_config", err: "FILE_NOT_FOUND"},
		{name: "debian: no sshd_config, alice not in sudo", site: site{plat: d, users: []string{alice}, locks: lock("!")},
			state: engine.Unknown, want: "no sshd_config", err: "FILE_NOT_FOUND"},
		{name: "ubuntu: alice !, sshd not started yet", site: site{plat: socket, users: []string{alice}, members: sudoGroup, locks: lock("!"), sshd: sshdPAM, failT: noRunDir},
			state: engine.Unknown, want: "sshd not started yet", err: "SSHD_ENV_NO_RUNDIR"},
		{name: "alpine container: alice !, sshd.pam would use PAM", site: inContainer("usepam yes\n"),
			state: engine.Unknown, want: "can't tell which sshd runs"},
		{name: "alpine container: alice !, sshd.pam would not", site: inContainer("usepam no\n"),
			state: engine.NotConfigured, want: "non-root, locked", codes: []string{users.CodeLocked}},
		{name: "alpine container: alice *, sshd.pam not asked", site: func() site { s := inContainer("usepam yes\n"); s.locks = lock("*"); return s }(),
			state: engine.Configured, want: "non-root, doas", codes: []string{noPass}},
		{name: "alpine container: sshd.pam's -G fails", site: func() site { s := inContainer("usepam yes\n"); s.failG = true; return s }(),
			state: engine.Unknown, want: "can't read", err: "COMMAND_FAILED"},
		{name: "proxmox: a user, no sudo, alice !", site: site{plat: p, users: []string{alice}, members: sudoGroup, locks: lock("!"), sshd: sshdPAM},
			state: engine.Partial, want: "non-root, no sudo"},
	})

	// The evidence names each lock that may decide, with no hash, and
	// sshd's PAM; the finding is at the lock's line.
	for _, c := range []struct {
		row  row
		want []string
	}{{
		row: row{site: vm(lock("!" + aliceHash)), state: engine.NotConfigured, want: "non-root, locked", codes: []string{users.CodeLocked}},
		want: []string{
			"sudo: not installed",
			"passwd: alice, uid 1000, /bin/sh",
			"/etc/shadow:13: alice locked (!)",
			"doas: alice by /etc/doas.d/wheel.conf:1: permit nopass :wheel",
			"sshd -T: no usepam (built without PAM)",
		},
	}, {
		row: row{site: vm(lock(aliceHash)), state: engine.Configured, want: "non-root, doas", codes: []string{noPass}},
		want: []string{
			"sudo: not installed",
			"passwd: alice, uid 1000, /bin/sh",
			"doas: alice by /etc/doas.d/wheel.conf:1: permit nopass :wheel",
		},
	}, {
		row: row{site: site{plat: d, users: []string{alice}, members: sudoGroup, locks: lock("!" + aliceHash), sshd: sshdPAM}, state: engine.Configured, want: "non-root, sudo"},
		want: []string{
			"doas: not installed",
			"passwd: alice, uid 1000, /bin/bash",
			"/etc/shadow:10: alice locked (!)",
			"sudo: alice by /etc/sudoers:22: %sudo ALL=(ALL:ALL) ALL",
			"sshd -T: usepam yes",
		},
	}, {
		row: row{site: site{plat: d, users: []string{alice}, members: sudoGroup, sshd: sshdPAM, fail: map[string]error{"read " + accounts.ShadowFile: fs.ErrPermission}},
			state: engine.Configured, want: "non-root, sudo"},
		want: []string{
			"doas: not installed",
			"passwd: alice, uid 1000, /bin/bash",
			"/etc/shadow: alice not read: read /etc/shadow: permission denied",
			"sudo: alice by /etc/sudoers:22: %sudo ALL=(ALL:ALL) ALL",
			"sshd -T: usepam yes",
		},
	}, {
		row: row{site: site{plat: a, users: []string{aliceSh}, members: wheel, sshd: sshdNoPAM, fail: map[string]error{"read " + accounts.ShadowFile: fs.ErrPermission}},
			state: engine.Unknown, want: "can't read", err: "PERMISSION_DENIED"},
		want: []string{
			"sudo: not installed",
			"passwd: alice, uid 1000, /bin/sh",
			"/etc/shadow: alice not read",
			"doas: alice by /etc/doas.d/20-wheel.conf:1: permit persist :wheel",
			"sshd -T: no usepam (built without PAM)",
		},
	}} {
		st := run(t, c.row)
		if !slices.Equal(st.Evidence, c.want) {
			t.Errorf("evidence\n%q\nwant\n%q", st.Evidence, c.want)
		}
		if c.row.state == engine.NotConfigured && (st.Findings[0].Detail != "alice: locked, no login" || st.Findings[0].Source != "/etc/shadow:13") {
			t.Errorf("finding %+v", st.Findings[0])
		}
		for i := 0; i+4 <= len(aliceHash); i++ {
			if strings.Contains(fmt.Sprintf("%+v", st), aliceHash[i:i+4]) {
				t.Errorf("the status holds %q of alice's hash: %+v", aliceHash[i:i+4], st)
			}
		}
	}

	// No lock decides: neither sshd -T nor, with root alone, /etc/shadow.
	for _, s := range []site{vm(lock("*")), {plat: a, sshd: sshdNoPAM}} {
		sys, h := s.build(t)
		if _, err := (users.Admin{}).Check(context.Background(), sys, s.plat.facts); err != nil {
			t.Fatal(err)
		}
		for _, c := range h.Calls() {
			if c.Op == "run" && sshdT(c.Argv) || len(s.users) == 0 && c.Path == accounts.ShadowFile {
				t.Errorf("%v", c)
			}
		}
	}
}

// TestOtherGrants: beside alice's grant, which makes the row Configured,
// every other grant without a password is USERS_NOPASSWD and every grant
// not established USERS_RULE_NOT_READ, at its rule: the risk review's
// cases (the same grant for bob read, on a continued line, by an alias,
// in a list), a Defaults, a command rule, a tool not read (each source),
// a shell not read; a rule before the deciding one that still decides
// "sudo -u root -g bob" (sudo's "(ALL)" and "(root)" without ":ALL",
// hardn's own line); and a grant for an account that cannot log in, or
// below uid 1000, which is not looked at.
func TestOtherGrants(t *testing.T) {
	d := debian13()
	both := []string{alice, bob}
	doasOn := func(tl detect.Tool) platform { return with(d, func(f *detect.Facts) { f.Elevation.Doas = tl }) }
	alpineBoth := with(alpine324(), func(f *detect.Facts) { f.Elevation.Sudo, f.Elevation.SudoFlavor = sudoYes, detect.SudoOriginal })
	alpineSudo := map[string]string{sudoers.MainConfig: "@includedir /etc/sudoers.d\n", grantDir + "zz-site": "alice ALL=(ALL:ALL) ALL\n"}
	twoFiles := func(first, second string) map[string]string {
		return map[string]string{grantDir + "10-bob": first, grantDir + "90-bob": second}
	}
	top := func(lines string) map[string]string { // before %sudo, whose grant to alice then decides
		return map[string]string{sudoers.MainConfig: lines + readStock(t, d, "sudoers")}
	}
	dropIn := func(rules string) map[string]string { return map[string]string{grantDir + "zz-site": rules} }
	cases := []struct {
		row
		find []string // each finding's detail and source
	}{
		{row{name: "read", site: site{users: both, files: dropIn("bob ALL=(ALL) NOPASSWD: ALL\n")}, state: engine.Configured, codes: []string{noPass}},
			[]string{"bob: sudo without password /etc/sudoers.d/zz-site:1"}},
		{row{name: "a continued line", site: site{users: both, files: dropIn("bob ALL=(ALL) \\\n  NOPASSWD: ALL\n")}, state: engine.Configured, codes: []string{notRead}},
			[]string{"may decide sudo for bob /etc/sudoers.d/zz-site:1"}},
		{row{name: "an alias", site: site{users: both, files: top("User_Alias ADMINS = bob\nADMINS ALL=(ALL) NOPASSWD: ALL\n")}, state: engine.Configured, codes: []string{notRead}},
			[]string{"may decide sudo for bob /etc/sudoers:2"}},
		{row{name: "a list", site: site{users: both, files: top("bob, george ALL=(ALL) NOPASSWD: ALL\n")}, state: engine.Configured, codes: []string{notRead}},
			[]string{"may decide sudo for bob /etc/sudoers:1"}},
		{row{name: "an alias after alice's grant", site: site{users: both, files: dropIn("User_Alias ADMINS = bob\nADMINS ALL=(ALL) NOPASSWD: ALL\n")},
			state: engine.Unknown, want: "can't tell who has sudo", codes: []string{notRead, notRead}}, nil},
		{row{name: "no password asked of bob", site: site{users: both, files: dropIn("bob ALL=(ALL) ALL\nDefaults:bob !authenticate\n")}, state: engine.Configured, codes: []string{notRead}},
			[]string{"may decide sudo for bob /etc/sudoers.d/zz-site:2"}},
		{row{name: "a command rule for bob", site: site{users: both, files: dropIn("bob ALL=(root) NOPASSWD: /usr/bin/systemctl restart app\n")}, state: engine.Configured, codes: []string{notRead}},
			[]string{"may decide sudo for bob /etc/sudoers.d/zz-site:1"}},
		{row{name: "bob's grant with a password", site: site{users: both, files: dropIn("bob ALL=(ALL:ALL) ALL\n")}, state: engine.Configured}, nil},
		{row{name: "doas not read", site: site{plat: with(d, func(f *detect.Facts) { f.Elevation.Doas = doasYes }), users: both}, state: engine.Configured, codes: []string{notRead, notRead}},
			[]string{"may decide doas for alice ", "may decide doas for bob "}},
		{row{name: "bob's shell not read", site: site{users: []string{alice, "bob:x:1001:1001::/home/bob:/bin/sh"}, fail: map[string]error{"stat /bin/sh": fs.ErrPermission},
			files: dropIn("bob ALL=(ALL) NOPASSWD: ALL\n")}, state: engine.Configured, codes: []string{noPass}},
			[]string{"bob: sudo without password /etc/sudoers.d/zz-site:1"}},
		{row{name: "bob's shell not read, a continued line", site: site{users: []string{alice, "bob:x:1001:1001::/home/bob:/bin/sh"}, fail: map[string]error{"stat /bin/sh": fs.ErrPermission},
			files: dropIn("bob ALL=(ALL) \\\n  NOPASSWD: ALL\n")}, state: engine.Configured, codes: []string{notRead}},
			[]string{"may decide sudo for bob /etc/sudoers.d/zz-site:1"}},
		{row{name: "doas presence not established", site: site{plat: doasOn(detect.Tool{Err: fs.ErrPermission}), users: both}, state: engine.Configured, codes: []string{notRead, notRead}},
			[]string{"may decide doas for alice ", "may decide doas for bob "}},
		{row{name: "doas's mode not read", site: site{plat: doasOn(doasYes), users: []string{alice}, fail: map[string]error{"stat " + doasYes.Path: fs.ErrPermission}},
			state: engine.Configured, codes: []string{notRead}}, []string{"may decide doas for alice "}},
		{row{name: "doas's file not read, sudo grants", site: site{plat: alpineBoth, users: []string{aliceSh}, files: alpineSudo,
			fail: map[string]error{"read " + sudoers.DoasDir + "/20-wheel.conf": fs.ErrPermission}}, state: engine.Configured, codes: []string{notRead}},
			[]string{"may decide doas for alice "}},
		{row{name: "doas -C rejects, sudo grants", site: site{plat: alpineBoth, users: []string{aliceSh}, files: alpineSudo, reject: sudoers.DoasDir + "/20-wheel.conf"},
			state: engine.Configured, codes: []string{notRead}}, []string{"may decide doas for alice "}},
		{row{name: "a list, then bob as root", site: site{users: both, files: merge(top("bob, george ALL=(ALL:ALL) NOPASSWD: ALL\n"), dropIn("bob ALL=(ALL) ALL\n"))},
			state: engine.Configured, codes: []string{notRead}}, []string{"may decide sudo for bob /etc/sudoers:1"}},
		{row{name: "a continued line, then bob as root", site: site{users: both, files: twoFiles("bob ALL=(ALL:ALL) \\\n  NOPASSWD: ALL\n", "bob ALL=(ALL) ALL\n")},
			state: engine.Configured, codes: []string{notRead}}, []string{"may decide sudo for bob /etc/sudoers.d/10-bob:1"}},
		{row{name: "read, then hardn's grant", site: site{users: both, files: merge(map[string]string{grantDir + "10-bob": "bob ALL=(ALL:ALL) NOPASSWD: ALL\n"},
			written(t, d, sudoers.Policy{User: "bob", Grant: sudoers.GrantPassword}))}, state: engine.Configured, codes: []string{noPass}},
			[]string{"bob: sudo without password /etc/sudoers.d/10-bob:1"}},
		{row{name: "read, then root alone", site: site{users: both, files: twoFiles("bob ALL=(root:ALL) NOPASSWD: ALL\n", "bob ALL=(root) ALL\n")},
			state: engine.Configured, codes: []string{noPass}}, []string{"bob: sudo without password /etc/sudoers.d/10-bob:1"}},
		{row{name: "alice's continued line, then hers as root", site: site{users: both, files: map[string]string{grantDir + "10-alice": "alice ALL=(ALL:ALL) \\\n  NOPASSWD: ALL\n",
			grantDir + "90-alice": "alice ALL=(ALL) ALL\n"}}, state: engine.Configured, codes: []string{notRead}},
			[]string{"may decide sudo for alice /etc/sudoers.d/10-alice:1"}},
		{row{name: "a continued line, then bob with any group", site: site{users: both, files: twoFiles("bob ALL=(ALL:ALL) \\\n  NOPASSWD: ALL\n", "bob ALL=(ALL:ALL) ALL\n")},
			state: engine.Configured}, nil},
		{row{name: "root alone, then bob as any user", site: site{users: both, files: twoFiles("bob ALL=(root) NOPASSWD: ALL\n", "bob ALL=(ALL) ALL\n")},
			state: engine.Configured}, nil},
		{row{name: "bob cannot log in", site: site{users: []string{alice, "bob:x:1001:1001::/home/bob:/usr/sbin/nologin"}, files: dropIn("bob ALL=(ALL) \\\n  NOPASSWD: ALL\n")},
			state: engine.Configured}, nil},
		{row{name: "a system account", site: site{users: []string{alice, "backup:x:34:34::/var/backups:/bin/bash"}, files: dropIn("backup ALL=(ALL) \\\n  NOPASSWD: ALL\n")},
			state: engine.Configured}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.site.plat.dir == "" {
				c.site.plat = d
			}
			c.site.members = sudoGroup
			if c.want == "" {
				c.want = "non-root, sudo"
			}
			st := run(t, c.row)
			var got []string
			for _, f := range st.Findings {
				got = append(got, f.Detail+" "+f.Source)
			}
			if c.find != nil && !slices.Equal(got, c.find) {
				t.Errorf("findings %q, want %q", got, c.find)
			}
		})
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

// TestElevation: the reader the System Details report takes (pkg/engine/details)
// decides as the row does: a rule counts where group membership would miss it,
// as cloud-init's does, and what cannot be read is not established.
func TestElevation(t *testing.T) {
	ctx := context.Background()
	const cloudInit = sudoers.DropInDir + "/90-cloud-init-users"
	alice := "alice:x:1000:1000::/home/alice:/bin/bash"
	george := "george:x:1001:1001::/home/george:/bin/bash"
	read := func(t *testing.T, s site) (users.Elevation, accounts.DB) {
		t.Helper()
		sys, _ := s.build(t)
		db, err := accounts.Read(ctx, sys)
		if err != nil {
			t.Fatal(err)
		}
		return users.ReadElevation(ctx, sys, s.plat.facts), db
	}
	byName := func(t *testing.T, db accounts.DB, name string) accounts.User {
		t.Helper()
		i := slices.IndexFunc(db.Users, func(u accounts.User) bool { return u.Name == name })
		if i < 0 {
			t.Fatalf("no %s in %v", name, db.Users)
		}
		return db.Users[i]
	}

	t.Run("a rule, in no group", func(t *testing.T) {
		e, db := read(t, site{plat: debian13(), users: []string{alice, george},
			files: map[string]string{cloudInit: "alice ALL=(ALL) NOPASSWD:ALL\n"}})
		cloud := users.Rule{Kind: users.RuleFile, File: cloudInit, Line: 1, Text: "alice ALL=(ALL) NOPASSWD:ALL"}
		if got, want := e.GrantsOf(db, byName(t, db, "alice")), []users.Grant{{Tool: "sudo", Grants: detect.Yes, Rule: cloud, NoPassword: true}, {Tool: "doas", Grants: detect.No}}; !slices.Equal(got, want) {
			t.Errorf("alice: %v, want %v", got, want)
		}
		if got, want := e.GrantsOf(db, byName(t, db, "george")), []users.Grant{{Tool: "sudo", Grants: detect.No}, {Tool: "doas", Grants: detect.No}}; !slices.Equal(got, want) {
			t.Errorf("george: %v, want %v", got, want)
		}
		if got := e.NotRead(); len(got) != 0 {
			t.Errorf("not read: %v", got)
		}
	})
	t.Run("doas, by group", func(t *testing.T) {
		e, db := read(t, site{plat: alpine324(), users: []string{alice}, members: map[string]string{"wheel": "alice"}})
		wheel := users.Rule{Kind: users.RuleGroup, File: sudoers.DoasDir + "/20-wheel.conf", Line: 1, Group: "wheel", Text: "permit persist :wheel"}
		if got, want := e.GrantsOf(db, byName(t, db, "alice")), []users.Grant{{Tool: "sudo", Grants: detect.No}, {Tool: "doas", Grants: detect.Yes, Rule: wheel}}; !slices.Equal(got, want) {
			t.Errorf("alice: %v, want %v", got, want)
		}
	})
	t.Run("hardn's own, a group's, without a password", func(t *testing.T) {
		grant := written(t, debian13(), sudoers.Policy{User: "alice", Grant: sudoers.GrantNoPassword})
		e, db := read(t, site{plat: debian13(), users: []string{alice, george}, members: map[string]string{"sudo": "alice,george"}, files: grant})
		hardn := users.Rule{Kind: users.RuleHardn, File: grantDir + "hardn-grant-alice", Line: 2, Text: "alice ALL=(ALL) NOPASSWD: ALL"}
		if got := e.GrantsOf(db, byName(t, db, "alice"))[0]; got != (users.Grant{Tool: "sudo", Grants: detect.Yes, Rule: hardn, NoPassword: true}) {
			t.Errorf("alice: %+v", got)
		}
		group := users.Rule{Kind: users.RuleGroup, File: sudoers.MainConfig, Line: 22, Group: "sudo", Text: "%sudo ALL=(ALL:ALL) ALL"}
		if got := e.GrantsOf(db, byName(t, db, "george"))[0]; got != (users.Grant{Tool: "sudo", Grants: detect.Yes, Rule: group}) {
			t.Errorf("george: %+v", got)
		}
	})
	t.Run("rules not read", func(t *testing.T) {
		e, db := read(t, site{plat: debian13(), users: []string{alice},
			fail: map[string]error{"read " + sudoers.MainConfig: fs.ErrPermission}})
		if got, want := e.GrantsOf(db, byName(t, db, "alice")), []users.Grant{{Tool: "sudo", Grants: detect.Unknown}, {Tool: "doas", Grants: detect.No}}; !slices.Equal(got, want) {
			t.Errorf("alice: %v, want %v", got, want)
		}
		got := e.NotRead()
		if len(got) != 1 || got[0].Tool != "sudo" || !errors.Is(got[0].Err, fs.ErrPermission) {
			t.Errorf("not read: %v", got)
		}
	})
}

package backup_test

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	backupscreen "github.com/abbott/hardn/pkg/screens/backup"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The hosts the Backup screen's tests run on, on hostfake, as the
// gallery's Backup hosts were drawn (gallery-backup.md §3.1) and Backup's
// control's tests have them. Synthetic values only: web-01.example.net,
// alice, 192.0.2.0/24. hardnYML and now are parity_test.go's.

const (
	elsewhere  = "/srv/backups/hardn"
	unsafePath = "/tmp/hardn-backups"
	osRelease  = "/etc/os-release"
)

// debian13 are every host's facts, for the engine.
var debian13 = detect.Facts{Platform: detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
	Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}, Container: detect.Container{Detected: detect.No}}

// ymlOf is hardn.yml as hardn.yml.example writes the two keys, with
// backups as enabled and path say, under a comment.
func ymlOf(enabled bool, path string) []byte {
	return fmt.Appendf(nil, "# hardn's configuration (web-01.example.net)\nusername: alice\n"+
		"enableBackups: %t               # Backup files before modifying them\n"+
		"backupPath: %q  # Path to store backups\n", enabled, path)
}

// testHost is one host: the fake, with directories on a read-only file
// system and paths whose Lstat fails.
type testHost struct {
	*hostfake.System
	readOnly []string
	lstat    map[string]error
}

// onReadOnly reports whether name is under one of h.readOnly.
func (h *testHost) onReadOnly(name string) bool {
	return slices.ContainsFunc(h.readOnly, func(d string) bool { return name == d || strings.HasPrefix(name, d+"/") })
}

// MkdirAll fails with EROFS under a read-only directory.
func (h *testHost) MkdirAll(ctx context.Context, name string, perm fs.FileMode) error {
	if h.onReadOnly(name) {
		return &fs.PathError{Op: "mkdir", Path: name, Err: syscall.EROFS}
	}
	return h.System.MkdirAll(ctx, name, perm)
}

// WriteFileAtomic fails with EROFS under a read-only directory.
func (h *testHost) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	if h.onReadOnly(name) {
		return &fs.PathError{Op: "open", Path: name, Err: syscall.EROFS}
	}
	return h.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

// Lstat fails for a path in lstat.
func (h *testHost) Lstat(ctx context.Context, name string) (fs.FileInfo, error) {
	if err := h.lstat[name]; err != nil {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: err}
	}
	return h.System.Lstat(ctx, name)
}

// The hosts, by name.
const (
	hostDefault   = "default"    // backups on, /var/backups/hardn not made yet
	hostOff       = "off"        // enableBackups: false
	hostMade      = "made"       // /var/backups/hardn made, root's, 0700
	hostElsewhere = "elsewhere"  // backupPath: /srv/backups/hardn, made, root's, 0700
	hostUnsafe    = "unsafe"     // backupPath: /tmp/hardn-backups, /tmp anyone can write to
	hostRelative  = "relative"   // backupPath: backups, as origin/main saves a relative path
	hostFile      = "file"       // /var/backups/hardn a regular file
	hostUnderFile = "under-file" // backupPath: /etc/os-release/backups
	hostUnknown   = "unknown"    // /var/backups cannot be examined: Check cannot tell
	hostReadOnly  = "readonly"   // /var/backups on a read-only file system
	hostRuns      = "runs"       // three earlier runs in /var/backups/hardn
	hostSomeRuns  = "some-runs"  // the three, and one whose manifest is not one
	hostNoRuns    = "no-runs"    // /var/backups/hardn made, but not listable
)

// The runs hostRuns holds, newest last, each backing up its files.
var earlierRuns = []struct {
	id      string
	enabled bool
	files   []string
}{
	{"20261017T083000Z-9c0d1e2f", true, []string{"/etc/ssh/sshd_config.d/00-hardn.conf"}},
	{"20261018T091500Z-1a2b3c4d", true, []string{"/etc/resolv.conf", hardnYML}},
	{"20261019T143000Z-5e6f7a8b", false, []string{hardnYML}},
}

// newHost returns the host name names, with hardn.yml as its name says,
// and hardn.yml as read from it.
func newHost(t *testing.T, name string) (*testHost, *config.Config) {
	t.Helper()
	ctx := context.Background()
	h := &testHost{System: hostfake.New(host.Options{}), lstat: map[string]error{}}
	for _, d := range []string{"/etc", "/etc/hardn", "/etc/ssh", "/etc/ssh/sshd_config.d", "/var", "/var/lib", "/var/backups", "/run", "/var/log", "/srv", "/srv/backups"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddDir("/tmp", fs.ModeSticky|0o777, 0, 0)
	h.AddFile(osRelease, []byte("ID=debian\nVERSION_ID=\"13\"\n"), 0o644, 0, 0)
	h.AddFile("/etc/resolv.conf", []byte("# Generated by dhcpcd from eth0.dhcp\nnameserver 192.0.2.1\n"), 0o644, 0, 0)
	h.Handler = func([]string, []byte) hostfake.Response { return hostfake.Response{Err: hostfake.ErrUnscripted} }
	enabled, path := true, backupPath
	switch name {
	case hostDefault:
	case hostOff:
		enabled = false
	case hostMade:
		h.AddDir(backupPath, 0o700, 0, 0)
	case hostElsewhere:
		path = elsewhere
		h.AddDir(elsewhere, 0o700, 0, 0)
	case hostUnsafe:
		path = unsafePath
	case hostRelative:
		path = "backups"
	case hostFile:
		h.AddFile(backupPath, []byte("not a directory\n"), 0o644, 0, 0)
	case hostUnderFile:
		path = osRelease + "/backups"
	case hostUnknown:
		h.lstat["/var/backups"] = fs.ErrPermission
	case hostReadOnly:
		h.readOnly = []string{"/var/backups"}
	case hostRuns, hostSomeRuns:
		h.AddFile(earlierRuns[0].files[0], []byte("Port 2230\n"), 0o644, 0, 0)
		h.AddFile(hardnYML, ymlOf(true, backupPath), 0o600, 0, 0)
		for _, r := range earlierRuns {
			run, err := hostbackup.Open(h, hostbackup.Settings{Path: backupPath, Enabled: r.enabled}, r.id)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range r.files {
				if _, err := run.Backup(ctx, f, hostbackup.RollbackRequired); err != nil {
					t.Fatal(err)
				}
			}
		}
		if name == hostSomeRuns {
			h.AddDir(backupPath+"/20261016T000000Z-00000000", 0o700, 0, 0)
			h.AddFile(backupPath+"/20261016T000000Z-00000000/"+hostbackup.ManifestName, []byte("not a manifest\n"), 0o600, 0, 0)
		}
	case hostNoRuns:
		h.AddDir(backupPath, 0o700, 0, 0)
	default:
		t.Fatalf("no host %q", name)
	}
	h.AddFile(hardnYML, ymlOf(enabled, path), 0o600, 0, 0)
	cfg, err := config.Read(ctx, h, hardnYML)
	if err != nil {
		t.Fatal(err)
	}
	return h, cfg
}

// noReadDir is a host whose ReadDir of dir fails: the runs there cannot
// be listed.
type noReadDir struct {
	*testHost
	dir string
}

// ReadDir fails for n.dir.
func (n noReadDir) ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error) {
	if name == n.dir {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return n.testHost.ReadDir(ctx, name)
}

// played is one run of the screen: what it wrote, with the answers echoed,
// what each change did (Options.Ran), how many engines it made, what Run
// returned and how many lines were left unread.
type played struct {
	out     string
	runs    []output.Run
	engines int
	err     error
	unread  int
}

// play runs the Backup screen on sys with cfg, plain at 120 columns (not a
// terminal), the user typing lines; a dry run with dryRun.
func play(sys host.System, cfg *config.Config, dryRun bool, lines ...string) played {
	return playWith(sys, cfg, dryRun, nil, lines...)
}

// playWith is play with the options set changes, when not nil. Each
// change's engine is built as cmd/hardn builds it (engineOptions): its
// backups hardn.yml's as loaded.
func playWith(sys host.System, cfg *config.Config, dryRun bool, set func(*backupscreen.Options), lines ...string) played {
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	ty := uitest.NewTypist(&out, lines...)
	var p played
	loaded := cfg.BackupSettings()
	o := backupscreen.Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		System:   sys, Config: cfg, DryRun: dryRun, LogPath: output.LegacyLogPath,
		Now: func() time.Time { return now },
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			p.engines++
			return engine.New(sys, engine.Options{
				Revert:   revert.Options{Now: func() time.Time { return now }, Hardn: "/usr/local/bin/hardn"},
				Detect:   func(context.Context, host.System) (detect.Facts, error) { return debian13, nil },
				Hold:     func() func() { return func() {} },
				PID:      5000,
				Backups:  loaded,
				Progress: progress,
			})
		},
		Ran: func(r output.Run) { p.runs = append(p.runs, r) },
	}
	if set != nil {
		set(&o)
	}
	p.err = backupscreen.Run(context.Background(), o)
	p.out, p.unread = out.String(), ty.Unread()
	return p
}

// writes are the calls on f that changed something outside /run/hardn,
// the engine's run directory, from the nth on.
func writes(f *hostfake.System, n int) []string {
	var out []string
	for _, c := range f.Calls()[n:] {
		switch c.Op {
		case "write-atomic", "write-in-place", "remove", "removeall", "rename", "mkdir", "chmod", "chown", "append":
			if !strings.HasPrefix(c.Path, "/run/hardn") {
				out = append(out, c.Op+" "+c.Path)
			}
		}
	}
	return out
}

// content is name's content on f, or "<absent>".
func content(f *hostfake.System, name string) string {
	e, ok := f.Entry(name)
	if !ok {
		return "<absent>"
	}
	return string(e.Data)
}

// screens are the Backup screens out shows, each from its title bar to
// its prompt: one per round.
func screens(out string) []string {
	var got []string
	for _, part := range strings.Split(out, "# Backup Settings #")[1:] {
		end := strings.Index(part, "> Enter selection")
		if end < 0 {
			end = len(part)
		}
		got = append(got, part[:end])
	}
	return got
}

// row is the value of the screen's row key in s, "" when it has none.
func row(s, key string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); strings.Contains(t, key+"  ") {
			_, v, _ := strings.Cut(t, key)
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// item is the description of the screen's item key in s, "" when it has
// none.
func item(s, key string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, key+") ") {
			f := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(t, key+") ")), "  ", 2)
			if len(f) < 2 {
				return ""
			}
			return strings.TrimSpace(f[1])
		}
	}
	return ""
}

// flat is out with each callout's continued lines joined ("\n# "), a
// word broken after its hyphen made whole again, its white space as
// single spaces, so that a wrapped sentence reads whole.
func flat(out string) string {
	out = strings.ReplaceAll(out, "-\n# ", "-")
	return strings.Join(strings.Fields(strings.ReplaceAll(out, "\n# ", " ")), " ")
}

// inOrder returns the first of want that out does not show after what
// came before it, each compared flat; "" when it shows them all, in
// order.
func inOrder(out string, want ...string) string {
	f, at := flat(out), 0
	for _, w := range want {
		i := strings.Index(f[at:], strings.Join(strings.Fields(w), " "))
		if i < 0 {
			return w
		}
		at += i + 1
	}
	return ""
}

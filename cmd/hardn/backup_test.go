package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// The Backup item's tests (refactor plan slice 3, unit (d)): the Backup
// screen on the DNS tests' hosts (dns_test.go), whose hardn.yml sets no
// backup key, so backups are on, under /var/backups/hardn, not made yet.
// Synthetic values only.

// backupTestPath is the path the tests move backups to: /srv is root's,
// 0755, and the path not made yet.
const backupTestPath = "/srv/backups/hardn"

// under are the calls of writes that changed something under dir.
func under(writes []string, dir string) []string {
	var out []string
	for _, w := range writes {
		if _, p, _ := strings.Cut(w, " "); p == dir || strings.HasPrefix(p, dir+"/") {
			out = append(out, w)
		}
	}
	return out
}

// TestBackupScreen: the main menu's Backup item runs the Backup screen on
// the host hardn builds, through the engine with engineOptions: turning
// backups off applies, saves hardn.yml's one setting, its backup under
// /var/backups/hardn, and the configuration the main menu holds adopts it;
// a path change backs up under the new path, nothing under the old; a dry
// run, as the configuration says when the item is chosen, a decline and
// Verify write nothing; 0 goes back (nil), q quits (nav.ErrQuit), the end
// of the input aborts (prompt.ErrAborted). No audit record is written.
// The signals are registered for the apply alone, and never while a
// prompt waits.
func TestBackupScreen(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dryRun  bool
		lines   []string
		want    error  // what the screen returns: nil, nav.ErrQuit, prompt.ErrAborted
		said    string // on the screen
		enabled bool   // the configuration's enableBackups afterwards
		path    string // its backupPath afterwards
		backups string // the backup path written under; "": nothing written
	}{
		{"turn off, then return", false, []string{"1", "y", "0"}, nil, "Backed up anyway: the rollback needs it.", false, backup.DefaultPath, backup.DefaultPath},
		{"a path change", false, []string{"2", backupTestPath, "y", "0"}, nil, "Use " + backupTestPath + "?", true, backupTestPath, backupTestPath},
		{"turn off as a dry run", true, []string{"1", "0"}, nil, "No actual changes were made to your system.", true, backup.DefaultPath, ""},
		{"a path change as a dry run", true, []string{"2", backupTestPath, "0"}, nil, "No actual changes were made to your system.", true, backup.DefaultPath, ""},
		{"turn off declined", false, []string{"1", "n", "0"}, nil, "Turn backups off?", true, backup.DefaultPath, ""},
		{"verify", false, []string{"3", "0"}, nil, "Nothing was written.", true, backup.DefaultPath, ""},
		{"quit", false, []string{"q"}, nav.ErrQuit, "Backup Settings", true, backup.DefaultPath, ""},
		{"the end of the input", false, nil, prompt.ErrAborted, "Backup Settings", true, backup.DefaultPath, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, cfg := newDNSHost(t, "debian13", dnsTestList)
			h.AddDir("/srv", 0o755, 0, 0)
			s := useDNSHost(t, h, tc.lines...)
			cfg.DryRun = tc.dryRun // as -n or hardn.yml leaves it before 5 is chosen
			err := runBackupScreen(context.Background(), cfg)
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("runBackupScreen = %v, want %v; screen:\n%s", err, tc.want, s.out.String())
			}
			if out := s.out.String(); !strings.Contains(out, tc.said) {
				t.Errorf("the screen does not say %q:\n%s", tc.said, out)
			}
			if cfg.EnableBackups != tc.enabled || cfg.BackupPath != tc.path {
				t.Errorf("the main menu's configuration: enableBackups %t, backupPath %q; want %t, %q", cfg.EnableBackups, cfg.BackupPath, tc.enabled, tc.path)
			}
			written := tc.backups != ""
			if want := map[bool]int{true: 1}[written]; s.applies != want || s.live != 0 || s.atPrompt != 0 {
				t.Errorf("signals registered %d times (want %d), %d left, %d prompts read under them", s.applies, want, s.live, s.atPrompt)
			}
			got := h.writes()
			if !written {
				if len(got) > 0 {
					t.Errorf("wrote %q", got)
				}
				return
			}
			if !slices.Contains(got, "write-atomic "+dnsTestYML) || len(under(got, tc.backups)) == 0 {
				t.Errorf("writes %q: want hardn.yml saved and its backup under %s", got, tc.backups)
			}
			if old := backup.DefaultPath; tc.backups != old && len(under(got, old)) > 0 {
				t.Errorf("wrote under the old path %s: %q", old, under(got, old))
			}
			if yml := h.content(dnsTestYML); !strings.HasPrefix(yml, "# hardn's configuration") || !strings.Contains(yml, "nameservers:\n  - 192.0.2.53\n") {
				t.Errorf("hardn.yml's other lines not kept:\n%s", yml)
			}
			if !cfg.LoadedFrom([]byte(h.content(dnsTestYML))) {
				t.Error("the configuration does not record hardn.yml as saved, so the next save would be refused CONFIG_CHANGED")
			}
			for _, c := range got {
				if strings.HasPrefix(c, "append ") {
					t.Errorf("an audit record was written: %s", c)
				}
			}
		})
	}
}

// hookSet records the DNS and Backup items' hooks as withDNSScreen and
// withBackupScreen set them.
type hookSet struct{ dns, backup func() error }

func (h *hookSet) SetDNS(run func() error)    { h.dns = run }
func (h *hookSet) SetBackup(run func() error) { h.backup = run }

// TestBackupThenDNS: a path change through the Backup item leaves the main
// menu's configuration holding the new path, and the DNS item's next
// change backs up under it, nothing under the old: each visit assembles
// engineOptions from the configuration as it then is (runDNSScreen,
// runBackupScreen), and each hook runs on the configuration main gives
// it, not a copy. DNS is visited, with a change, before the path change,
// so that options assembled once, when the hook is set, at the first
// visit or at the first change, back the last change up under the old
// path; and a hook that copied the configuration would have the Backup
// item's save refused CONFIG_CHANGED. Both hooks as main sets them, on
// one host, the user typing on through the three visits, as from the
// main menu.
func TestBackupThenDNS(t *testing.T) {
	h, cfg := newDNSHost(t, "debian13", dnsTestList)
	h.AddDir("/srv", 0o755, 0, 0)
	s := useDNSHost(t, h, "3", "2", "y", "0", "2", backupTestPath, "y", "0", "2", "192.0.2.55", "y", "0")
	set := &hookSet{}
	if got := withBackupScreen(withDNSScreen(set, cfg), cfg); got != set || set.dns == nil || set.backup == nil {
		t.Fatal("withDNSScreen and withBackupScreen did not set the DNS and Backup items' screens on the menu given")
	}
	if err := set.dns(); err != nil {
		t.Fatalf("the DNS item, first = %v; screen:\n%s", err, s.out.String())
	}
	if got := under(h.writes(), backup.DefaultPath); len(got) == 0 {
		t.Fatalf("the first DNS change wrote %q: want its backup under %s", h.writes(), backup.DefaultPath)
	}
	if err := set.backup(); err != nil {
		t.Fatalf("the Backup item = %v; screen:\n%s", err, s.out.String())
	}
	if cfg.BackupPath != backupTestPath {
		t.Fatalf("the main menu's configuration says backupPath %q after the path change, want %q; screen:\n%s", cfg.BackupPath, backupTestPath, s.out.String())
	}
	if o := engineOptions(context.Background(), h, cfg); o.Backups != (backup.Settings{Path: backupTestPath, Enabled: true}) {
		t.Errorf("engineOptions' Backups = %+v after the path change", o.Backups)
	}
	before := len(h.writes())
	if err := set.dns(); err != nil {
		t.Fatalf("the DNS item, again = %v; screen:\n%s", err, s.out.String())
	}
	dnsWrites := h.writes()[before:]
	if !slices.Contains(dnsWrites, "write-atomic "+hostdns.ResolvConf) || len(under(dnsWrites, backupTestPath)) == 0 {
		t.Errorf("the DNS change wrote %q: want /etc/resolv.conf and its backup under %s; screen:\n%s", dnsWrites, backupTestPath, s.out.String())
	}
	if got := under(dnsWrites, backup.DefaultPath); len(got) > 0 {
		t.Errorf("the DNS change wrote under the old path: %q", got)
	}
	if want := []string{dnsTestList[0], "192.0.2.55"}; !slices.Equal(cfg.Nameservers, want) || cfg.BackupPath != backupTestPath {
		t.Errorf("the configuration lists %q, backupPath %q; want %q, %q", cfg.Nameservers, cfg.BackupPath, want, backupTestPath)
	}
	if s.applies != 3 || s.live != 0 || s.atPrompt != 0 {
		t.Errorf("signals registered %d times (want 3), %d left, %d prompts read under them", s.applies, s.live, s.atPrompt)
	}
}

// signalHost is a DNS host whose writes of hardn.yml turning backups off
// call at first, and are made even if a signal at sent has cancelled ctx
// since, as a write the kernel has begun completes; at's error is then
// the write's, the file written, as a failed sync's would be.
type signalHost struct {
	*dnsHost
	at func() error
}

func (h signalHost) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	if name != dnsTestYML || !strings.Contains(string(data), "enableBackups: false") {
		return h.dnsHost.WriteFileAtomic(ctx, name, data, perm, uid, gid)
	}
	err := h.at()
	if werr := h.dnsHost.WriteFileAtomic(context.WithoutCancel(ctx), name, data, perm, uid, gid); werr != nil {
		return werr
	}
	return err
}

// TestBackupScreenSignals: Ctrl-C, SIGTERM and a hangup, each sent to this
// test process as a change's save writes hardn.yml on the Backup screen,
// as the kernel delivers it, cancel that apply: the change is rolled back
// here, hardn.yml as it was, the report naming hardn's log, and the screen
// goes on; the user then goes back (nil), the configuration as it was.
// The change before it in the same visit, its save failing, was rolled
// back under the engine's own hold (hold.Signals), whose release ends a
// registration made before it: each apply registers its own
// (engineSignals), or the signal would end hardn half applied, as Go's
// default would end this test process.
func TestBackupScreenSignals(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			signals := engineSignals // the real one: useDNSHost replaces it
			h, cfg := newDNSHost(t, "debian13", dnsTestList)
			yml := h.content(dnsTestYML)
			s := useDNSHost(t, h, "1", "y", "1", "y", "0")
			engineHold = nil // the engine's own
			var applying context.Context
			engineSignals = func(ctx context.Context) (context.Context, context.CancelFunc) {
				s.applies++
				c, stop := signals(ctx)
				applying = c
				return c, stop
			}
			saves := 0
			engineHost = func() host.System {
				return signalHost{h, func() error {
					if saves++; saves == 1 {
						return &fs.PathError{Op: "sync", Path: dnsTestYML, Err: syscall.EIO} // the first change fails and is rolled back
					}
					if err := syscall.Kill(os.Getpid(), sig); err != nil {
						t.Error(err)
					}
					select {
					case <-applying.Done():
					case <-time.After(5 * time.Second):
						t.Errorf("%v did not cancel the apply", sig)
					}
					return nil
				}}
			}
			if err := runBackupScreen(context.Background(), cfg); err != nil {
				t.Fatalf("runBackupScreen = %v; screen:\n%s", err, s.out.String())
			}
			out := s.out.String()
			if s.applies != 2 || saves != 2 || strings.Count(out, "# Operation Failed") != 2 || !strings.Contains(out, "(CANCELLED)\n\n  [ ] Save the configuration  Rolled back") {
				t.Errorf("%d applies, %d saves; screen:\n%s", s.applies, saves, out)
			}
			if !strings.Contains(out, "Check the log file at "+output.LegacyLogPath) {
				t.Errorf("the failure's report does not name %s:\n%s", output.LegacyLogPath, out)
			}
			if last := out[strings.LastIndex(out, "# Backup Settings #"):]; !strings.Contains(last, "Backup State         Enabled") || !strings.HasSuffix(strings.TrimRight(last, " \n"), "Enter selection [0-4 or q]: 0") {
				t.Errorf("the screen did not go on after the signal, backups on:\n%s", last)
			}
			if got := h.content(dnsTestYML); got != yml || !cfg.EnableBackups {
				t.Errorf("hardn.yml after the signal (enableBackups %t):\n%s", cfg.EnableBackups, got)
			}
		})
	}
}

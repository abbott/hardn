package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/backup"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
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

// hookSet records the Backup item's hook as withBackupScreen sets it.
type hookSet struct{ backup func() error }

func (h *hookSet) SetBackup(run func() error) { h.backup = run }

// TestBackupThenDNS: a path change through the Backup item, as
// withBackupScreen sets it, leaves the main menu's configuration holding
// the new path, and the DNS screen's next change backs up under it: each
// visit assembles engineOptions from the configuration as it then is
// (runDNSScreen, runBackupScreen), so nothing of that change goes under
// the old path. Both screens on one host, the user typing on through
// both, as from the main menu.
func TestBackupThenDNS(t *testing.T) {
	h, cfg := newDNSHost(t, "debian13", dnsTestList)
	h.AddDir("/srv", 0o755, 0, 0)
	s := useDNSHost(t, h, "2", backupTestPath, "y", "0", "2", "192.0.2.55", "y", "0")
	set := &hookSet{}
	if got := withBackupScreen(set, cfg); got != set || set.backup == nil {
		t.Fatal("withBackupScreen did not set the Backup item's screen on the menu given")
	}
	if err := set.backup(); err != nil {
		t.Fatalf("the Backup item = %v; screen:\n%s", err, s.out.String())
	}
	if cfg.BackupPath != backupTestPath {
		t.Fatalf("the main menu's configuration says backupPath %q after the path change, want %q", cfg.BackupPath, backupTestPath)
	}
	if o := engineOptions(context.Background(), h, cfg); o.Backups != (backup.Settings{Path: backupTestPath, Enabled: true}) {
		t.Errorf("engineOptions' Backups = %+v after the path change", o.Backups)
	}
	before := len(h.writes())
	if err := runDNSScreen(context.Background(), cfg); err != nil {
		t.Fatalf("the DNS item = %v; screen:\n%s", err, s.out.String())
	}
	dnsWrites := h.writes()[before:]
	if !slices.Contains(dnsWrites, "write-atomic "+hostdns.ResolvConf) || len(under(dnsWrites, backupTestPath)) == 0 {
		t.Errorf("the DNS change wrote %q: want /etc/resolv.conf and its backup under %s", dnsWrites, backupTestPath)
	}
	if got := under(h.writes(), backup.DefaultPath); len(got) > 0 {
		t.Errorf("wrote under the old path: %q", got)
	}
	if !slices.Equal(cfg.Nameservers, append(slices.Clone(dnsTestList), "192.0.2.55")) || cfg.BackupPath != backupTestPath {
		t.Errorf("the configuration lists %q, backupPath %q", cfg.Nameservers, cfg.BackupPath)
	}
	if s.applies != 2 || s.live != 0 || s.atPrompt != 0 {
		t.Errorf("signals registered %d times (want 2), %d left, %d prompts read under them", s.applies, s.live, s.atPrompt)
	}
}

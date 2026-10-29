package parity

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// The harness's own tests, apart from any slice's table: the trace parser,
// the new path's end state, the table's checks. Each slice's TestParity
// runs the whole of it (pkg/screens/dns's: docs/engine/parity-dns.md).
// Synthetic values only: 192.0.2.0/24, example.net.

// legacyGolden is a characterization golden in pkg/characterization's
// layout: two V-numbers, a probe that fails, a write with its lines and a
// directory made in each cfg.DryRun section, and the -n refusal.
const legacyGolden = `# origin/main behaviour
# FIXED (P0.8, gated by dryRunFlagApplies): V1 — hardn -n refuses every operation flag
# KNOWN-DEFECT: V18 — writes are non-atomic and nothing is backed up first
# KNOWN-DEFECT: V18 — said twice, kept once

== fixture
host: os=debian version=13

== cfg.DryRun=false
result Manager.Configure: ok
trace:
  1 exec   which resolvconf -> error: fixture: resolvconf not installed
  2 mkdir  /etc/example mode=0755 -> ok
  3 write  /etc/example/example.conf mode=0644 -> ok
        | first line
        |
        | nameserver 192.0.2.53
  4 exec   systemctl restart example -> ok

== cfg.DryRun=true
result Manager.Configure: ok
trace:
  1 exec   which resolvconf -> error: fixture: resolvconf not installed

== hardn -n -x (modelled)
refused: exit status 2
stderr: hardn: -n/--dry-run only previews changes. Nothing was changed.
`

// TestReadLegacy: the parser reads each section's end state, the
// V-numbers once each, and the -n refusal; the probes are recorded apart
// from the commands that change something.
func TestReadLegacy(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ops", "example"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ops", "example", "debian.golden"), []byte(legacyGolden), 0o644); err != nil {
		t.Fatal(err)
	}
	r := readLegacy(t, dir, []string{"which resolvconf"}, "ops/example/debian.golden")
	if r.Golden != "ops/example/debian.golden" || !slices.Equal(r.Vs, []string{"V1", "V18"}) {
		t.Errorf("golden %q, V-numbers %q", r.Golden, r.Vs)
	}
	if want := "exit status 2: hardn: -n/--dry-run only previews changes. Nothing was changed."; r.CLI != want {
		t.Errorf("the -n refusal %q, want %q", r.CLI, want)
	}
	l := r.Live
	if len(l.Files) != 1 || l.Files[0].String() != "/etc/example/example.conf 0644" || l.Files[0].Atomic ||
		!slices.Equal(l.Files[0].Lines, []string{"first line", "", "nameserver 192.0.2.53"}) {
		t.Errorf("live files %+v", l.Files)
	}
	if !slices.Equal(l.Dirs, []string{"/etc/example 0755"}) || !slices.Equal(l.Actions, []string{"systemctl restart example"}) ||
		!slices.Equal(l.Probes, []string{"which resolvconf -> error: fixture: resolvconf not installed"}) || l.Refusal != "" || l.YML != nil {
		t.Errorf("live dirs %q, actions %q, probes %q, refusal %q, hardn.yml %v", l.Dirs, l.Actions, l.Probes, l.Refusal, l.YML)
	}
	if d := r.Dry; d.Wrote() || len(d.Actions) != 0 || len(d.Probes) != 1 {
		t.Errorf("dry run %+v", d)
	}
}

// TestParseLegacyRefuses: what the parser does not compare is an error,
// not dropped: a golden whose run is in no section it reads (the Backup
// screen's layout, its trace under "The repository's file system calls"),
// and a removal.
func TestParseLegacyRefuses(t *testing.T) {
	for _, tc := range []struct{ name, golden, want string }{
		{"another layout", `# origin/main behaviour
# ----
# The repository's file system calls:
  1 mkdir  $TMP/var/backups/example mode=0755 -> ok
  2 write  $TMP/var/backups/example/.write_test mode=0644 -> ok
        | test
  3 remove $TMP/var/backups/example/.write_test -> ok
# ./hardn.yml as written:
enableBackups: true
`, `menu/example.golden: no "== cfg.DryRun=false" or "== result" section, the run the harness compares: a layout it does not read`},
		{"a removal", `== cfg.DryRun=false
result Manager.Configure: ok
trace:
  1 remove /etc/example/example.conf -> ok
`, `menu/example.golden: an operation the harness does not compare (remove): "  1 remove /etc/example/example.conf -> ok"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseLegacy([]byte(tc.golden), nil, "menu/example.golden"); err == nil || err.Error() != tc.want {
				t.Errorf("parseLegacy: %v, want %s", err, tc.want)
			}
		})
	}
}

// TestNewState: the new path's end state from the fake's calls after the
// nth: the host's files as they are now, in the order first written, the
// directories, the commands apart from those that only read, hardn.yml
// apart, the journal and the backups from the run's manifest; the
// engine's run directory left out.
func TestNewState(t *testing.T) {
	ctx := context.Background()
	h := hostfake.New(host.Options{})
	for _, d := range []string{"/etc", "/etc/hardn", "/run", "/var/lib", "/var/lib/hardn", "/var/backups"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddFile("/etc/hardn/hardn.yml", []byte("username: alice\n"), 0o600, 0, 0)
	h.Handler = func([]string, []byte) hostfake.Response { return hostfake.Response{} }
	n := len(h.Calls())
	const run = backup.DefaultPath + "/20261027T120000Z-0a1b2c3d"
	manifest := `{"entries": [{"path": "/etc/example.conf", "kind": "file"}, {"path": "/etc/new.conf", "kind": "absent"}]}`
	for _, step := range []func() error{
		func() error { return h.MkdirAll(ctx, "/run/hardn/x", 0o700) },
		func() error { return h.WriteFileAtomic(ctx, "/var/lib/hardn/journal.json", []byte("{}"), 0o600, 0, 0) },
		func() error { return h.MkdirAll(ctx, run, 0o700) },
		func() error { return h.WriteFileAtomic(ctx, run+"/manifest.json", []byte(manifest), 0o600, 0, 0) },
		func() error { return h.WriteFileAtomic(ctx, "/etc/example.conf", []byte("a\nb\n"), 0o644, 0, 0) },
		func() error { return h.MkdirAll(ctx, "/etc/example.d", 0o755) },
		func() error { return h.WriteFileInPlace(ctx, "/etc/hardn/hardn.yml", []byte("username: bob\n"), 0o600) },
		func() error { _, err := h.Run(ctx, "systemctl", "is-active", "example"); return err },
		func() error { _, err := h.Run(ctx, "systemctl", "reload", "example"); return err },
		func() error { return h.WriteFileAtomic(ctx, "/etc/example.conf", []byte("a\nc\n"), 0o644, 0, 0) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	s := NewState(t, h, n, []string{"systemctl is-active example"}, "/etc/hardn/hardn.yml")
	if len(s.Files) != 1 || s.Files[0].String() != "/etc/example.conf 0644" || !s.Files[0].Atomic || !slices.Equal(s.Files[0].Lines, []string{"a", "c"}) {
		t.Errorf("files %+v", s.Files)
	}
	if s.YML == nil || s.YML.Atomic || !slices.Equal(s.YML.Lines, []string{"username: bob"}) {
		t.Errorf("hardn.yml %+v", s.YML)
	}
	if !slices.Equal(s.Dirs, []string{"/etc/example.d 0755"}) || !slices.Equal(s.Actions, []string{"systemctl reload example"}) ||
		!slices.Equal(s.Probes, []string{"systemctl is-active example"}) {
		t.Errorf("dirs %q, actions %q, probes %q", s.Dirs, s.Actions, s.Probes)
	}
	if want := []string{"/etc/example.conf", "/etc/new.conf (absent before: the rollback removes it)"}; !s.Journal || !slices.Equal(s.Backups, want) {
		t.Errorf("journal %t, backups %q; want journalled, %q", s.Journal, s.Backups, want)
	}
	if got := HostText(s); !strings.Contains(got, "/etc/example.conf 0644\na\nc\n") || !strings.Contains(got, "/etc/hardn/hardn.yml 0600\nusername: bob\n") {
		t.Errorf("HostText:\n%s", got)
	}
}

// TestTableProblems: the table's checks, one cell at a time: a difference
// listed passes; one unlisted is UNEXPLAINED; an entry whose difference
// does not occur is stale; a V-number the cell's golden does not name, and
// an entry listed twice, fail.
func TestTableProblems(t *testing.T) {
	cell := Cell{Scenario: Scenario{Name: "direct"}, Distro: Distro{Name: "debian"},
		legacy: legacyRun{Golden: "ops/example/debian.golden", Vs: []string{"V18"}},
		diffs:  []aspect{{Name: "write", Legacy: "in place", New: "atomic"}}}
	entry := func(aspect, source string) Tie {
		return Tie{Scenarios: []string{"direct"}, Distros: []string{"debian"}, Aspect: aspect, Source: source, Why: "why"}
	}
	for _, tc := range []struct {
		name  string
		table []Tie
		want  []string
	}{
		{"listed", []Tie{entry("write", "V18")}, nil},
		{"unlisted", nil, []string{`UNEXPLAINED direct on debian, write: origin/main "in place", new "atomic"`}},
		{"stale", []Tie{entry("write", "V18"), entry("mode", "V18")}, []string{"the table lists direct on debian, mode, which no longer differs"}},
		{"a V-number not named", []Tie{entry("write", "V12; decision 1")},
			[]string{"direct on debian, write: tied to V12, which ops/example/debian.golden does not name (it names [V18])"}},
		{"twice", []Tie{entry("write", "V18"), entry("write", "V18")}, []string{"the table lists direct on debian, write twice"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tableProblems(tc.table, []Cell{cell}); !slices.Equal(got, tc.want) {
				t.Errorf("tableProblems = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestWhere: a group of differences as the page's first two columns.
func TestWhere(t *testing.T) {
	for _, tc := range []struct {
		cells         [][2]string
		scenarios, on string
	}{
		{[][2]string{{"a", "Debian 13"}, {"a", "Alpine 3.23"}}, "a", "all"},
		{[][2]string{{"a", "Debian 13"}, {"b", "Debian 13"}}, "a, b", "Debian 13"},
		{[][2]string{{"a", "Debian 13"}, {"b", "Alpine 3.23"}}, "a (Debian 13); b (Alpine 3.23)", "as listed"},
	} {
		if s, on := where(tc.cells, 2); s != tc.scenarios || on != tc.on {
			t.Errorf("where(%q) = %q, %q; want %q, %q", tc.cells, s, on, tc.scenarios, tc.on)
		}
	}
}

// TestPageCites: the page's v0.3.2 line names the sections the slice
// gives, or none.
func TestPageCites(t *testing.T) {
	for sections, want := range map[string]string{
		"§3": "from [v032-reference.md](../refactor/phase0/v032-reference.md) §3.\n",
		"":   "from [v032-reference.md](../refactor/phase0/v032-reference.md).\n",
	} {
		if got := renderPage(Slice{Pkg: "pkg/screens/example", Golden: "parity.golden", V032Sections: sections}, nil); !strings.Contains(got, want) {
			t.Errorf("V032Sections %q: the page has no %q:\n%s", sections, want, got)
		}
	}
}

package dns_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
)

// The end state of one run, origin/main's or the new path's, in the shape
// the parity table compares (parity_test.go).

// endState is what a run left and said.
type endState struct {
	refusal  string      // what stopped it before any change; "" for nothing
	files    []fileWrite // the host's files written, in order
	dirs     []string    // directories made ("path mode"), modes set ("chmod path mode")
	actions  []string    // commands that change something
	probes   []string    // commands that only read: recorded, not compared
	backups  []string    // files backed up before the change
	journal  bool        // the change journalled before it ran
	findings []string    // the plan's findings, by code
	yml      *fileWrite  // hardn.yml as saved; nil when not saved
	list     []string    // the list saved in hardn.yml
	loaded   []string    // hardn.yml as loaded, the fixture's lines
}

// fileWrite is one file as a run left it.
type fileWrite struct {
	path   string
	mode   fs.FileMode
	lines  []string
	atomic bool // replaced whole by a rename; false: written in place
}

func (f fileWrite) String() string { return fmt.Sprintf("%s %04o", f.path, uint32(f.mode.Perm())) }

// wrote reports whether the run wrote a file, the host's or hardn.yml.
func (s endState) wrote() bool { return len(s.files) > 0 || s.yml != nil }

// legacyRun is origin/main's run, read from its characterization golden.
type legacyRun struct {
	golden string   // relative to pkg/characterization/testdata/golden
	vs     []string // the V-numbers its KNOWN-DEFECT and FIXED lines name
	live   endState // cfg.DryRun=false, or the menu item's result
	dry    endState // cfg.DryRun=true (ops goldens only)
	cli    string   // hardn -n with the flag: what pkg/cmd's refusal says
}

// legacyGoldens is where pkg/characterization keeps them: read in place,
// never regenerated here.
var legacyGoldens = filepath.Join("..", "..", "characterization", "testdata", "golden")

// legacyProbes are the commands origin/main's DNS repository runs only to
// choose a strategy (pkg/adapter/secondary/file_dns_repository.go:38, 44).
var legacyProbes = []string{"systemctl is-active systemd-resolved", "which resolvconf"}

var (
	traceLine   = regexp.MustCompile(`^\s*\d+ (exec|write|mkdir|read|stat|remove|rmall)\s+(.*?)(?: -> (ok|error: .*))?$`)
	writeTarget = regexp.MustCompile(`^(\S+) mode=(\d{4})$`)
	defectLine  = regexp.MustCompile(`^# (?:KNOWN-DEFECT|FIXED [^:]*): (V\d+) `)
)

// readLegacy parses the characterization golden rel: its V-numbers, each
// trace section into an end state, and its -n refusal. An ops golden has
// both cfg.DryRun sections; a menu_dns golden has its result.
func readLegacy(t *testing.T, rel string) legacyRun {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(legacyGoldens, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("origin/main's golden: %v", err)
	}
	r := legacyRun{golden: rel}
	var cur *endState
	var last *fileWrite
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		l := sc.Text()
		if m := defectLine.FindStringSubmatch(l); m != nil && !slices.Contains(r.vs, m[1]) {
			r.vs = append(r.vs, m[1])
		}
		switch {
		case l == "== cfg.DryRun=false", l == "== result":
			cur, last = &r.live, nil
			continue
		case l == "== cfg.DryRun=true":
			cur, last = &r.dry, nil
			continue
		case strings.HasPrefix(l, "== "):
			cur, last = nil, nil
			continue
		case strings.HasPrefix(l, "refused: "):
			r.cli = strings.TrimPrefix(l, "refused: ")
		case strings.HasPrefix(l, "stderr: ") && r.cli != "":
			r.cli += ": " + strings.TrimPrefix(l, "stderr: ")
		}
		if cur == nil {
			continue
		}
		if c, ok := strings.CutPrefix(l, "        "); ok && last != nil {
			switch {
			case c == "|":
				last.lines = append(last.lines, "")
			case strings.HasPrefix(c, "| "):
				last.lines = append(last.lines, c[2:])
			}
			continue
		}
		if v, ok := strings.CutPrefix(l, "message: "); ok {
			cur.refusal = v // kept only when nothing was saved, below
		}
		if v, ok := strings.CutPrefix(l, "list: "); ok && v != "none" {
			cur.list = strings.Split(v, ", ")
		}
		m := traceLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		switch m[1] {
		case "exec":
			if slices.Contains(legacyProbes, m[2]) {
				cur.probes = append(cur.probes, m[2]+" -> "+m[3])
			} else {
				cur.actions = append(cur.actions, m[2])
			}
		case "mkdir":
			p, mode, _ := strings.Cut(m[2], " mode=")
			cur.dirs = append(cur.dirs, p+" "+mode)
		case "write":
			w := writeTarget.FindStringSubmatch(m[2])
			if w == nil {
				t.Fatalf("%s: a write the parser does not know: %q", rel, l)
			}
			var mode uint32
			if _, err := fmt.Sscanf(w[2], "%o", &mode); err != nil {
				t.Fatal(err)
			}
			// In place: interfaces.OSFileSystem.WriteFile and SaveConfig are
			// os.WriteFile (side-effects.md §1 item 10), whichever the trace's
			// seam recorded.
			f := fileWrite{path: w[1], mode: fs.FileMode(mode)}
			if f.path == "./hardn.yml" {
				cur.yml = &f
				last = cur.yml
			} else {
				cur.files = append(cur.files, f)
				last = &cur.files[len(cur.files)-1]
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if r.live.yml != nil || !strings.HasPrefix(rel, "menu_dns/") {
		r.live.refusal = "" // a menu item's message is its refusal only when it saved nothing
	}
	return r
}

// The new path's own directories: its run directory, the journal and the
// backups (engine.md), which are not the host's end state; the journal and
// the backups are recorded as such.
const (
	runDir     = "/run/hardn"
	journalDir = "/var/lib/hardn"
)

func under(p, dir string) bool { return p == dir || strings.HasPrefix(p, dir+"/") }

// readOnly are the commands the new path runs only to read (dns.md §2.2,
// and the check after a reload, §2.12).
var readOnly = []string{"systemctl is-active " + hostdns.ResolvedUnit, "resolvectl --no-pager dns", "resolvectl --no-pager domain"}

// newState is the end state h's calls from the nth on left: the host's
// files and hardn.yml as they are now, the directories, the commands, the
// backups (the last manifest's entries) and whether the journal was
// written; refusal and findings are the caller's.
func newState(t *testing.T, h *fakeHost, n int) endState {
	t.Helper()
	var s endState
	var order []string
	modes := map[string]hostfake.Call{}
	var manifest []byte
	for _, c := range h.Calls()[n:] {
		switch {
		case under(c.Path, runDir):
			continue
		case under(c.Path, backup.DefaultPath):
			if c.Op == "write-atomic" && path.Base(c.Path) == "manifest.json" {
				manifest = c.Data
			}
			continue
		case under(c.Path, journalDir):
			s.journal = s.journal || c.Op == "write-atomic"
			continue
		}
		switch c.Op {
		case "write-atomic", "write-in-place":
			if _, ok := modes[c.Path]; !ok {
				order = append(order, c.Path)
			}
			modes[c.Path] = c
		case "mkdir":
			s.dirs = append(s.dirs, fmt.Sprintf("%s %04o", c.Path, uint32(c.Mode.Perm())))
		case "chmod":
			s.dirs = append(s.dirs, fmt.Sprintf("chmod %s %04o", c.Path, uint32(c.Mode.Perm())))
		case "run":
			argv := host.FormatArgv(c.Argv)
			switch {
			case !slices.Contains(readOnly, argv):
				s.actions = append(s.actions, argv)
			case !slices.Contains(s.probes, argv):
				s.probes = append(s.probes, argv)
			}
		case "remove", "removeall", "rename", "chown", "append":
			t.Errorf("a change the table does not compare: %s", c)
		}
	}
	for _, p := range order {
		c := modes[p]
		e, ok := h.Entry(p)
		if !ok {
			t.Fatalf("%s written, then gone", p)
		}
		f := fileWrite{path: p, mode: c.Mode.Perm(), lines: strings.Split(strings.TrimSuffix(string(e.Data), "\n"), "\n"), atomic: c.Op == "write-atomic"}
		if p == hardnYML {
			s.yml = &f
			continue
		}
		s.files = append(s.files, f)
	}
	if s.yml != nil {
		c, err := config.Read(context.Background(), h, hardnYML)
		if err != nil {
			t.Fatal(err)
		}
		s.list = c.Nameservers
	}
	if manifest != nil {
		var m struct{ Entries []struct{ Path, Kind string } }
		if err := json.Unmarshal(manifest, &m); err != nil {
			t.Fatal(err)
		}
		for _, e := range m.Entries {
			switch e.Kind {
			case string(backup.KindFile):
				s.backups = append(s.backups, e.Path)
			case string(backup.KindAbsent):
				s.backups = append(s.backups, e.Path+" (absent before: the rollback removes it)")
			default:
				s.backups = append(s.backups, e.Path+" ("+e.Kind+")")
			}
		}
	}
	return s
}

// planned sets s's refusal and findings from p, a plan of the DNS
// control: the refusal's code, and the codes of its changes' findings.
func planned(s endState, p *engine.Plan) endState {
	if err := p.Err(); err != nil {
		s.refusal = output.CodeOf(err)
	}
	for _, c := range p.Changes {
		for _, f := range c.Findings {
			if !slices.Contains(s.findings, f.Code) {
				s.findings = append(s.findings, f.Code)
			}
		}
	}
	return s
}

// Host states the legacy strategies stand for (docs/engine/parity-dns.md §3).
const (
	stateFile       = "a regular /etc/resolv.conf, systemd-resolved not running"
	stateResolved   = "/etc/resolv.conf linked to resolved's stub, systemd-resolved running"
	stateResolvconf = "/etc/resolv.conf linked to resolvconf's file"
)

// parityDistro is one column of origin/main's matrix
// (pkg/characterization's osCells), with the facts the new path's host
// takes, and what its stock install has (docs/safety/dns.md §3).
type parityDistro struct {
	name, label string
	facts       detect.Facts
	stock       string // the host state of a stock install
	file        string // that install's /etc/resolv.conf, when a file
}

var parityDistros = []parityDistro{
	{"debian", "Debian 13", facts(detect.DistroDebian, "13", detect.Systemd), stateFile, dhcpcdHead + "nameserver " + dhcpNS + "\n"},
	{"ubuntu", "Ubuntu 24.04", facts(detect.DistroUbuntu, "24.04", detect.Systemd), stateResolved, ""},
	{"alpine", "Alpine 3.23", facts(detect.DistroAlpine, "3.23", detect.OpenRC), stateFile, "search example.net\nnameserver " + dhcpNS + "\n"},
	{"proxmox", "Proxmox VE 9", facts(detect.DistroProxmox, "9", detect.Systemd), stateFile, "search example.net\nnameserver " + dhcpNS + "\n"},
}

// fixtures are the hardn.yml files both paths load, pkg/characterization's
// (the characterization fixture's settings).
var fixtures = filepath.Join("..", "..", "characterization", "testdata", "fixtures", "config")

// parityHost is d in host state state, with the fixture hardn.yml at
// /etc/hardn/hardn.yml (0644, as config.CreateDefaultConfig writes it), as
// config.Read loads it. A file on a distribution whose stock install has
// none is Ubuntu's forced one: the search line and the DHCP server of the
// others, no DHCP client.
func parityHost(t *testing.T, d parityDistro, state, fixture string) (*fakeHost, *config.Config) {
	t.Helper()
	h := &fakeHost{System: hostfake.New(host.Options{}), facts: d.facts, active: "inactive", fail: map[string]error{}, lstat: map[string]error{},
		links: "Link 2 (eth0): " + dhcpNS + "\n", domains: "Link 2 (eth0): example.net\n"}
	for _, dir := range []string{"/etc", "/etc/hardn", "/var/lib", "/var/backups", "/run", "/var/log"} {
		h.AddDir(dir, 0o755, 0, 0)
	}
	switch state {
	case stateFile:
		file := d.file
		if file == "" {
			file = "search example.net\nnameserver " + dhcpNS + "\n"
		}
		h.AddFile(hostdns.ResolvConf, []byte(file), 0o644, 0, 0)
		switch d.name {
		case "debian":
			h.AddFile("/usr/sbin/dhcpcd", nil, 0o755, 0, 0)
		case "alpine":
			h.AddFile(hostdns.UDHCPCConf, []byte("# Do not overwrite /etc/resolv.conf\n#RESOLV_CONF=\"no\"\n\n#NO_DNS=\"eth1 wlan1\"\n"), 0o644, 0, 0)
		}
	case stateResolved:
		h.active = "active"
		h.AddSymlink(hostdns.ResolvConf, "../run/systemd/resolve/stub-resolv.conf")
		h.AddFile("/etc/systemd/resolved.conf", []byte("[Resolve]\n#DNS=\n#Domains=\n"), 0o644, 0, 0)
	case stateResolvconf:
		h.AddSymlink(hostdns.ResolvConf, "../run/resolvconf/resolv.conf")
	default:
		t.Fatalf("no host state %q", state)
	}
	data, err := os.ReadFile(filepath.Join(fixtures, fixture))
	if err != nil {
		t.Fatal(err)
	}
	h.AddFile(hardnYML, data, 0o644, 0, 0)
	h.Handler = h.answer
	cfg, err := config.Read(context.Background(), h, hardnYML)
	if err != nil {
		t.Fatal(err)
	}
	return h, cfg
}

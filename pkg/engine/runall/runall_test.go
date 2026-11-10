package runall_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/apparmor"
	backupctl "github.com/abbott/hardn/pkg/engine/controls/backup"
	dnsctl "github.com/abbott/hardn/pkg/engine/controls/dns"
	envctl "github.com/abbott/hardn/pkg/engine/controls/environment"
	fwctl "github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/runall"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/screens/screenkit"
)

var (
	login = ssh.Login{}.ID()
	port  = ssh.Port{}.ID()
)

// want is what a host's rows must be: the rows of each outcome but the
// plain ones (as asked, nothing asked, left), and whether the plan is
// lockout-risk.
type want struct {
	planned, refused, na, elsewhere, later, pending []string
	lockout                                         bool
}

// TestHosts holds each host's rows, as the gallery's TestRunAllHosts, with
// decision 5 (a) applied (ufw-inactive): every outcome kind, each row held
// to its control alone (held).
func TestHosts(t *testing.T) {
	all := map[string]want{
		"debian13":           {planned: []string{login, dnsctl.ID}, lockout: true},
		"ubuntu2604":         {planned: []string{login, dnsctl.ID}, lockout: true},
		"alpine323":          {planned: []string{login, dnsctl.ID}, refused: []string{fwctl.ID}, lockout: true},
		"fresh":              {planned: []string{users.ID, dnsctl.ID}, refused: []string{login, fwctl.ID}},
		"hardened":           {},
		"partial":            {planned: []string{login}, lockout: true},
		"port":               {planned: []string{login, dnsctl.ID}, elsewhere: []string{port}, lockout: true},
		"pve9":               {planned: []string{login, dnsctl.ID}, refused: []string{users.ID, fwctl.ID}, na: []string{updates.ID}, lockout: true},
		"container":          {planned: []string{login}, na: []string{dnsctl.ID}, lockout: true},
		"nothing-asked":      {},
		"refused":            {planned: []string{login, dnsctl.ID}, refused: []string{updates.ID}, lockout: true},
		"pending":            {pending: []string{users.ID, login, fwctl.ID, dnsctl.ID}},
		"ufw-inactive":       {planned: []string{fwctl.ID}, later: []string{login}, lockout: true},
		"new-admin":          {planned: []string{users.ID, login}, lockout: true},
		"george-no-sudo":     {planned: []string{dnsctl.ID}, refused: []string{login}, elsewhere: []string{users.ID}},
		"carol-ufw-inactive": {planned: []string{users.ID, fwctl.ID, dnsctl.ID}, later: []string{login}, lockout: true},
	}
	for _, name := range runHosts {
		w, ok := all[name]
		if !ok {
			t.Errorf("%s: no rows expected", name)
			continue
		}
		h := newHost(t, name)
		r := h.read(t, name)
		ids := func(k runall.Kind) []string {
			var out []string
			for _, rw := range r.Rows {
				if rw.Kind == k {
					out = append(out, rw.ID)
				}
			}
			return out
		}
		for _, c := range []struct {
			kind runall.Kind
			want []string
		}{{runall.Planned, w.planned}, {runall.Refused, w.refused}, {runall.NotApplicable, w.na}, {runall.Elsewhere, w.elsewhere},
			{runall.Later, w.later}, {runall.Pending, w.pending}} {
			if got := ids(c.kind); !slices.Equal(got, c.want) {
				t.Errorf("%s: %v %q, want %q", name, c.kind, got, c.want)
			}
		}
		if r.Lockout() != w.lockout {
			t.Errorf("%s: lockout %v, want %v", name, r.Lockout(), w.lockout)
		}
		held(t, name, h, r)
	}
}

// TestPVERootNeverLoosened: on Proxmox VE, Run All's root login off is
// -d's (sshchange.Disallow): root login already off is not opened to root
// key-only, and root key-only already is not refused; each is as asked,
// nothing lockout-risk planned. Root login off where root key-only could
// not be planned (AllowUsers leaving root out, which Proxmox VE's own
// check of a key-only candidate refuses) is as asked too: a change never
// made is not refused.
func TestPVERootNeverLoosened(t *testing.T) {
	for _, tc := range []struct{ value, dropIn string }{{"no", ""}, {"prohibit-password", ""}, {"no", "AllowUsers george\n"}} {
		h := newHost(t, "pve9")
		h.sshFile(sshd.MainConfig, sshd.IncludeLine+"\n\nUsePAM yes\nPermitRootLogin "+tc.value+"\n", 0o644)
		name := "pve9, PermitRootLogin " + tc.value
		if tc.dropIn != "" {
			h.sshFile(sshd.DropInDir+"/50-site.conf", tc.dropIn, 0o644)
			name += ", " + strings.TrimSpace(tc.dropIn)
		}
		r := h.read(t, name)
		for _, rw := range r.Rows {
			if rw.ID == login && rw.Kind != runall.AsAsked {
				t.Errorf("%s: root login %v, want as asked", name, rw.Kind)
			}
		}
		if r.Lockout() {
			t.Errorf("%s: lockout-risk, want not", name)
		}
		held(t, name, h, r)
	}
}

// held holds r's rows on h to the controls: the rows in IDs' order, each
// with its check; a Planned row's control plans changes alone and is in the
// one plan, its words the mapping's; a Refused row's refuses alone with
// the row's words; an "already as asked" row's plans nothing alone (the
// user's: its name taken); the rest planned by no one.
func held(t *testing.T, name string, h *runHost, r runall.Reading) {
	t.Helper()
	ctx := context.Background()
	if got := rowIDs(r); !slices.Equal(got, runall.IDs()) || len(r.Reports) != len(r.Rows) {
		t.Fatalf("%s: rows %q, %d reports; want %q", name, got, len(r.Reports), runall.IDs())
	}
	for i, rw := range r.Rows {
		if rw.Report.Control != rw.ID || r.Reports[i].Control != rw.ID || rw.Title == "" {
			t.Errorf("%s %s: report %q, title %q", name, rw.ID, rw.Report.Control, rw.Title)
		}
		if (rw.Kind == runall.Planned) != (rw.Control != nil) || (rw.Kind == runall.Refused || rw.Kind == runall.Pending) != (rw.Refusal != "" && rw.Err != nil) {
			t.Errorf("%s %s: %v with control %v, refusal %q, err %v", name, rw.ID, rw.Kind, rw.Control, rw.Refusal, rw.Err)
		}
		if w, ok := map[string]string{apparmor.ID: runall.WordsStatus, backupctl.ID: runall.WordsUnchanged}[rw.ID]; ok && (rw.Kind != runall.Left || rw.Words != w) {
			t.Errorf("%s %s: %v %q, want left, %q", name, rw.ID, rw.Kind, rw.Words, w)
		}
		if (rw.ID == ssh.Auth{}.ID() || rw.ID == envctl.ID) && (rw.Kind != runall.NothingAsked || rw.Words != runall.WordsNothing) {
			t.Errorf("%s %s: %v %q, want nothing asked", name, rw.ID, rw.Kind, rw.Words)
		}
		if rw.Kind == runall.NotApplicable && (rw.Words == "" || rw.Words != rw.Report.Status.Detail) {
			t.Errorf("%s %s: not applicable %q, its check's %q", name, rw.ID, rw.Words, rw.Report.Status.Detail)
		}
		a := runall.Asked(h.input(), rw.ID)
		if a.Control == nil || rw.Kind == runall.NotApplicable || rw.Kind == runall.Pending {
			continue
		}
		p, err := h.newEngine(nil).Plan(ctx, []engine.Control{a.Control}, h.cfg)
		if err == nil && len(p.Refused) > 0 {
			err = p.Refused[0]
		}
		switch rw.Kind {
		case runall.Planned:
			if err != nil || len(p.Changes) == 0 || rw.Words != a.Words || !slices.ContainsFunc(r.Plan.Changes, func(c engine.Change) bool { return c.Control == rw.ID }) {
				t.Errorf("%s %s: planned %q, but alone %v, %d changes, or not in the one plan", name, rw.ID, rw.Words, err, len(p.Changes))
			}
		case runall.Later:
			if err != nil || len(p.Changes) == 0 || rw.Words != runall.WordsAfterFirewall || slices.ContainsFunc(r.Plan.Changes, func(c engine.Change) bool { return c.Control == rw.ID }) {
				t.Errorf("%s %s: later %q, but alone %v, %d changes, or in the one plan", name, rw.ID, rw.Words, err, len(p.Changes))
			}
		case runall.Refused:
			if err == nil || !errors.Is(rw.Err, err) && rw.Err.Error() != err.Error() || rw.Words != runall.WordsRefused {
				t.Errorf("%s %s: refused %q (%v), alone %v", name, rw.ID, rw.Refusal, rw.Err, err)
			}
		case runall.AsAsked:
			if rw.ID == users.ID && !errors.Is(err, users.ErrNameTaken) || rw.ID != users.ID && (err != nil || len(p.Changes) > 0) || rw.Words != runall.WordsAsked {
				t.Errorf("%s %s: already as asked, but alone %v, %d changes", name, rw.ID, err, len(p.Changes))
			}
		}
	}
}

func rowIDs(r runall.Reading) []string {
	var out []string
	for _, rw := range r.Rows {
		out = append(out, rw.ID)
	}
	return out
}

// TestAsked holds the mapping (decision 4 (a)) key by key, table-driven:
// each gate on and off, the change's words, the port compared only,
// updates on alone, and the rows no key asks of.
func TestAsked(t *testing.T) {
	key := synthKey(1, "george@laptop-1")
	full := func() *config.Config {
		return &config.Config{Username: "george", SshKeys: []string{key}, SshPort: 2230, EnableUfwSshPolicy: true, ConfigureDns: true,
			Nameservers: nameservers, DisableRootSSH: true, EnableUnattendedUpgrades: true}
	}
	debian := detect.Facts{Platform: detect.Platform{Distro: detect.DistroDebian}}
	pve := detect.Facts{Platform: detect.Platform{Distro: detect.DistroProxmox}}
	records := revert.Options{Dir: "/var/lib/hardn-test"}
	tests := []struct {
		name  string
		id    string
		facts detect.Facts
		set   func(*config.Config)
		want  engine.Control
		words string
		port  uint16
	}{
		{"user", users.ID, debian, nil, users.Admin{Action: users.ActionCreate, New: users.New{Name: "george", Admin: true, Keys: []string{key}}}, "Create george, 1 key", 0},
		{"user, two keys", users.ID, debian, func(c *config.Config) { c.SshKeys = []string{key, key} }, users.Admin{Action: users.ActionCreate, New: users.New{Name: "george", Admin: true, Keys: []string{key, key}}}, "Create george, 2 keys", 0},
		{"no username", users.ID, debian, func(c *config.Config) { c.Username = "" }, nil, "", 0},
		{"sudoNoPassword false", users.ID, debian, func(c *config.Config) { c.SudoNoPassword = false }, users.Admin{Action: users.ActionCreate, New: users.New{Name: "george", Admin: true, Keys: []string{key}}}, "Create george, 1 key", 0},
		{"root login off", login, debian, nil, sshchange.Disallow{Root: sshchange.Root{Want: "no"}}, "Root login off", 0},
		{"root key-only", login, pve, nil, sshchange.Disallow{Root: sshchange.Root{Want: "no"}}, "Root key-only", 0},
		{"disableRootSSH false", login, debian, func(c *config.Config) { c.DisableRootSSH = false }, nil, "", 0},
		{"port", port, debian, nil, nil, "", 2230},
		{"port 0", port, debian, func(c *config.Config) { c.SshPort = 0 }, nil, "", 0},
		{"port out of range", port, debian, func(c *config.Config) { c.SshPort = 65536 }, nil, "", 0},
		{"port 70000", port, debian, func(c *config.Config) { c.SshPort = 70000 }, nil, "", 0},
		{"port -1", port, debian, func(c *config.Config) { c.SshPort = -1 }, nil, "", 0},
		{"firewall", fwctl.ID, debian, nil, fwctl.Incoming{Action: fwctl.ActionEnable}, "Enable, SSH allowed", 0},
		{"enableUfwSshPolicy false", fwctl.ID, debian, func(c *config.Config) { c.EnableUfwSshPolicy = false }, nil, "", 0},
		{"dns", dnsctl.ID, debian, nil, dnsctl.Nameservers{}, "Set 2 nameservers", 0},
		{"configureDns false", dnsctl.ID, debian, func(c *config.Config) { c.ConfigureDns = false }, nil, "", 0},
		{"no nameservers", dnsctl.ID, debian, func(c *config.Config) { c.Nameservers = nil }, nil, "", 0},
		{"updates on", updates.ID, debian, nil, updates.Auto{Action: updates.AutoOn, Records: records, Version: "v0.4.0"}, "Turn on", 0},
		{"enableUnattendedUpgrades false", updates.ID, debian, func(c *config.Config) { c.EnableUnattendedUpgrades = false }, nil, "", 0},
		{"ssh auth", ssh.Auth{}.ID(), debian, nil, nil, "", 0},
		{"apparmor", apparmor.ID, debian, nil, nil, "", 0},
		{"backup", backupctl.ID, debian, nil, nil, "", 0},
		{"environment", envctl.ID, debian, nil, nil, "", 0},
	}
	for _, tt := range tests {
		cfg := full()
		if tt.set != nil {
			tt.set(cfg)
		}
		in := runall.Input{Config: cfg, Facts: tt.facts, Revert: records, Version: "v0.4.0"}
		a := runall.Asked(in, tt.id)
		if a.Words != tt.words || a.Port != tt.port || a.Asked() != (tt.want != nil || tt.port != 0) {
			t.Errorf("%s: words %q, port %d, asked %v; want %q, %d", tt.name, a.Words, a.Port, a.Asked(), tt.words, tt.port)
		}
		if !sameControl(a.Control, tt.want, cfg) {
			t.Errorf("%s: control %#v, want %#v", tt.name, a.Control, tt.want)
		}
	}
}

// sameControl reports whether got is want, Getenv and the config pointer
// aside.
func sameControl(got, want engine.Control, cfg *config.Config) bool {
	switch w := want.(type) {
	case nil:
		return got == nil
	case users.Admin:
		g, ok := got.(users.Admin)
		return ok && g.Getenv != nil && g.Action == w.Action && g.New.Name == w.New.Name && g.New.Admin && !g.New.Password && slices.Equal(g.New.Keys, w.New.Keys)
	case sshchange.Disallow:
		g, ok := got.(sshchange.Disallow)
		return ok && g.Getenv != nil && g.Want == w.Want
	case dnsctl.Nameservers:
		g, ok := got.(dnsctl.Nameservers)
		return ok && g.Config == cfg
	case updates.Auto:
		g, ok := got.(updates.Auto)
		return ok && g.Action == w.Action && g.Version == w.Version && g.Records.Dir == w.Records.Dir
	}
	return got == want
}

// TestPending: a change waiting for hardn confirm, read before any plan:
// every row asked of but the port Pending with SSH Login's words, nothing
// planned, the reading refused with them; and debian13 read again after
// its lockout-risk plan is applied, unconfirmed, the same.
func TestPending(t *testing.T) {
	check := func(name string, r runall.Reading) {
		t.Helper()
		if !r.Busy || !r.Pending() || r.Plan != nil || len(r.Planned()) > 0 || r.Refusal != runall.PendingWords || !errors.Is(r.Err, revert.ErrPending) {
			t.Errorf("%s: busy %v, pending %v, plan %v, %d planned, refusal %q, err %v", name, r.Busy, r.Pending(), r.Plan != nil, len(r.Planned()), r.Refusal, r.Err)
		}
		for _, rw := range r.Rows {
			if rw.Kind == runall.Pending && (rw.Words != runall.WordsPending || rw.Refusal != runall.PendingWords || !errors.Is(rw.Err, revert.ErrPending)) {
				t.Errorf("%s %s: %q, %q, %v", name, rw.ID, rw.Words, rw.Refusal, rw.Err)
			}
		}
	}
	check("pending", newHost(t, "pending").read(t, "pending"))

	h := newHost(t, "debian13")
	r := h.read(t, "debian13")
	if !r.Lockout() || r.Refusal != "" {
		t.Fatalf("debian13: lockout %v, refusal %q", r.Lockout(), r.Refusal)
	}
	if _, err := h.newEngine(nil).Apply(context.Background(), r.Plan); err != nil {
		t.Fatal(err)
	}
	again := h.read(t, "debian13")
	check("debian13 after the apply", again)
	for _, rw := range again.Rows {
		if a := runall.Asked(h.input(), rw.ID); a.Control != nil && rw.Kind != runall.Pending {
			t.Errorf("debian13 after the apply: %s %v, want pending", rw.ID, rw.Kind)
		}
	}
}

// TestAfterFirewall is decision 5 (a): where Enable and root login off both
// plan changes (ufw installed and off), the engine refuses the two as one
// plan, so the firewall is planned and root login off skipped, said; where
// the firewall has nothing to change, root login off is planned.
func TestAfterFirewall(t *testing.T) {
	ctx := context.Background()
	h := newHost(t, "ufw-inactive")
	in := h.input()
	both := []engine.Control{runall.Asked(in, fwctl.ID).Control, runall.Asked(in, login).Control}
	p, err := h.newEngine(nil).Plan(ctx, both, h.cfg)
	if err != nil || len(p.Refused) == 0 || !errors.Is(p.Refused[0], engine.ErrConflict) {
		t.Fatalf("ufw-inactive: Enable and root login off together: %v, refused %v; want ErrConflict, the rule's premise", err, p.Refused)
	}
	r := h.read(t, "ufw-inactive")
	rw, _ := r.Row(login)
	if rw.Kind != runall.Later || rw.Words != runall.WordsAfterFirewall || rw.Control != nil || r.Refusal != "" || r.Plan == nil || len(r.Plan.Refused) > 0 {
		t.Errorf("ufw-inactive: root login %v %q, refusal %q; want later, the plan unrefused", rw.Kind, rw.Words, r.Refusal)
	}
	if rw, _ := newHost(t, "debian13").read(t, "debian13").Row(login); rw.Kind != runall.Planned {
		t.Errorf("debian13, ufw on: root login %v, want planned", rw.Kind)
	}
}

// TestCombinedRefused: the one plan's refusal, decision 5 (a)'s rule not
// applied (root login off put back with the firewall): the engine refuses
// the two together, and Apply all is refused with the plan's first
// refusal, in its words, the plan kept. A guard for a conflict the rule
// does not cover.
func TestCombinedRefused(t *testing.T) {
	ctx := context.Background()
	h := newHost(t, "ufw-inactive")
	in := h.input()
	r := h.read(t, "ufw-inactive")
	i := slices.IndexFunc(r.Rows, func(rw runall.Row) bool { return rw.ID == login })
	r.Rows[i].Kind, r.Rows[i].Control, r.Plan = runall.Planned, runall.Asked(in, login).Control, nil
	if err := runall.Combine(ctx, in, &r); err != nil {
		t.Fatal(err)
	}
	if r.Plan == nil || len(r.Plan.Refused) == 0 || r.Err != r.Plan.Refused[0] || !errors.Is(r.Err, engine.ErrConflict) || r.Refusal != screenkit.RefusalWords(r.Err) {
		t.Errorf("plan %v, err %v, refusal %q; want the plan kept, its first refusal (ErrConflict) in its words", r.Plan != nil, r.Err, r.Refusal)
	}
}

// TestPortRow: the SSH Port row where sshd cannot be read (no
// /run/sshd, so sshd -T fails) is Refused, the read's error its words;
// and the read is given in.Session: with it, sshd's log of the session's
// login is read (journalctl), without it not.
func TestPortRow(t *testing.T) {
	h := newHost(t, "port")
	if err := h.Remove(context.Background(), "/run/sshd"); err != nil {
		t.Fatal(err)
	}
	rw, _ := h.read(t, "port").Row(port)
	if rw.Kind != runall.Refused || rw.Words != runall.WordsRefused || rw.Err == nil || rw.Refusal != screenkit.RefusalWords(rw.Err) {
		t.Errorf("sshd not read: %v %q, refusal %q, err %v; want refused, the read's words", rw.Kind, rw.Words, rw.Refusal, rw.Err)
	}

	logs := func(sess revert.Session) int {
		h := withYML(t, vm(debian13()).withLog("publickey", "george", 1), "sshPort: 2230\n")
		in := h.input()
		in.Session = sess
		n := len(h.Calls())
		r, err := runall.Read(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		if rw, _ := r.Row(port); rw.Kind != runall.Elsewhere {
			t.Errorf("port only: %v %q, want elsewhere", rw.Kind, rw.Words)
		}
		c := 0
		for _, call := range h.Calls()[n:] {
			if call.Op == "run" && call.Argv[0] == "journalctl" {
				c++
			}
		}
		return c
	}
	if with, without := logs(applySession), logs(revert.Session{}); with == 0 || without != 0 {
		t.Errorf("sshd's log read %d times with the session, %d without; want some, none", with, without)
	}
}

// TestAutoUpdatesAlpine: Auto Updates planned, on Alpine 3.23 with crond
// installed and started and apk-cron not (the gallery's runall-alpine323):
// apk-cron's install is in the one plan.
func TestAutoUpdatesAlpine(t *testing.T) {
	h := newHost(t, "alpine323")
	h.cfg.EnableUnattendedUpgrades = true
	h.AddFile(alpine.InitScript, []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
	h.AddSymlink(alpine.RunlevelLink, alpine.InitScript)
	r := h.read(t, "alpine323")
	if rw, _ := r.Row(updates.ID); rw.Kind != runall.Planned || rw.Words != "Turn on" {
		t.Errorf("auto updates: %v %q (%v), want planned, Turn on", rw.Kind, rw.Words, rw.Err)
	}
	held(t, "alpine323 with updates", h, r)
}

// noPasswd is a host whose /etc/passwd cannot be read: Read's System, the
// engine's still the host, so Create plans (the name taken) and the users
// read fails.
type noPasswd struct{ host.System }

func (n noPasswd) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if name == accounts.PasswdFile {
		return nil, fs.ErrPermission
	}
	return n.System.ReadFile(ctx, name)
}

// stages are p's stages in order, each once, the boot unit's left out.
func stages(p *engine.Plan) []engine.Stage {
	var out []engine.Stage
	for _, c := range p.Changes {
		if c.Stage != engine.StageBoot && (len(out) == 0 || out[len(out)-1] != c.Stage) {
			out = append(out, c.Stage)
		}
	}
	return out
}

// TestCombined holds the one plan (decision 5 (a)): the engine's order,
// accounts before the firewall before files before the reload, and its
// risk; no plan saves hardn.yml (neither DNS's save nor a backup saver's).
func TestCombined(t *testing.T) {
	tests := []struct {
		host    string
		want    []engine.Stage
		lockout bool
	}{
		{"new-admin", []engine.Stage{engine.StageAccounts, engine.StageFiles, engine.StageReload, engine.StageVerify}, true},
		{"carol-ufw-inactive", []engine.Stage{engine.StageAccounts, engine.StageFirewall, engine.StageFiles}, true},
		{"debian13", []engine.Stage{engine.StageFiles, engine.StageReload, engine.StageVerify}, true},
		{"fresh", []engine.Stage{engine.StageAccounts, engine.StageFiles}, false},
	}
	for _, tt := range tests {
		r := newHost(t, tt.host).read(t, tt.host)
		if r.Plan == nil || r.Refusal != "" {
			t.Fatalf("%s: plan %v, refusal %q", tt.host, r.Plan != nil, r.Refusal)
		}
		if got := stages(r.Plan); !slices.Equal(got, tt.want) || r.Lockout() != tt.lockout {
			t.Errorf("%s: stages %v, lockout %v; want %v, %v", tt.host, got, r.Lockout(), tt.want, tt.lockout)
		}
		if !slices.EqualFunc(r.Plan.Items, r.Controls(), func(it engine.Item, c engine.Control) bool { return it.Control == c.ID() }) {
			t.Errorf("%s: the plan's controls are not the planned rows'", tt.host)
		}
	}
	for _, name := range runHosts {
		r := newHost(t, name).read(t, name)
		if r.Plan == nil {
			continue
		}
		for _, c := range r.Plan.Changes {
			if c.ID == dnsctl.ChangeSave || c.ID == backupctl.ChangeSave || c.Control == backupctl.ID {
				t.Errorf("%s: the plan has %s", name, c.ID)
			}
		}
	}
}

// TestWritesNothing: Read plans only, on every host: nothing changes
// outside the engine's run directory.
func TestWritesNothing(t *testing.T) {
	for _, name := range runHosts {
		h := newHost(t, name)
		n := len(h.Calls())
		h.read(t, name)
		if w := writes(h, n); len(w) > 0 {
			t.Errorf("%s: Read changed the host: %q", name, w)
		}
	}
}

// TestUserThere: a user there is as asked only as an admin with every
// asked key, else User Management's with what is missing.
func TestUserThere(t *testing.T) {
	tests := []struct {
		host string
		keys []string
		want string
		kind runall.Kind
	}{
		{"debian13", nil, runall.WordsAsked, runall.AsAsked},
		{"george-no-sudo", nil, "User Management's: not an admin", runall.Elsewhere},
		{"debian13", []string{synthKey(9, "george@laptop-2")}, "User Management's: 1 key missing", runall.Elsewhere},
		{"george-no-sudo", []string{synthKey(9, "george@laptop-2"), synthKey(8, "george@laptop-3")}, "User Management's: not an admin, 2 keys missing", runall.Elsewhere},
		{"debian13", []string{"ssh-ed25519 not-a-key george@laptop-2"}, "User Management's: 1 key missing", runall.Elsewhere},
	}
	for _, tt := range tests {
		h := newHost(t, tt.host)
		h.cfg.SshKeys = append(slices.Clone(h.cfg.SshKeys), tt.keys...)
		rw, _ := h.read(t, tt.host).Row(users.ID)
		if rw.Kind != tt.kind || rw.Words != tt.want || rw.Control != nil {
			t.Errorf("%s with %d more keys: %v %q; want %v %q", tt.host, len(tt.keys), rw.Kind, rw.Words, tt.kind, tt.want)
		}
	}

	// sshd's name: taken, but no user the read lists (a system account).
	h := newHost(t, "debian13")
	h.cfg.Username = "sshd"
	if rw, _ := h.read(t, "debian13").Row(users.ID); rw.Kind != runall.Elsewhere || rw.Words != "User Management's: name taken" {
		t.Errorf("sshd: %v %q, want User Management's: name taken", rw.Kind, rw.Words)
	}

	// The users read failing: Refused, its error the row's why.
	h = newHost(t, "debian13")
	in := h.input()
	in.System = noPasswd{h}
	r, err := runall.Read(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	rw, _ := r.Row(users.ID)
	if rw.Kind != runall.Refused || rw.Words != runall.WordsRefused || !errors.Is(rw.Err, fs.ErrPermission) || rw.Refusal != screenkit.RefusalWords(rw.Err) {
		t.Errorf("users not read: %v %q, refusal %q, err %v; want refused, the read's error", rw.Kind, rw.Words, rw.Refusal, rw.Err)
	}
}

// TestReadInvalid: Read needs a System, a Config and NewEngine.
func TestReadInvalid(t *testing.T) {
	h := newHost(t, "debian13")
	for _, in := range []runall.Input{{Config: h.cfg, NewEngine: h.newEngine}, {System: h, NewEngine: h.newEngine}, {System: h, Config: h.cfg}} {
		if _, err := runall.Read(context.Background(), in); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("%+v: %v, want fs.ErrInvalid", in, err)
		}
	}
}

// TestPortRocky: on Rocky Linux the SSH Port row whose sshPort sshd doesn't
// listen on is SSH Login's to change, as on Debian: SSH Login changes the
// port there too (el-ports.md).
func TestPortRocky(t *testing.T) {
	p := debian13()
	p.facts.Platform = detect.Platform{Distro: detect.DistroRocky, Version: "10", Support: detect.Supported}
	p.facts.SSH.SSHDUnits = true
	h := withYML(t, vm(p), "sshPort: 2230\n")
	r, err := runall.Read(context.Background(), h.input())
	if err != nil {
		t.Fatal(err)
	}
	if rw, _ := r.Row(port); rw.Kind != runall.Elsewhere {
		t.Errorf("port: %v %q, err %v; want elsewhere", rw.Kind, rw.Words, rw.Err)
	}
}

// TestFirewallRocky: on Rocky Linux the Firewall row is status only, the
// Firewall screen's, even where hardn.yml asks for the firewall
// (enableUfwSshPolicy): Run All plans no firewall change there
// (el-firewalld.md §1.7).
func TestFirewallRocky(t *testing.T) {
	p := debian13()
	p.facts.Platform = detect.Platform{Distro: detect.DistroRocky, Version: "10", Support: detect.Supported}
	p.facts.SSH.SSHDUnits = true
	h := withYML(t, vm(p), "enableUfwSshPolicy: true\n")
	r, err := runall.Read(context.Background(), h.input())
	if err != nil {
		t.Fatal(err)
	}
	if rw, _ := r.Row(fwctl.ID); rw.Kind != runall.Left || rw.Words != runall.WordsStatus || rw.Control != nil {
		t.Errorf("firewall: %v %q, control %v; want left, status only", rw.Kind, rw.Words, rw.Control)
	}
}

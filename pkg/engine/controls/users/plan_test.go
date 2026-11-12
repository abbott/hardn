package users_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/revert/plans"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
)

// The hosts the plans run on: gallery-users.md §3's, in the VMs' shapes
// (s5/00-inspect-*.log, 2026-11-03) with synthetic values: george, the
// login user, uid 1000, password field "!" (Debian, Ubuntu) or "*"
// (Alpine); alice, uid 1001, a hash (synthetic); cloud-init's NOPASSWD
// file for george.
const (
	georgeLine  = "george:x:1000:1000::/home/george:/bin/bash"
	aliceLine   = "alice:x:1001:1001::/home/alice:/bin/bash"
	hashSet     = "$y$j9T$Kp3Lm8Nq2Rs5Tv7Wx9Yz1$Bc4Df6Gh8Jk0Lm2Np4Qr6St8Uv0Wx2Yz4Ab6Cd8Ef0"
	cloudInit   = sudoers.DropInDir + "/90-cloud-init-users"
	cloudRule   = "george ALL=(ALL) NOPASSWD:ALL\n"
	hardnBinary = "/usr/local/bin/hardn"
)

var (
	// sudoGeorge and sudoAlice are hardn run through sudo by george, by
	// alice; rootLogin is root's own login (no SUDO_USER).
	sudoGeorge = map[string]string{"SUDO_USER": "george", "USER": "root"}
	rootLogin  = map[string]string{"USER": "root"}
	remote     = revert.Session{Remote: true, Conn: "192.0.2.10 50022 192.0.2.20 22", Source: "SSH_CONNECTION"}
	now        = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
)

func env(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }

// debian13Login is Debian 13 as the VM is: systemd running, george and
// alice in sudo, george granted by cloud-init's file too.
func debian13Login(files ...map[string]string) site {
	p := with(debian13(), func(f *detect.Facts) {
		f.Init = detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir}
	})
	return site{plat: p, users: []string{georgeLine, aliceLine}, locks: map[string]string{"george": "!", "alice": hashSet},
		files: merge(files...)}
}

// live builds s with what the engine needs to apply a plan: its
// directories, hardn's binary for the revert timer, and the timer's and
// the boot unit's commands answered.
func live(t *testing.T, s site) (host.System, *hostfake.System) {
	t.Helper()
	seed := s.seed
	s.seed = func(h *hostfake.System) {
		for _, d := range []string{"/var/lib", "/var/backups", "/run", "/var/log", "/usr/local/bin", "/usr/lib/systemd/system", "/etc/systemd/system", "/etc/init.d", "/etc/runlevels/default"} {
			h.AddDir(d, 0o755, 0, 0)
		}
		h.AddDir(revert.ProcDir, 0o555, 0, 0) // no sshd process: the session is the journal's alone
		h.AddFile(hardnBinary, []byte("ELF"), 0o755, 0, 0)
		if seed != nil {
			seed(h)
		}
	}
	sys, h := s.build(t)
	inner := h.Handler
	h.Handler = func(argv []string, stdin []byte) hostfake.Response {
		cmd := strings.Join(argv, " ")
		switch {
		case cmd == "systemctl --no-reload enable hardn-boot.service":
			h.AddSymlink("/etc/systemd/system/multi-user.target.wants/hardn-boot.service", "/etc/systemd/system/hardn-boot.service")
			return hostfake.Response{}
		case cmd == "rc-update add hardn-boot default":
			h.AddSymlink(detect.OpenRCRunlevels+"/default/hardn-boot", "/etc/init.d/hardn-boot")
			return hostfake.Response{}
		case argv[0] == "systemd-run", argv[0] == "systemd-analyze", argv[0] == "sh", argv[0] == "/bin/sh", argv[0] == "setsid", argv[0] == "rc-service", argv[0] == "rc-update",
			argv[0] == "systemctl" && strings.Contains(cmd, "hardn"), cmd == "systemctl daemon-reload":
			return hostfake.Response{}
		}
		return inner(argv, stdin)
	}
	return sys, h
}

// engineOn is the engine on sys as cmd/hardn builds it: its clock, the
// facts, the default backups, an SSH session.
func engineOn(sys host.System, f detect.Facts) *engine.Engine {
	return engine.New(sys, engine.Options{
		Revert:  revert.Options{Now: func() time.Time { return now }, Hardn: hardnBinary},
		Detect:  func(context.Context, host.System) (detect.Facts, error) { return f, nil },
		Hold:    func() func() { return func() {} },
		PID:     5000,
		Session: remote,
		Backups: backup.Settings{Path: backup.DefaultPath, Enabled: true},
	})
}

// planOn plans a on s through the engine, and the calls that changed
// something outside hardn's run directory, which must be none.
func planOn(t *testing.T, s site, a users.Admin) (*engine.Engine, *engine.Plan, host.System, *hostfake.System) {
	t.Helper()
	sys, h := live(t, s)
	e := engineOn(sys, s.plat.facts)
	n := len(h.Calls())
	p, err := e.Plan(context.Background(), []engine.Control{a}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range h.Calls()[n:] {
		switch c.Op {
		case "read", "read-nofollow", "stat", "lstat", "readdir", "run":
		default:
			if !strings.HasPrefix(c.Path, "/run/hardn") {
				t.Errorf("planning wrote: %v", c)
			}
		}
	}
	return e, p, sys, h
}

// refusalOf is the plan's one refusal, a *steps.Refusal.
func refusalOf(t *testing.T, p *engine.Plan) *steps.Refusal {
	t.Helper()
	var rf *steps.Refusal
	if !p.Empty() || len(p.Items) != 1 || !errors.As(p.Items[0].Err, &rf) {
		t.Fatalf("plan: %d changes, items %+v", len(p.Changes), p.Items)
	}
	return rf
}

// TestRefusals: every refusal, in the decided words (gallery-users.md §6)
// as the screen says them (screenkit.RefusalWords), with its code, the
// registry's, and nothing written before it.
func TestRefusals(t *testing.T) {
	d := debian13Login().plat
	hardnGeorge := written(t, d, sudoers.Policy{User: "george", Grant: sudoers.GrantNoPassword})
	hardnAlice := written(t, d, sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword})
	inSudo := map[string]string{"sudo": "george,alice"}
	for _, tc := range []struct {
		name  string
		site  site
		a     users.Admin
		code  string
		words string
	}{
		{"no tool", site{plat: pve9(), users: []string{georgeLine}}, users.Admin{Action: users.ActionGrant, User: "george"},
			"USERS_NO_TOOL", "sudo isn't installed, so hardn can't grant admin rights. Install sudo, then try again."},
		{"rules not read", func() site { s := debian13Login(); s.reject = sudoers.MainConfig; return s }(), users.Admin{Action: users.ActionRemove, User: "george"},
			"USERS_ELEVATION_NOT_READ", "hardn can't read sudo's rules: its files don't check. Check them with visudo -c, then try again."},
		{"not listed", debian13Login(), users.Admin{Action: users.ActionGrant, User: "carol"},
			"USERS_NOT_LISTED", "carol isn't a user hardn manages here. Choose one of the users listed."},
		{"edited", debian13Login(map[string]string{grantDir + "hardn-grant-george": "george ALL=(ALL) ALL # mine\n"}), users.Admin{Action: users.ActionRemove, User: "george"},
			"USERS_GRANT_EDITED", "/etc/sudoers.d/hardn-grant-george holds a rule hardn didn't write. Change it there, then try again."},
		{"a group's", func() site { s := debian13Login(); s.members = inSudo; return s }(), users.Admin{Action: users.ActionRemove, User: "alice"},
			"USERS_GROUP_GRANT", "alice is an admin through the sudo group, which hardn leaves alone. Take alice out of it with gpasswd -d alice sudo."},
		{"cloud-init's", debian13Login(map[string]string{cloudInit: cloudRule}), users.Admin{Action: users.ActionRemove, User: "george"},
			"USERS_GRANT_NOT_HARDNS", "george is an admin through /etc/sudoers.d/90-cloud-init-users, which hardn didn't write, so hardn can't remove it. Change it there."},
		{"still, cloud-init's", debian13Login(hardnGeorge, map[string]string{cloudInit: cloudRule}), users.Admin{Action: users.ActionRemove, User: "george"},
			"USERS_STILL_ADMIN", "george would stay an admin through /etc/sudoers.d/90-cloud-init-users, which hardn didn't write. Change it there, then try again."},
		{"still, two", func() site {
			s := debian13Login(hardnAlice, map[string]string{grantDir + "50-site": "alice ALL=(ALL:ALL) ALL\n"})
			s.members = inSudo
			return s
		}(), users.Admin{Action: users.ActionRemove, User: "alice", Getenv: env(sudoGeorge)},
			"USERS_STILL_ADMIN", "alice would stay an admin through /etc/sudoers.d/50-site and the sudo group, which hardn didn't write. Change it there, then try again."},
		{"still, a rule not read", debian13Login(hardnGeorge, map[string]string{grantDir + "50-site": "george ALL=(root) /usr/bin/apt\n"}), users.Admin{Action: users.ActionRemove, User: "george"},
			"USERS_STILL_UNKNOWN", "hardn can't tell whether george would stay an admin: it can't read /etc/sudoers.d/50-site:1. Check it, then try again."},
		{"the session user's own", debian13Login(hardnGeorge), users.Admin{Action: users.ActionRemove, User: "george", Getenv: env(sudoGeorge)},
			"USERS_SESSION_USER", "You're running hardn through george's sudo. Removing it would leave no way to become root. Grant another user first."},
		{"the last admin, session not known", debian13Login(hardnGeorge), users.Admin{Action: users.ActionRemove, User: "george", Getenv: env(rootLogin)},
			"USERS_LAST_ADMIN", "hardn can't tell who you're logged in as, and george is the last admin. Grant another user first."},
		{"the session user's own, still granted", debian13Login(hardnGeorge, map[string]string{cloudInit: cloudRule}), users.Admin{Action: users.ActionRemove, User: "george", Getenv: env(sudoGeorge)},
			"USERS_STILL_ADMIN", "george would stay an admin through /etc/sudoers.d/90-cloud-init-users, which hardn didn't write. Change it there, then try again."},
		{"not listed, no tool", site{plat: pve9(), users: []string{georgeLine}}, users.Admin{Action: users.ActionGrant, User: "carol"},
			"USERS_NO_TOOL", "sudo isn't installed, so hardn can't grant admin rights. Install sudo, then try again."},
		{"not listed, rules not read", func() site { s := debian13Login(); s.reject = sudoers.MainConfig; return s }(), users.Admin{Action: users.ActionGrant, User: "carol"},
			"USERS_ELEVATION_NOT_READ", "hardn can't read sudo's rules: its files don't check. Check them with visudo -c, then try again."},
		{"ask, not an admin", debian13Login(), users.Admin{Action: users.ActionAskPassword, User: "alice"},
			"USERS_NOT_ADMIN", "alice isn't an admin. Grant admin rights first."},
		{"don't ask, not an admin", debian13Login(), users.Admin{Action: users.ActionNoPassword, User: "alice"},
			"USERS_NOT_ADMIN", "alice isn't an admin. Grant admin rights first."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, p, _, _ := planOn(t, tc.site, tc.a)
			rf := refusalOf(t, p)
			if rf.Code != tc.code || output.CodeOf(rf) != tc.code || output.CodeOf(p.Err()) != tc.code {
				t.Errorf("code %s, registry's %s, want %s", rf.Code, output.CodeOf(rf), tc.code)
			}
			if got := screenkit.RefusalWords(rf); got != tc.words {
				t.Errorf("words\n %q\nwant\n %q", got, tc.words)
			}
		})
	}
}

// TestAskPassword: asking a password refuses for a user with none, the
// field "!" (Debian's and Ubuntu's login user), "*" (Alpine's) or empty,
// and is allowed with a hash, as a lockout-risk plan (sudoers.RiskLockout:
// the typed hostname, confirm or revert).
func TestAskPassword(t *testing.T) {
	g := written(t, debian13(), sudoers.Policy{User: "george", Grant: sudoers.GrantNoPassword})
	for _, field := range []string{"!", "*", ""} {
		s := debian13Login(g)
		s.locks["george"] = field
		_, p, _, _ := planOn(t, s, users.Admin{Action: users.ActionAskPassword, User: "george", Getenv: env(sudoGeorge)})
		rf := refusalOf(t, p)
		if want := "george has no password, so sudo couldn't ask for one. Set one with passwd george, then try again."; rf.Code != "USERS_NO_PASSWORD_TO_ASK" ||
			output.CodeOf(rf) != rf.Code || screenkit.RefusalWords(rf) != want {
			t.Errorf("%q: %s %q", field, rf.Code, screenkit.RefusalWords(rf))
		}
	}
	s := debian13Login(g)
	s.locks["george"] = hashSet
	_, p, _, _ := planOn(t, s, users.Admin{Action: users.ActionAskPassword, User: "george", Getenv: env(sudoGeorge)})
	if p.Err() != nil || p.Risk != engine.RiskLockout || len(p.Changes) == 0 || p.Changes[len(p.Changes)-1].ID != users.ChangeGrant ||
		p.Changes[len(p.Changes)-1].Summary != "Ask george's password" {
		t.Errorf("with a hash: %v, risk %v, %+v", p.Err(), p.Risk, p.Changes)
	}
}

// TestGrant: Grant writes hardn's grant, asking alice's password (a hash)
// and george's too (none: "!"), with the finding that george can't use it
// until one is set (the maintainer, 2026-11-11: no password is opt-in),
// plain (y/N); granted already by hardn, an empty plan. Don't ask password
// is plain too.
func TestGrant(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		user, want, finding string
	}{
		{"alice", "alice ALL=(ALL) ALL\n", ""},
		{"george", "george ALL=(ALL) ALL\n", "george can't use sudo until a password is set: run passwd george"},
	} {
		e, p, _, h := planOn(t, debian13Login(), users.Admin{Action: users.ActionGrant, User: tc.user, Getenv: env(sudoGeorge)})
		if p.Err() != nil || p.Risk != engine.RiskPlain || len(p.Changes) != 1 || p.Changes[0].Risk != engine.RiskPlain || p.Changes[0].Summary != "Let "+tc.user+" use sudo" {
			t.Fatalf("%s: %v, risk %v, %+v", tc.user, p.Err(), p.Risk, p.Changes)
		}
		if fs := p.Changes[0].Findings; (tc.finding == "") != (len(fs) == 0) || (len(fs) > 0 && (fs[0].Code != users.CodePasswordNotSet || fs[0].Detail != tc.finding)) {
			t.Errorf("%s: findings %+v, want %q", tc.user, fs, tc.finding)
		}
		if _, err := e.Apply(ctx, p); err != nil {
			t.Fatal(err)
		}
		name := grantDir + "hardn-grant-" + tc.user
		if e, ok := h.Entry(name); !ok || string(e.Data) != sudoers.Header+tc.want || e.Mode.Perm() != 0o440 {
			t.Errorf("%s: %q", name, e.Data)
		}
	}
	s := debian13Login(written(t, debian13(), sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword}))
	if _, p, _, _ := planOn(t, s, users.Admin{Action: users.ActionGrant, User: "alice"}); p.Err() != nil || !p.Empty() {
		t.Errorf("granted already: %v, %d changes", p.Err(), len(p.Changes))
	}
	s = debian13Login(written(t, debian13(), sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword}))
	if _, p, _, _ := planOn(t, s, users.Admin{Action: users.ActionNoPassword, User: "alice"}); p.Err() != nil || p.Risk != engine.RiskPlain || len(p.Changes) != 1 {
		t.Errorf("don't ask: %v, risk %v, %d changes", p.Err(), p.Risk, len(p.Changes))
	}
}

// TestRemove: decision 4 both ways. Another user's grant (alice's, with
// george the session user; and with the session not known while george is
// an admin too) is a lockout-risk plan removing hardn's file alone, and
// the rollback puts it back byte for byte, its mode too; the session
// user's own and, with the session not known, the last admin's are refused
// (TestRefusals).
func TestRemove(t *testing.T) {
	ctx := context.Background()
	d := debian13()
	both := merge(written(t, d, sudoers.Policy{User: "george", Grant: sudoers.GrantNoPassword}), written(t, d, sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword}))
	for name, vars := range map[string]map[string]string{"george's session": sudoGeorge, "session not known, two admins": rootLogin} {
		t.Run(name, func(t *testing.T) {
			e, p, sys, h := planOn(t, debian13Login(both), users.Admin{Action: users.ActionRemove, User: "alice", Getenv: env(vars)})
			if p.Err() != nil || p.Risk != engine.RiskLockout {
				t.Fatalf("plan: %v, risk %v", p.Err(), p.Risk)
			}
			c := p.Changes[len(p.Changes)-1]
			if c.ID != users.ChangeGrant || c.Summary != "Remove alice's sudo" || c.Target != grantDir+"hardn-grant-alice" {
				t.Errorf("change %+v", c)
			}
			before, _ := h.Entry(grantDir + "hardn-grant-alice")
			res, err := e.Apply(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := h.Entry(grantDir + "hardn-grant-alice"); ok {
				t.Error("hardn-grant-alice still there")
			}
			if _, err := revert.Rollback(ctx, sys, res.ID, revert.Options{Now: func() time.Time { return now.Add(time.Minute) }, Hardn: hardnBinary, Reason: revert.ReasonTimeout, Plans: plans.Registry()}); err != nil {
				t.Fatal(err)
			}
			after, ok := h.Entry(grantDir + "hardn-grant-alice")
			if !ok || string(after.Data) != string(before.Data) || after.Mode != before.Mode || after.UID != 0 || after.GID != 0 {
				t.Errorf("rolled back: %q %v, was %q %v", after.Data, after.Mode, before.Data, before.Mode)
			}
		})
	}
	if _, p, _, _ := planOn(t, debian13Login(), users.Admin{Action: users.ActionRemove, User: "alice"}); p.Err() != nil || !p.Empty() {
		t.Errorf("no grant at all: %v, %d changes", p.Err(), len(p.Changes))
	}
}

// TestLastAdmin: with the session not known (a root login), another admin
// counts only where it can become root: bob, in group sudo, whose rule
// asks a password, with the field "!" cannot, so removing alice's grant
// (NOPASSWD, hardn's) refuses as the last admin's; with a hash, it plans.
func TestLastAdmin(t *testing.T) {
	alice := written(t, debian13(), sudoers.Policy{User: "alice", Grant: sudoers.GrantNoPassword})
	for field, code := range map[string]string{"!": "USERS_LAST_ADMIN", hashSet: ""} {
		s := debian13Login(alice)
		s.users = []string{aliceLine, "bob:x:1002:1002::/home/bob:/bin/bash"}
		s.members = map[string]string{"sudo": "bob"}
		s.locks = map[string]string{"alice": hashSet, "bob": field}
		_, p, _, _ := planOn(t, s, users.Admin{Action: users.ActionRemove, User: "alice", Getenv: env(rootLogin)})
		if got := codeOf(p.Err()); got != code {
			t.Errorf("bob's field %q: %q, want %q", field, got, code)
		}
		if code == "" && (p.Risk != engine.RiskLockout || p.Changes[len(p.Changes)-1].ID != users.ChangeGrant) {
			t.Errorf("bob's hash: risk %v, %+v", p.Risk, p.Changes)
		}
	}
}

// TestGrantRisk: Grant keeps the planner's risk for a user who is an admin
// already: alice, a hash, granted by a NOPASSWD file, gets hardn's file
// asking her password, which sudo now asks where it asked none: a lockout
// risk. bob, not an admin, plain.
func TestGrantRisk(t *testing.T) {
	for user, want := range map[string]engine.RiskClass{"alice": engine.RiskLockout, "bob": engine.RiskPlain} {
		s := debian13Login(map[string]string{grantDir + "50-site": "alice ALL=(ALL) NOPASSWD:ALL\n"})
		s.users = []string{aliceLine, "bob:x:1002:1002::/home/bob:/bin/bash"}
		s.locks = map[string]string{"alice": hashSet, "bob": hashSet}
		_, p, _, _ := planOn(t, s, users.Admin{Action: users.ActionGrant, User: user, Getenv: env(rootLogin)})
		if p.Err() != nil || p.Risk != want || p.Changes[len(p.Changes)-1].Summary != "Let "+user+" use sudo" {
			t.Errorf("%s: %v, risk %v, want %v", user, p.Err(), p.Risk, want)
		}
	}
}

// TestStillAdmin: what still grants once hardn's grant is gone, read by the
// real reader: cloud-init's file, the sudo group, the wheel group on doas
// (nopass on the Alpine VM's wheel.conf), and both in turn; a rule asking
// a password the user lacks not counted; a rule not read, Unknown.
func TestStillAdmin(t *testing.T) {
	ctx := context.Background()
	d := debian13()
	hardnGeorge := written(t, d, sudoers.Policy{User: "george", Grant: sudoers.GrantNoPassword})
	alpine := with(alpine324(), func(f *detect.Facts) { f.Init = detect.Init{Kind: detect.OpenRC, Running: detect.Yes} })
	hardnDoas := written(t, alpine, sudoers.Policy{User: "george", Grant: sudoers.GrantNoPassword})
	wheelNopass := map[string]string{sudoers.DoasDir + "/20-wheel.conf": "", sudoers.DoasDir + "/wheel.conf": "permit nopass :wheel\n"}
	cloud := users.Rule{Kind: users.RuleFile, File: cloudInit, Line: 1, Text: strings.TrimSpace(cloudRule)}
	sudoGroup := users.Rule{Kind: users.RuleGroup, File: sudoers.MainConfig, Line: 22, Group: "sudo", Text: "%sudo ALL=(ALL:ALL) ALL"}
	wheel := users.Rule{Kind: users.RuleGroup, File: sudoers.DoasDir + "/wheel.conf", Line: 1, Group: "wheel", Text: "permit nopass :wheel"}
	for _, tc := range []struct {
		name string
		site site
		want users.Still
	}{
		{"hardn's alone", debian13Login(hardnGeorge), users.Still{}},
		{"cloud-init's", debian13Login(hardnGeorge, map[string]string{cloudInit: cloudRule}), users.Still{By: []users.Rule{cloud}}},
		{"the sudo group, a password george lacks", func() site {
			s := debian13Login(hardnGeorge)
			s.members = map[string]string{"sudo": "george"}
			return s
		}(), users.Still{}},
		{"the sudo group, a password george has", func() site {
			s := debian13Login(hardnGeorge, map[string]string{cloudInit: cloudRule})
			s.members, s.locks["george"] = map[string]string{"sudo": "george"}, hashSet
			return s
		}(), users.Still{By: []users.Rule{cloud, sudoGroup}}},
		{"wheel on doas", site{plat: alpine, users: []string{georgeLine}, members: map[string]string{"wheel": "george"}, locks: map[string]string{"george": "*"}, files: merge(hardnDoas, wheelNopass)},
			users.Still{By: []users.Rule{wheel}}},
		{"a rule not read", debian13Login(hardnGeorge, map[string]string{grantDir + "50-site": "george ALL=(root) /usr/bin/apt\n"}), users.Still{Unknown: grantDir + "50-site:1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sys, h := tc.site.build(t)
			r, err := users.Read(ctx, sys, tc.site.plat.facts, env(sudoGeorge))
			if err != nil {
				t.Fatal(err)
			}
			got := users.StillAdmin(ctx, sys, r, "george")
			if !slices.Equal(got.By, tc.want.By) || got.Unknown != tc.want.Unknown {
				t.Errorf("%+v, want %+v", got, tc.want)
			}
			readOnly(t, h)
		})
	}
}

// TestRead: what the screen draws, read only, with the commands it runs
// held: Debian 13 as the VM (george "!" in sudo and by cloud-init's file,
// alice a hash in sudo), Proxmox VE (root only, no sudo), and Debian
// without sudo.
func TestRead(t *testing.T) {
	ctx := context.Background()
	s := debian13Login(map[string]string{cloudInit: cloudRule})
	s.members, s.sshd = map[string]string{"sudo": "george,alice"}, sshdPAM
	sys, h := s.build(t)
	r, err := users.Read(ctx, sys, s.plat.facts, env(sudoGeorge))
	if err != nil {
		t.Fatal(err)
	}
	if r.Choice.Tool != sudoers.ToolSudo || r.Invoker != "george" || len(r.NotRead) != 0 || r.Status.State != engine.Configured || r.Status.Detail != "non-root, sudo" || r.StatusErr != nil {
		t.Errorf("reading %+v", r)
	}
	var names []string
	for _, u := range r.Users {
		names = append(names, u.Name)
	}
	if !slices.Equal(names, []string{"george", "alice"}) {
		t.Fatalf("users %q", names)
	}
	g, a := r.Users[0], r.Users[1]
	if g.Admin != detect.Yes || g.Tool != "sudo" || g.Decided.File != cloudInit || !g.NoPassword || g.Password != accounts.PasswordNone || g.Policy.Grant != sudoers.GrantNone || g.PolicyErr != nil {
		t.Errorf("george %+v", g)
	}
	if a.Admin != detect.Yes || a.Decided.Kind != users.RuleGroup || a.Decided.Group != "sudo" || a.NoPassword || a.Password != accounts.PasswordSet {
		t.Errorf("alice %+v", a)
	}
	readOnly(t, h)
	var runs []string
	for _, c := range h.Calls() {
		if c.Op == "run" {
			runs = append(runs, strings.Join(c.Argv, " "))
		}
	}
	// Check's, for george's "!" too; Read's: the rules, sshd's PAM, its key files.
	if want := []string{"visudo -c", "sshd -T -f " + sshdConfig, "visudo -c", "sshd -T -f " + sshdConfig, "sshd -T -f " + sshdConfig}; !slices.Equal(runs, want) {
		t.Errorf("ran %q, want %q", runs, want)
	}

	pve := site{plat: pve9()}
	sys, h = pve.build(t)
	if r, err = users.Read(ctx, sys, pve.plat.facts, env(rootLogin)); err != nil || r.Choice.Tool != sudoers.ToolNone || len(r.Users) != 0 || r.Invoker != "" ||
		r.Status.State != engine.NotConfigured || r.Status.Detail != "root only" {
		t.Errorf("Proxmox VE: %+v, %v", r, err)
	}
	readOnly(t, h)

	none := debian13Login()
	none.plat.facts.Elevation.Sudo = no
	sys, h = none.build(t)
	if r, err = users.Read(ctx, sys, none.plat.facts, env(sudoGeorge)); err != nil || r.Choice.Tool != sudoers.ToolNone || len(r.Users) != 2 || r.Users[0].Admin != detect.No || r.Invoker != "" {
		t.Errorf("no sudo: %+v, %v", r, err)
	}
	readOnly(t, h)
}

// TestNotMigrated: the status list's Admin (ActionNone) plans nothing:
// engine.ErrNotMigrated, reading nothing.
func TestNotMigrated(t *testing.T) {
	sys, h := debian13Login().build(t)
	cs, err := users.Admin{User: "george"}.Plan(context.Background(), sys, engine.Input{Facts: debian13().facts})
	if !errors.Is(err, engine.ErrNotMigrated) || cs != nil || len(h.Calls()) != 0 {
		t.Errorf("%v, %v, %d calls", cs, err, len(h.Calls()))
	}
}

// TestRemoveDoasKeptVariable: on doas a rule keeps variables only with its
// grant, so Remove takes a setenv an earlier hardn wrote away with the
// rule (the Environment screen, which could stop keeping it, is gone:
// 2026-11-12), where it was refused for good (SUDOERS_ENV_NEEDS_GRANT).
func TestRemoveDoasKeptVariable(t *testing.T) {
	alpine := with(alpine324(), func(f *detect.Facts) { f.Init = detect.Init{Kind: detect.OpenRC, Running: detect.Yes} })
	kept := written(t, alpine, sudoers.Policy{User: "george", Grant: sudoers.GrantNoPassword, Env: []string{"HARDN_CONFIG"}})
	s := site{plat: alpine, users: []string{georgeLine, aliceSh}, locks: map[string]string{"george": "*", "alice": "*"}, files: kept}
	asAlice := func(k string) string { return map[string]string{"DOAS_USER": "alice"}[k] }
	e, p, _, h := planOn(t, s, users.Admin{Action: users.ActionRemove, User: "george", Getenv: asAlice})
	if p.Err() != nil || len(p.Changes) == 0 {
		t.Fatalf("Remove: %v, %d changes", p.Err(), len(p.Changes))
	}
	if _, err := e.Apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.Entry(sudoers.DoasDir + "/zz-hardn-george.conf"); ok {
		t.Error("hardn's rule for george is still there")
	}
}

package migrate

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// plan runs Find and PlanFor on a fresh host shaped as sc.
func plan(t *testing.T, sc scenario) (*testHost, Plan, error) {
	t.Helper()
	h := newHost(t, sc.files)
	f, err := Find(context.Background(), h, FindRequest{ConfigFile: sc.configFile})
	if err != nil {
		t.Fatal(err)
	}
	pl, err := PlanFor(Input{Found: f, Desired: sc.desired, SSH: sc.ssh, Platform: sc.platform, Before: h.before(t, sc.ssh, sc.socket)})
	return h, pl, err
}

func opKinds(p Plan) []string {
	var ks []string
	for _, op := range p.Ops {
		ks = append(ks, op.Kind.String()+" "+op.Path)
	}
	return ks
}

// scenario is a host shape the migration meets, with the files it gives.
type scenario struct {
	name       string
	files      map[string]string
	configFile string
	desired    []Directive
	ssh        detect.SSH
	platform   detect.Platform
	socket     string // systemctl show -p Listen ssh.socket's output
	ops        []string
}

// customConf is a v0.3.x sshConfigFile other than its default.
const customConf = sshd.DropInDir + "/10-site-hardn.conf"

// The ssh.socket a v0.3.x listen.conf gives once a daemon-reload or a
// reboot has read it, and the stock one it had before.
const (
	socket2230 = "Listen=[::]:2230 (Stream)\n"
	socket22   = "Listen=[::]:22 (Stream)\n"
)

func scenarios(t *testing.T) []scenario {
	manage, listen := fixture(t, "v032-manage.conf"), fixture(t, "v032-listen.conf")
	return []scenario{
		{name: "v032-ubuntu", ssh: socketGen, platform: ubuntu2604, configFile: ManageConf, socket: socket2230,
			files: map[string]string{sshd.MainConfig: stockMain, sshd.DropInDir + "/50-cloud-init.conf": cloudInit, ManageConf: manage, ListenConf: listen},
			ops:   []string{"write " + sshd.HardnDropIn, "remove " + ManageConf, "remove-socket-drop-in " + ListenConf}},
		// v0.3.x never ran daemon-reload after writing listen.conf
		// (side-effects.md D9): until a reboot the socket keeps port 22.
		{name: "v032-ubuntu-unreloaded", ssh: socketGen, platform: ubuntu2604, configFile: ManageConf, socket: socket22,
			files: map[string]string{sshd.MainConfig: stockMain, sshd.DropInDir + "/50-cloud-init.conf": cloudInit, ManageConf: manage, ListenConf: listen},
			ops:   []string{"write " + sshd.HardnDropIn, "remove " + ManageConf, "remove-socket-drop-in " + ListenConf}},
		{name: "upgraded-ubuntu", ssh: socketGen, platform: ubuntu2604, configFile: HardnConf, socket: socket2230,
			files: map[string]string{sshd.MainConfig: stockMain, sshd.DropInDir + "/50-cloud-init.conf": cloudInit, ManageConf: manage,
				HardnConf: fixture(t, "main-hardn-r.conf"), ListenConf: listen},
			ops: []string{"write " + sshd.HardnDropIn, "remove " + ManageConf, "remove " + HardnConf, "remove-socket-drop-in " + ListenConf}},
		{name: "main-c-debian", ssh: service, platform: debian13,
			files: map[string]string{sshd.MainConfig: stockMain, HardnConf: fixture(t, "main-hardn-c.conf")},
			ops:   []string{"write " + sshd.HardnDropIn, "remove " + HardnConf}},
		{name: "v032-debian", ssh: service, platform: debian13, configFile: ManageConf,
			files: map[string]string{sshd.MainConfig: stockMain, ManageConf: manage, ListenConf: listen},
			ops:   []string{"write " + sshd.HardnDropIn, "remove " + ManageConf, "remove-socket-drop-in " + ListenConf}},
		{name: "custom-debian", ssh: service, platform: debian13, configFile: customConf,
			files: map[string]string{sshd.MainConfig: stockMain, customConf: manage, ListenConf: listen},
			ops:   []string{"write " + sshd.HardnDropIn, "remove " + customConf, "remove-socket-drop-in " + ListenConf}},
		{name: "listen-only-ubuntu", ssh: socketGen, platform: ubuntu2604, socket: socket2230,
			files: map[string]string{sshd.MainConfig: stockMain, ListenConf: listen},
			ops:   []string{"write " + sshd.HardnDropIn, "remove-socket-drop-in " + ListenConf}},
		{name: "desired-ubuntu", ssh: socketGen, platform: ubuntu2604, configFile: ManageConf, socket: socket2230,
			desired: []Directive{{"PermitRootLogin", []string{"prohibit-password"}}, {"AuthorizedKeysFile", []string{".ssh/authorized_keys"}}, {"Banner", []string{"/etc/issue net"}}},
			files:   map[string]string{sshd.MainConfig: stockMain, sshd.DropInDir + "/50-cloud-init.conf": cloudInit, ManageConf: manage, ListenConf: listen},
			ops:     []string{"write " + sshd.HardnDropIn, "remove " + ManageConf, "remove-socket-drop-in " + ListenConf}},
		{name: "kept-00-hardn", ssh: service, platform: debian13, configFile: ManageConf,
			files: map[string]string{sshd.MainConfig: stockMain, ManageConf: manage, sshd.HardnDropIn: "# Managed by hardn.\nPort 2230\nPermitRootLogin prohibit-password\n"},
			ops:   []string{"write " + sshd.HardnDropIn, "remove " + ManageConf}},
		{name: "v032-alpine", ssh: openrc, platform: alpine324,
			files: map[string]string{sshd.MainConfig: fixture(t, "v032-alpine-sshd_config")},
			ops:   []string{"restore " + sshd.MainConfig}},
		{name: "main-alpine", ssh: openrc, platform: alpine324,
			files: map[string]string{sshd.MainConfig: fixture(t, "main-alpine-sshd_config"), sshd.DropInDir + "/20-site.conf": "MaxAuthTries 4\n"},
			ops:   []string{"restore " + sshd.MainConfig}},
	}
}

// named is the scenario called name.
func named(t *testing.T, name string) scenario {
	t.Helper()
	for _, sc := range scenarios(t) {
		if sc.name == name {
			return sc
		}
	}
	t.Fatalf("no scenario %s", name)
	return scenario{}
}

// TestPlanForScenarios pins the steps and the files they write for each
// host shape (goldens in testdata/golden; -update rewrites them).
func TestPlanForScenarios(t *testing.T) {
	for _, sc := range scenarios(t) {
		t.Run(sc.name, func(t *testing.T) {
			h, p, err := plan(t, sc)
			if err != nil {
				t.Fatal(err)
			}
			if got := opKinds(p); !slices.Equal(got, sc.ops) {
				t.Errorf("ops = %q, want %q", got, sc.ops)
			}
			golden(t, sc.name+".00-hardn.conf", p.DropIn)
			for _, op := range p.Ops {
				if op.Note == "" {
					t.Errorf("%s %s has no note", op.Kind, op.Path)
				}
				if op.Kind == OpRestoreMain {
					golden(t, sc.name+".sshd_config", op.Data)
					if inc := sshd.ParseInclude(op.Data); inc.Mode != sshd.IncludeFirst {
						t.Errorf("restored sshd_config: Include mode %v, want the stock first line", inc.Mode)
					}
				}
			}
			if slices.ContainsFunc(h.Calls(), func(c hostfake.Call) bool { return !readOnly(c) }) {
				t.Errorf("Find or PlanFor changed the host: %v", h.Calls())
			}
		})
	}
}

// TestPlanForLines checks what becomes of the legacy lines: a line sshd
// reads first moves, one another file beats stays behind with why, and
// listen.conf's port is held.
func TestPlanForLines(t *testing.T) {
	sc := named(t, "v032-ubuntu")
	_, p, err := plan(t, sc)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(p.Dropped, func(c Carried) bool {
		return c.File == ManageConf && c.Text == "PasswordAuthentication no" && strings.Contains(c.Why, "50-cloud-init.conf line 1")
	}) {
		t.Errorf("manage.conf's PasswordAuthentication, which cloud-init's line beats, is not dropped with why: %+v", p.Dropped)
	}
	if !slices.ContainsFunc(p.Carried, func(c Carried) bool { return c.File == ManageConf && c.Text == "PermitRootLogin no" }) {
		t.Errorf("manage.conf's PermitRootLogin, which sshd reads first, is not carried: %+v", p.Carried)
	}
	if want := "[[::]:2230]"; strings.Join(fmtListeners(p.Hold), " ") != want {
		t.Errorf("Hold = %v, want %s", p.Hold, want)
	}
	for _, k := range []string{"port", "listenaddress", "passwordauthentication", "authorizedkeysfile", "permitrootlogin"} {
		if !slices.Contains(p.Keywords, k) {
			t.Errorf("Keywords %q lack %s", p.Keywords, k)
		}
	}
	if slices.ContainsFunc(p.Keywords, func(k string) bool { return strings.Contains(k, " ") || k != strings.ToLower(k) }) {
		t.Errorf("Keywords %q: want lower-case first words", p.Keywords)
	}
}

// TestPlanForListeners: the socket's listeners now are kept even when
// listen.conf never took effect, and a ListenAddress line another one
// already covers is dropped with why, so no two sockets overlap.
func TestPlanForListeners(t *testing.T) {
	_, p, err := plan(t, named(t, "v032-ubuntu-unreloaded"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(fmtListeners(p.Hold), " "), "[[::]:2230 [::]:22]"; got != want {
		t.Errorf("Hold = %s, want %s", got, want)
	}
	if !strings.Contains(p.Ops[0].Note, "ListenAddress [::]:22, ListenAddress 0.0.0.0:22 added") {
		t.Errorf("note %q does not name the socket's listeners it adds", p.Ops[0].Note)
	}
	_, p, err = plan(t, named(t, "upgraded-ubuntu"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(p.Dropped, func(c Carried) bool {
		return c.File == ManageConf && c.Text == "ListenAddress 0.0.0.0:2230" && strings.Contains(c.Why, "would not bind")
	}) {
		t.Errorf("manage.conf's ListenAddress 0.0.0.0:2230, which hardn.conf's Port 2230 and ListenAddress 0.0.0.0 configure too, is not dropped: %+v", p.Dropped)
	}
	if slices.ContainsFunc(p.Carried, func(c Carried) bool { return c.Text == "ListenAddress 0.0.0.0:2230" }) {
		t.Error("the dropped line is still listed as carried")
	}
}

// TestPlanForNeedsSocket: on a socket host whose generator follows the
// files, a plan that writes them needs ssh.socket's listeners.
func TestPlanForNeedsSocket(t *testing.T) {
	sc := named(t, "v032-ubuntu")
	h := newHost(t, sc.files)
	f, err := Find(context.Background(), h, FindRequest{ConfigFile: sc.configFile})
	if err != nil {
		t.Fatal(err)
	}
	before := h.before(t, service, "") // read as if sshd.service listened: the socket not read
	if _, err := PlanFor(Input{Found: f, SSH: sc.ssh, Platform: sc.platform, Before: before}); !errors.Is(err, ErrNotEstablished) {
		t.Errorf("PlanFor without the socket's listeners = %v, want ErrNotEstablished", err)
	}
	h.AddFile(sshd.DropInDir+"/20-site.conf", []byte("ListenAddress web-01.example.net\n"), 0o644, 0, 0)
	if f, err = Find(context.Background(), h, FindRequest{ConfigFile: sc.configFile}); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanFor(Input{Found: f, SSH: sc.ssh, Platform: sc.platform, Before: h.before(t, sc.ssh, sc.socket)}); !errors.Is(err, ErrNotEstablished) {
		t.Errorf("PlanFor with a host name in ListenAddress = %v, want ErrNotEstablished", err)
	}
}

// TestListenersOf pins the model of sshd's listeners the plan keeps and
// dedupes with.
func TestListenersOf(t *testing.T) {
	for _, tt := range []struct {
		conf string
		want string // "" with ok false
	}{
		{"", "[::]:22 0.0.0.0:22"},
		{"Port 2230\nPort 22\n", "[::]:2230 0.0.0.0:2230 [::]:22 0.0.0.0:22"},
		{"AddressFamily inet\nPort 2230\n", "0.0.0.0:2230"},
		{"AddressFamily inet6\n", "[::]:22"},
		{"Port 2230\nListenAddress 0.0.0.0\nListenAddress 0.0.0.0:2230\n", "0.0.0.0:2230 0.0.0.0:2230"},
		{"ListenAddress ::\nListenAddress [192.0.2.10]:2230\nPort 2222\n", "[::]:2222 192.0.2.10:2230"},
		{"ListenAddress [::1]:2230 rdomain r1\n", "[::1]:2230 rdomain r1"},
		{"ListenAddress web-01.example.net\n", ""},
		{"Port ssh\n", ""},
		{"AddressFamily inet\nAddressFamily inet6\n", ""},
		{"ListenAddress 0.0.0.0:0\n", ""},
	} {
		var ls []outLine
		for _, l := range sshd.ReadOrder([]byte(tt.conf), nil) {
			ls = append(ls, outLine{keyword: l.Keyword, args: l.Args})
		}
		got, ok := listenersOf(ls)
		var s []string
		for _, l := range got {
			s = append(s, l.String())
		}
		if strings.Join(s, " ") != tt.want || ok != (tt.want != "") {
			t.Errorf("listenersOf(%q) = %q, %v; want %q", tt.conf, s, ok, tt.want)
		}
	}
	if got := overlaps(nil); got != nil {
		t.Errorf("overlaps(nil) = %q", got)
	}
}

func fmtListeners(ls []sshd.Listener) []string {
	s := "["
	for i, l := range ls {
		if i > 0 {
			s += " "
		}
		s += l.String()
	}
	return []string{s + "]"}
}

// TestPlanForRules covers the cases where an artifact stays, or the plan is
// refused.
func TestPlanForRules(t *testing.T) {
	manage, listen := fixture(t, "v032-manage.conf"), fixture(t, "v032-listen.conf")
	alpineGen := fixture(t, "v032-alpine-sshd_config")
	unknown := detect.SSH{}
	tests := []struct {
		name     string
		files    map[string]string
		desired  []Directive
		ssh      detect.SSH
		platform detect.Platform
		ops      []string
		left     []string // path: a word of why
		err      error
	}{
		{name: "nothing to migrate", files: map[string]string{sshd.MainConfig: stockMain},
			desired: []Directive{{"Port", []string{"2230"}}}, ssh: socketGen, platform: ubuntu2604},
		{name: "Debian opted into the socket", files: map[string]string{sshd.MainConfig: stockMain, ListenConf: listen},
			ssh: socketNoGen, platform: debian13, left: []string{ListenConf + ": ssh.socket.d"}},
		{name: "SSH mode unknown", files: map[string]string{sshd.MainConfig: stockMain, ListenConf: listen},
			ssh: unknown, platform: ubuntu2604, left: []string{ListenConf + ": not established"}},
		{name: "generated main off Alpine", files: map[string]string{sshd.MainConfig: alpineGen, ListenConf: listen},
			ssh: socketGen, platform: ubuntu2604, left: []string{sshd.MainConfig + ": Alpine only", ListenConf + ": first-line Include"}},
		{name: "generated main given an Include", files: map[string]string{sshd.MainConfig: alpineGen + sshd.IncludeLine + "\n"},
			ssh: openrc, platform: alpine324, left: []string{sshd.MainConfig + ": has an Include"}},
		{name: "generated main with a Match block", files: map[string]string{sshd.MainConfig: alpineGen + "Match User bob\n  PasswordAuthentication yes\n"},
			ssh: openrc, platform: alpine324, left: []string{sshd.MainConfig + ": Match"}},
		{name: "legacy drop-in with a Match block", files: map[string]string{sshd.MainConfig: stockMain, ManageConf: manage + "Match User bob\n  PasswordAuthentication yes\n"},
			ssh: service, platform: debian13, left: []string{ManageConf + ": Match"}},
		{name: "Include not first", files: map[string]string{sshd.MainConfig: "Port 22\n" + sshd.IncludeLine + "\n", ManageConf: manage},
			ssh: service, platform: debian13, err: ErrIncludeOther},
		{name: "Include not first, inert listen.conf", files: map[string]string{sshd.MainConfig: "Port 22\n" + sshd.IncludeLine + "\n", ListenConf: listen},
			ssh: service, platform: debian13, ops: []string{"remove-socket-drop-in " + ListenConf}},
		{name: "no Include, a drop-in sshd does not read", files: map[string]string{sshd.MainConfig: "Port 22\n", HardnConf: fixture(t, "main-hardn-r.conf")},
			ssh: openrc, platform: alpine324, ops: []string{"remove " + HardnConf}},
		{name: "00-hardn.conf with a Match block", files: map[string]string{sshd.MainConfig: stockMain, ManageConf: manage, sshd.HardnDropIn: "Match User bob\n  PasswordAuthentication yes\n"},
			ssh: service, platform: debian13, err: fs.ErrInvalid},
		{name: "desired Match", files: map[string]string{sshd.MainConfig: stockMain, ManageConf: manage},
			desired: []Directive{{"Match", []string{"User", "bob"}}}, ssh: service, platform: debian13, err: fs.ErrInvalid},
		{name: "desired without an argument", files: map[string]string{sshd.MainConfig: stockMain},
			desired: []Directive{{"PermitRootLogin", nil}}, ssh: service, platform: debian13, err: fs.ErrInvalid},
		{name: "desired with a line break", files: map[string]string{sshd.MainConfig: stockMain},
			desired: []Directive{{"Banner", []string{"/etc/issue\nPort 22"}}}, ssh: service, platform: debian13, err: fs.ErrInvalid},
		{name: "desired keyword not a word", files: map[string]string{sshd.MainConfig: stockMain},
			desired: []Directive{{"Port=", []string{"22"}}}, ssh: service, platform: debian13, err: fs.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, p, err := plan(t, scenario{files: tt.files, desired: tt.desired, ssh: tt.ssh, platform: tt.platform})
			if !errors.Is(err, tt.err) || tt.err != nil && err == nil {
				t.Fatalf("PlanFor = %v, want %v", err, tt.err)
			}
			if err != nil {
				return
			}
			if got := opKinds(p); !slices.Equal(got, tt.ops) {
				t.Errorf("ops = %q, want %q", got, tt.ops)
			}
			if p.Empty() != (len(tt.ops) == 0) || p.Empty() && p.DropIn != nil {
				t.Errorf("Empty() = %v with DropIn %q", p.Empty(), p.DropIn)
			}
			if len(p.Left) != len(tt.left) {
				t.Fatalf("Left = %+v, want %q", p.Left, tt.left)
			}
			for i, l := range p.Left {
				path, word, _ := strings.Cut(tt.left[i], ": ")
				if l.Path != path || !strings.Contains(l.Why, word) {
					t.Errorf("Left[%d] = %+v, want %s and a why with %q", i, l, path, word)
				}
			}
		})
	}
}

func TestRender(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"publickey"}, "K publickey"},
		{[]string{".ssh/authorized_keys", ".ssh_%u/authorized_keys"}, "K .ssh/authorized_keys .ssh_%u/authorized_keys"},
		{[]string{"my keys/%u"}, `K "my keys/%u"`},
		{[]string{`a"b\c`}, `K "a\"b\\c"`},
		{[]string{"#x"}, `K "#x"`},
		{[]string{""}, `K ""`},
	} {
		got := render("K", tt.args)
		if got != tt.want {
			t.Errorf("render(%q) = %s, want %s", tt.args, got, tt.want)
		}
		// sshd splits it back into the same arguments.
		ls := sshd.ReadOrder([]byte(got), nil)
		if len(ls) != 1 || !slices.Equal(ls[0].Args, tt.args) {
			t.Errorf("%s reads back as %+v", got, ls)
		}
	}
}

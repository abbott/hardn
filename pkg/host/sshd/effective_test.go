package sshd_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sshd"
)

var (
	sshdT  = []string{"sshd", "-T", "-f", sshd.MainConfig}
	listen = []string{"systemctl", "show", "-p", "Listen", sshd.SocketUnit}
)

// fixture is a fake host holding testdata/effective/<name>/root, with sshd
// -T answered from its sshd-T.out and, when there is one, systemctl show -p
// Listen ssh.socket from its listen.out. Both outputs are written in the
// format of servconf.c's dump_config and systemctl-show.c, not captured from
// a host.
func fixture(t *testing.T, name string) (*hostfake.System, string) {
	t.Helper()
	h := hostfake.New(host.Options{})
	dir := filepath.Join("testdata", "effective", name)
	root := filepath.Join(dir, "root")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == root {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			h.AddDir("/"+filepath.ToSlash(rel), 0o755, 0, 0)
			return nil
		}
		data, err := os.ReadFile(p)
		h.AddFile("/"+filepath.ToSlash(rel), data, 0o644, 0, 0)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(filepath.Join(dir, "sshd-T.out"))
	if err != nil {
		t.Fatal(err)
	}
	h.Respond(hostfake.Response{Stdout: string(out)}, sshdT[0], sshdT[1:]...)
	if l, err := os.ReadFile(filepath.Join(dir, "listen.out")); err == nil {
		h.Respond(hostfake.Response{Stdout: string(l)}, listen[0], listen[1:]...)
	}
	return h, string(out)
}

// checkReadOnly fails on any call that changes the fake.
func checkReadOnly(t *testing.T, h *hostfake.System) {
	t.Helper()
	for _, c := range h.Calls() {
		switch c.Op {
		case "write-atomic", "write-in-place", "mkdir", "remove", "removeall", "rename", "chmod", "chown":
			t.Errorf("ReadEffective changed the host: %s", c)
		}
	}
}

func checkSet(t *testing.T, name string, got sshd.Set, want ...string) {
	t.Helper()
	if !got.Known || !slices.Equal(got.Values, want) {
		t.Errorf("%s = %+v, want Known %q", name, got, want)
	}
}

func drop(name string) string { return sshd.DropInDir + "/" + name }

func TestReadEffectiveSocketHost(t *testing.T) {
	h, _ := fixture(t, "ubuntu-socket")
	e, found, err := sshd.ReadEffective(context.Background(), h, sshd.EffectiveRequest{SSH: detect.SSH{Mode: detect.SSHSocket}})
	if err != nil {
		t.Fatalf("ReadEffective: %v", err)
	}
	if runs := h.Runs(); !slices.EqualFunc(runs, [][]string{sshdT, listen}, slices.Equal) {
		t.Errorf("runs = %q", runs)
	}
	checkReadOnly(t, h)
	known := func(v string) sshd.Setting { return sshd.Setting{Value: v, Known: true} }
	if e.PermitRootLogin != known("prohibit-password") || e.PasswordAuthentication != known("no") ||
		e.KbdInteractiveAuthentication != known("no") || e.PubkeyAuthentication != known("yes") {
		t.Errorf("settings = %+v %+v %+v %+v", e.PermitRootLogin, e.PasswordAuthentication, e.KbdInteractiveAuthentication, e.PubkeyAuthentication)
	}
	checkSet(t, "Port", e.Port, "2230")
	checkSet(t, "AllowUsers", e.AllowUsers, "alice")
	checkSet(t, "AcceptEnv", e.AcceptEnv, "LANG", "LC_*")
	checkSet(t, "DenyUsers", e.DenyUsers)
	if l := e.Listeners; !l.Known || len(l.SSHD) != 2 || len(l.Socket) != 2 || len(l.All) != 2 || !slices.Equal(l.Ports(), []uint16{2230}) {
		t.Errorf("Listeners = %+v", l)
	}
	want := []sshd.Conflict{{Kind: sshd.FindingShadowed, Keyword: "passwordauthentication",
		Hardn: sshd.Origin{File: sshd.HardnDropIn, Line: 4, Text: "PasswordAuthentication no"},
		Other: sshd.Origin{File: drop("50-cloud-init.conf"), Line: 1, Text: "PasswordAuthentication yes"}}}
	if !slices.Equal(e.Conflicts, want) {
		t.Errorf("Conflicts = %+v, want %+v", e.Conflicts, want)
	}
	if len(found) != 1 || found[0].Kind != sshd.FindingShadowed || found[0].File != drop("50-cloud-init.conf") || found[0].Line != 1 {
		t.Errorf("findings = %+v", found)
	}
}

// TestConflictByteOrder holds the plan's rule against a directory: sshd reads
// the drop-ins in C byte order (strcmp), so a file whose name is less than
// "00-hardn.conf" byte by byte beats it for a first-match keyword ('+' and
// '-' are below '0', '.' below every letter), and "000-x.conf", "00.conf"
// and "00_x.conf" do not. A name starting with '.' or not ending in ".conf"
// is not read at all.
func TestConflictByteOrder(t *testing.T) {
	h, _ := fixture(t, "byteorder")
	e, found, err := sshd.ReadEffective(context.Background(), h, sshd.EffectiveRequest{SSH: detect.SSH{Mode: detect.SSHService}})
	if err != nil {
		t.Fatalf("ReadEffective: %v", err)
	}
	type row struct {
		file    string
		kind    sshd.FindingKind
		keyword string
	}
	want := []row{
		{"+plus.conf", sshd.FindingOverridden, "x11forwarding"},
		{"0-x.conf", sshd.FindingOverridden, "passwordauthentication"},
		{"00-a.conf", sshd.FindingOverridden, "permitrootlogin"},
		{"00-g.conf", sshd.FindingOverridden, "kbdinteractiveauthentication"}, // ChallengeResponseAuthentication, an alias
		{"00-h.conf", sshd.FindingOverridden, "pubkeyauthentication"},
		{"00-hardn-x.conf", sshd.FindingOverridden, "maxauthtries"},
		{"00-hardn0.conf", sshd.FindingShadowed, "logingracetime"},
		{"00.conf", sshd.FindingShadowed, "clientaliveinterval"},
		{"000-x.conf", sshd.FindingShadowed, "allowtcpforwarding"},
		{"00_x.conf", sshd.FindingShadowed, "allowagentforwarding"},
		{"A-upper.conf", sshd.FindingShadowed, "maxsessions"},
		{"hardn.conf", sshd.FindingShadowed, "permitemptypasswords"},
	}
	var got []row
	for _, c := range e.Conflicts {
		got = append(got, row{filepath.Base(c.Other.File), c.Kind, c.Keyword})
	}
	if !slices.Equal(got, want) {
		t.Errorf("conflicts, in read order:\n got %v\nwant %v", got, want)
	}
	if len(found) != len(want) {
		t.Errorf("%d findings, want %d: %+v", len(found), len(want), found)
	}
	// sshd -T agrees with the winners, so nothing is Unknown.
	if e.PasswordAuthentication != (sshd.Setting{Value: "yes", Known: true}) || e.KbdInteractiveAuthentication != (sshd.Setting{Value: "yes", Known: true}) {
		t.Errorf("PasswordAuthentication = %+v, KbdInteractiveAuthentication = %+v", e.PasswordAuthentication, e.KbdInteractiveAuthentication)
	}
}

func TestReadEffectiveAccumulateMatch(t *testing.T) {
	h, out := fixture(t, "accumulate-match")
	main := sshd.Block{File: sshd.MainConfig, Line: 9, Text: "Match Address 192.0.2.0/24"}
	bobs := sshd.Block{File: drop("20-match.conf"), Line: 1, Text: "Match User bob"}
	def, bob := sshd.DefaultConn(), sshd.DefaultConn()
	bob.User = "bob"
	root := strings.Replace(out, "permitrootlogin prohibit-password", "permitrootlogin yes", 1)
	h.Respond(hostfake.Response{Stdout: root}, sshdT[0], append(sshdT[1:], "-C", def.Spec())...)
	h.Respond(hostfake.Response{Stdout: strings.Replace(root, "passwordauthentication no", "passwordauthentication yes", 1)}, sshdT[0], append(sshdT[1:], "-C", bob.Spec())...)

	e, found, err := sshd.ReadEffective(context.Background(), h, sshd.EffectiveRequest{SSH: detect.SSH{Mode: detect.SSHService}})
	if err != nil {
		t.Fatalf("ReadEffective: %v", err)
	}
	if want := [][]string{sshdT, append(slices.Clone(sshdT), "-C", def.Spec()), append(slices.Clone(sshdT), "-C", bob.Spec())}; !slices.EqualFunc(h.Runs(), want, slices.Equal) {
		t.Errorf("runs = %q, want %q", h.Runs(), want)
	}
	checkReadOnly(t, h)
	checkSet(t, "Port", e.Port, "22", "2230")
	checkSet(t, "AllowUsers", e.AllowUsers, "alice", "bob")
	if !e.Listeners.Known || !slices.Equal(e.Listeners.Ports(), []uint16{22, 2230}) || e.Listeners.Socket != nil {
		t.Errorf("Listeners = %+v", e.Listeners)
	}
	port22 := sshd.Origin{File: sshd.HardnDropIn, Line: 1, Text: "Port 22"}
	root2 := sshd.Origin{File: sshd.HardnDropIn, Line: 3, Text: "PermitRootLogin prohibit-password"}
	want := []sshd.Conflict{
		{Kind: sshd.FindingAccumulates, Keyword: "allowusers", Hardn: sshd.Origin{File: sshd.HardnDropIn, Line: 5, Text: "AllowUsers alice"}, Other: sshd.Origin{File: drop("10-legacy.conf"), Line: 1, Text: "AllowUsers bob"}},
		{Kind: sshd.FindingMatchOverride, Keyword: "passwordauthentication", Hardn: sshd.Origin{File: sshd.HardnDropIn, Line: 4, Text: "PasswordAuthentication no"},
			Other: sshd.Origin{File: drop("20-match.conf"), Line: 2, Text: "\tPasswordAuthentication yes"}, Match: bobs},
		// A Match block ends with its drop-in: 30-after.conf's line is global.
		{Kind: sshd.FindingShadowed, Keyword: "permitrootlogin", Hardn: root2, Other: sshd.Origin{File: drop("30-after.conf"), Line: 1, Text: "PermitRootLogin no"}},
		// sshd_config's settings after the Include are read after every drop-in.
		{Kind: sshd.FindingAccumulates, Keyword: "port", Hardn: port22, Other: sshd.Origin{File: sshd.MainConfig, Line: 5, Text: "Port 22"}},
		{Kind: sshd.FindingMatchOverride, Keyword: "permitrootlogin", Hardn: root2, Other: sshd.Origin{File: sshd.MainConfig, Line: 10, Text: "\tPermitRootLogin yes"}, Match: main},
	}
	if !slices.Equal(e.Conflicts, want) {
		t.Errorf("Conflicts:\n got %+v\nwant %+v", e.Conflicts, want)
	}
	wantOver := []sshd.Override{
		{Block: main, Conn: def, Keyword: "permitrootlogin", Value: "yes"},
		{Block: bobs, Conn: bob, Keyword: "permitrootlogin", Value: "yes"},
		{Block: bobs, Conn: bob, Keyword: "passwordauthentication", Value: "yes"},
	}
	if !slices.Equal(e.Overrides, wantOver) {
		t.Errorf("Overrides:\n got %+v\nwant %+v", e.Overrides, wantOver)
	}
	if len(found) != len(want) {
		t.Errorf("findings = %+v, want one per conflict", found)
	}
}

func TestReadEffectiveIncludeNone(t *testing.T) {
	h, _ := fixture(t, "include-none")
	block := sshd.Owner{File: sshd.MainConfig, First: 3, Last: 7} // # BEGIN hardn ... # END hardn
	e, found, err := sshd.ReadEffective(context.Background(), h, sshd.EffectiveRequest{SSH: detect.SSH{Mode: detect.SSHOpenRC}, Owner: block})
	if err != nil {
		t.Fatalf("ReadEffective: %v", err)
	}
	var kinds []sshd.FindingKind
	for _, f := range found {
		kinds = append(kinds, f.Kind)
	}
	// 00-a.conf is never read: there is no Include.
	if want := []sshd.FindingKind{sshd.FindingClobberable, sshd.FindingOverridden, sshd.FindingShadowed}; !slices.Equal(kinds, want) {
		t.Errorf("finding kinds = %v, want %v (%+v)", kinds, want, found)
	}
	if len(e.Conflicts) != 2 || e.Conflicts[0].Other.Line != 2 || e.Conflicts[1].Other.Line != 8 {
		t.Errorf("Conflicts = %+v", e.Conflicts)
	}
	if e.PasswordAuthentication != (sshd.Setting{Value: "yes", Known: true}) {
		t.Errorf("PasswordAuthentication = %+v, want the line before hardn's block", e.PasswordAuthentication)
	}
	h, _ = fixture(t, "include-none")
	if e, _, _ := sshd.ReadEffective(context.Background(), h, sshd.EffectiveRequest{SSH: detect.SSH{Mode: detect.SSHOpenRC}}); len(e.Conflicts) != 0 {
		t.Errorf("with the default Owner (00-hardn.conf, unread here): Conflicts = %+v", e.Conflicts)
	}
}

// base is a complete sshd -T output for Evaluate's table tests.
const base = "port 22\naddressfamily any\nlistenaddress [::]:22\nlistenaddress 0.0.0.0:22\npermitrootlogin prohibit-password\n" +
	"pubkeyauthentication yes\npasswordauthentication no\nkbdinteractiveauthentication no\nallowusers alice\n"

func TestEvaluateOutput(t *testing.T) {
	for _, tc := range []struct {
		name, out   string
		modeUnknown bool // detect.SSHUnknown; otherwise detect.SSHService
		check       func(sshd.Effective) bool
		unknown     int // FindingUnknown findings
	}{
		{name: "complete", out: base, check: func(e sshd.Effective) bool {
			return e.PermitRootLogin.Known && e.Port.Known && e.Listeners.Known && len(e.Keywords["listenaddress"]) == 2
		}},
		{name: "mixed case and without-password (OpenSSH before 10.1, 10.4 and later)",
			out: "Port 22\nListenAddress 0.0.0.0:22\nPermitRootLogin without-password\nPasswordAuthentication No\nKbdInteractiveAuthentication no\nPubkeyAuthentication yes\n",
			check: func(e sshd.Effective) bool {
				return e.PermitRootLogin == (sshd.Setting{Value: "prohibit-password", Known: true}) && e.PasswordAuthentication.Value == "no" && e.Keywords["permitrootlogin"] != nil
			}},
		{name: "an alias", out: strings.Replace(base, "kbdinteractiveauthentication no", "ChallengeResponseAuthentication no", 1),
			check: func(e sshd.Effective) bool {
				return e.KbdInteractiveAuthentication == (sshd.Setting{Value: "no", Known: true})
			}},
		{name: "a keyword missing", out: strings.Replace(base, "kbdinteractiveauthentication no\n", "", 1), unknown: 1,
			check: func(e sshd.Effective) bool {
				return !e.KbdInteractiveAuthentication.Known && e.PasswordAuthentication.Known
			}},
		{name: "printed twice", out: base + "passwordauthentication yes\n", unknown: 1,
			check: func(e sshd.Effective) bool { return e.PasswordAuthentication == sshd.Setting{} }},
		{name: "a value hardn does not know", out: strings.Replace(base, "prohibit-password", "UNKNOWN", 1), unknown: 1,
			check: func(e sshd.Effective) bool { return !e.PermitRootLogin.Known }},
		{name: "not keyword and value", out: base + "garbage\n", unknown: 1,
			check: func(e sshd.Effective) bool {
				return e.Keywords == nil && !e.PubkeyAuthentication.Known && !e.Listeners.Known
			}},
		{name: "nothing", out: "", unknown: 1, check: func(e sshd.Effective) bool { return !e.Port.Known }},
		{name: "a port that is not a number", out: base + "port ssh\n", unknown: 1,
			check: func(e sshd.Effective) bool { return !e.Port.Known && e.Port.Values == nil }},
		{name: "rdomain", out: strings.Replace(base, "listenaddress 0.0.0.0:22", "listenaddress 192.0.2.10:2230 rdomain 5", 1),
			check: func(e sshd.Effective) bool {
				return e.Listeners.Known && e.Listeners.SSHD[1].String() == "192.0.2.10:2230 rdomain 5" && slices.Equal(e.Listeners.Ports(), []uint16{22, 2230})
			}},
		{name: "a listenaddress that is not an address", out: base + "listenaddress web-01.example.net\n", unknown: 1,
			check: func(e sshd.Effective) bool { return !e.Listeners.Known }},
		{name: "SSH mode not established", out: base, modeUnknown: true, unknown: 1,
			check: func(e sshd.Effective) bool { return !e.Listeners.Known && len(e.Listeners.All) == 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mode := detect.SSHService
			if tc.modeUnknown {
				mode = detect.SSHUnknown
			}
			e, found := sshd.Evaluate(sshd.EffectiveInputs{Effective: []byte(tc.out), Mode: mode})
			if !tc.check(e) {
				t.Errorf("Effective = %+v", e)
			}
			if len(found) != tc.unknown || slices.ContainsFunc(found, func(f sshd.Finding) bool { return f.Kind != sshd.FindingUnknown }) {
				t.Errorf("findings = %+v, want %d effective-unknown", found, tc.unknown)
			}
		})
	}
}

func TestEvaluateSocket(t *testing.T) {
	for _, tc := range []struct {
		name, socket string
		known        bool
		kind         sshd.FindingKind
	}{
		{"one dual-stack socket", "Listen=[::]:22 (Stream)\n", true, 0},
		{"both families", "Listen=0.0.0.0:22 (Stream)\nListen=[::]:22 (Stream)\n", true, 0},
		{"another address", "Listen=192.0.2.10:22 (Stream)\n", false, sshd.FindingDisagree},
		{"a port change not yet applied", "Listen=[::]:2230 (Stream)\n", false, sshd.FindingDisagree},
		{"no listener", "", false, sshd.FindingUnknown},
		{"an empty property", "Listen=\n", false, sshd.FindingUnknown},
		{"a path", "Listen=/run/sshd.sock (Stream)\n", false, sshd.FindingUnknown},
		{"not a stream", "Listen=[::]:22 (Datagram)\n", false, sshd.FindingUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, found := sshd.Evaluate(sshd.EffectiveInputs{Effective: []byte(base), Mode: detect.SSHSocket, Socket: []byte(tc.socket)})
			if e.Listeners.Known != tc.known {
				t.Errorf("Listeners = %+v, want Known %v", e.Listeners, tc.known)
			}
			if tc.known != (len(found) == 0) || !tc.known && found[0].Kind != tc.kind {
				t.Errorf("findings = %+v, want %v", found, tc.kind)
			}
			if !slices.Contains(e.Listeners.Ports(), 22) {
				t.Errorf("All = %v: sshd's listeners are always kept", e.Listeners.All)
			}
		})
	}
}

// TestEvaluateDisagree: where sshd -T and the files disagree the value is
// Unknown, never one of the two.
func TestEvaluateDisagree(t *testing.T) {
	main := []byte(sshd.IncludeLine + "\n")
	for _, tc := range []struct {
		name, hardn, out string
		check            func(sshd.Effective) bool
	}{
		{"a setting", "PasswordAuthentication no\n", strings.Replace(base, "passwordauthentication no", "passwordauthentication yes", 1),
			func(e sshd.Effective) bool {
				return e.PasswordAuthentication == sshd.Setting{} && e.PermitRootLogin.Known
			}},
		{"a set", "AllowUsers alice\n", base + "allowusers bob\n",
			func(e sshd.Effective) bool { return !e.AllowUsers.Known && e.AllowUsers.Values == nil }},
		{"the port set", "Port 22\nPort 2230\n", base,
			func(e sshd.Effective) bool { return !e.Port.Known && e.Listeners.Known }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, found := sshd.Evaluate(sshd.EffectiveInputs{Effective: []byte(tc.out), Mode: detect.SSHService,
				Main: main, DropIns: []sshd.File{{Path: sshd.HardnDropIn, Data: []byte(tc.hardn)}}})
			if !tc.check(e) {
				t.Errorf("Effective = %+v", e)
			}
			if len(found) != 1 || found[0].Kind != sshd.FindingDisagree {
				t.Errorf("findings = %+v, want one sources-disagree", found)
			}
		})
	}
}

func TestReadEffectiveErrors(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name  string
		ctx   context.Context
		req   sshd.EffectiveRequest
		setup func(*hostfake.System)
		check func(error) bool
		runs  int
	}{
		{"cancelled", cancelled, sshd.EffectiveRequest{}, nil,
			func(err error) bool { return errors.Is(err, context.Canceled) }, 0},
		{"no privilege separation directory", context.Background(), sshd.EffectiveRequest{},
			func(h *hostfake.System) {
				h.Respond(hostfake.Response{Stderr: "Missing privilege separation directory: /run/sshd\r\n", ExitCode: 255}, sshdT[0], sshdT[1:]...)
			},
			func(err error) bool {
				var e *sshd.EnvError
				return errors.As(err, &e) && e.Kind == sshd.EnvPrivsepDir && errors.Is(err, sshd.ErrEnvironment)
			}, 1},
		{"systemctl fails", context.Background(), sshd.EffectiveRequest{SSH: detect.SSH{Mode: detect.SSHSocket}},
			func(h *hostfake.System) {
				h.Respond(hostfake.Response{Stdout: base}, sshdT[0], sshdT[1:]...)
				h.Respond(hostfake.Response{ExitCode: 1}, listen[0], listen[1:]...)
			},
			func(err error) bool { var x *host.ExitError; return errors.As(err, &x) && x.ExitCode == 1 }, 2},
		{"an Include the package does not follow", context.Background(), sshd.EffectiveRequest{},
			func(h *hostfake.System) {
				h.AddFile(drop("10-x.conf"), []byte("Include /etc/ssh/extra.conf\n"), 0o644, 0, 0)
			},
			func(err error) bool { return errors.Is(err, sshd.ErrUnsupportedInclude) }, 0},
		{"a test connection sshd cannot take", context.Background(), sshd.EffectiveRequest{Conn: sshd.Conn{User: "a,b", Host: "h", Addr: "192.0.2.1"}}, nil,
			func(err error) bool { return errors.Is(err, fs.ErrInvalid) }, 0},
		{"another sshd flavour", context.Background(), sshd.EffectiveRequest{Binary: "sshd.pam", SSH: detect.SSH{Mode: detect.SSHOpenRC}},
			func(h *hostfake.System) { h.Respond(hostfake.Response{Stdout: base}, "sshd.pam", sshdT[1:]...) },
			func(err error) bool { return err == nil }, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			h.AddFile(sshd.MainConfig, []byte(sshd.IncludeLine+"\n"), 0o644, 0, 0)
			h.AddFile(sshd.HardnDropIn, []byte("Port 22\n"), 0o644, 0, 0)
			if tc.setup != nil {
				tc.setup(h)
			}
			_, _, err := sshd.ReadEffective(tc.ctx, h, tc.req)
			if !tc.check(err) {
				t.Errorf("err = %v", err)
			}
			if n := len(h.Runs()); n != tc.runs {
				t.Errorf("%d runs, want %d: %q", n, tc.runs, h.Runs())
			}
			checkReadOnly(t, h)
		})
	}
}

// TestFindingCodes pins the finding codes: scripts and p3-output match on
// them, so they never change.
func TestFindingCodes(t *testing.T) {
	want := map[sshd.FindingKind]string{
		sshd.FindingClobberable: "clobberable", sshd.FindingIncludeNotFirst: "include-not-first",
		sshd.FindingMatchUntargeted: "match-untargeted", sshd.FindingWarning: "sshd-warning",
		sshd.FindingOverridden: "overridden", sshd.FindingShadowed: "shadowed", sshd.FindingAccumulates: "accumulates",
		sshd.FindingMatchOverride: "match-override", sshd.FindingDisagree: "sources-disagree", sshd.FindingUnknown: "effective-unknown",
	}
	for k, code := range want {
		if k.String() != code {
			t.Errorf("%d.String() = %q, want %q", k, k.String(), code)
		}
	}
	if s := sshd.FindingKind(0).String(); s != "unknown" {
		t.Errorf("the zero kind is %q", s)
	}
}

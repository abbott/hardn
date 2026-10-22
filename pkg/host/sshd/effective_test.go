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

// detect's facts for a host whose unit that listens is up.
var (
	socketUp  = detect.SSH{Mode: detect.SSHSocket, Socket: detect.UnitState{Enabled: "enabled", Active: "active"}}
	serviceUp = detect.SSH{Mode: detect.SSHService, Service: detect.UnitState{Enabled: "enabled", Active: "active"}}
	openRCUp  = detect.SSH{Mode: detect.SSHOpenRC, OpenRC: detect.OpenRCService{Started: detect.Yes}}
)

func checkRuns(t *testing.T, h *hostfake.System, want ...[]string) {
	t.Helper()
	if runs := h.Runs(); !slices.EqualFunc(runs, want, slices.Equal) {
		t.Errorf("runs = %q, want %q", runs, want)
	}
}

func TestReadEffectiveSocketHost(t *testing.T) {
	h, _ := fixture(t, "ubuntu-socket")
	e, found, err := sshd.ReadEffective(context.Background(), h, sshd.EffectiveRequest{SSH: socketUp})
	if err != nil {
		t.Fatalf("ReadEffective: %v", err)
	}
	checkRuns(t, h, sshdT, listen)
	checkReadOnly(t, h)
	known := func(v string) sshd.Setting { return sshd.Setting{Value: v, Known: true} }
	if e.PermitRootLogin != known("prohibit-password") || e.PasswordAuthentication != known("no") ||
		e.KbdInteractiveAuthentication != known("no") || e.PubkeyAuthentication != known("yes") || e.UsePAM != known("yes") || e.BuiltWithoutPAM {
		t.Errorf("settings = %+v %+v %+v %+v %+v (without PAM %v)", e.PermitRootLogin, e.PasswordAuthentication, e.KbdInteractiveAuthentication,
			e.PubkeyAuthentication, e.UsePAM, e.BuiltWithoutPAM)
	}
	checkSet(t, "Port", e.Port, "2230")
	checkSet(t, "AllowUsers", e.AllowUsers, "alice")
	checkSet(t, "AcceptEnv", e.AcceptEnv, "LANG", "LC_*")
	checkSet(t, "DenyUsers", e.DenyUsers)
	if !e.AllowUsers.Has("alice") || e.AllowUsers.Has("bob") {
		t.Errorf("AllowUsers.Has: %+v", e.AllowUsers)
	}
	if l := e.Listeners; !l.Known || !l.SocketRead || len(l.SSHD) != 2 || len(l.Socket) != 2 || len(l.All) != 2 || !slices.Equal(l.Ports(), []uint16{2230}) {
		t.Errorf("Listeners = %+v", l)
	}
	want := []sshd.Conflict{{Kind: sshd.FindingShadowed, Keyword: "passwordauthentication",
		Hardn: sshd.Origin{File: sshd.HardnDropIn, Line: 4, Text: "PasswordAuthentication no"},
		Other: sshd.Origin{File: drop("50-cloud-init.conf"), Line: 1, Text: "PasswordAuthentication yes"}}}
	if !slices.Equal(e.Conflicts, want) || !e.ConflictsKnown {
		t.Errorf("Conflicts = %+v (known %v), want %+v", e.Conflicts, e.ConflictsKnown, want)
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
// is not read at all: if they were, ".hidden.conf" would be the line that
// overrides PasswordAuthentication, and "00-hardn.conf.bak" (read right
// after hardn's) would shadow its MaxStartups.
func TestConflictByteOrder(t *testing.T) {
	h, _ := fixture(t, "byteorder")
	e, found, err := sshd.ReadEffective(context.Background(), h, sshd.EffectiveRequest{SSH: serviceUp})
	if err != nil {
		t.Fatalf("ReadEffective: %v", err)
	}
	checkRuns(t, h, sshdT)
	checkReadOnly(t, h)
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

	e, found, err := sshd.ReadEffective(context.Background(), h, sshd.EffectiveRequest{SSH: serviceUp})
	if err != nil {
		t.Fatalf("ReadEffective: %v", err)
	}
	checkRuns(t, h, sshdT, append(slices.Clone(sshdT), "-C", def.Spec()), append(slices.Clone(sshdT), "-C", bob.Spec()))
	checkReadOnly(t, h)
	checkSet(t, "Port", e.Port, "22", "2230")
	checkSet(t, "AllowUsers", e.AllowUsers, "alice", "bob")
	if !e.Listeners.Known || !slices.Equal(e.Listeners.Ports(), []uint16{22, 2230}) || e.Listeners.Socket != nil || e.Listeners.SocketRead {
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
		{Kind: sshd.FindingAccumulates, Keyword: "port", Hardn: port22, Other: sshd.Origin{File: sshd.MainConfig, Line: 5, Text: "Port 22"}, SameValue: true},
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
	e, found, err := sshd.ReadEffective(context.Background(), h, sshd.EffectiveRequest{SSH: openRCUp, Owner: block})
	if err != nil {
		t.Fatalf("ReadEffective: %v", err)
	}
	checkRuns(t, h, sshdT)
	checkReadOnly(t, h)
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
	// Alpine's sshd, built without PAM, prints no usepam line.
	if e.UsePAM != (sshd.Setting{Value: "no", Known: true}) || !e.BuiltWithoutPAM {
		t.Errorf("UsePAM = %+v (without PAM %v), want a build without PAM", e.UsePAM, e.BuiltWithoutPAM)
	}
	// With the default Owner (00-hardn.conf, unread here) hardn's lines are
	// not found: the report is not established, and a finding says so.
	h, _ = fixture(t, "include-none")
	e, found, _ = sshd.ReadEffective(context.Background(), h, sshd.EffectiveRequest{SSH: openRCUp})
	if len(e.Conflicts) != 0 || e.ConflictsKnown || len(found) != 2 || found[1].Kind != sshd.FindingUnknown || !strings.Contains(found[1].Detail, "set no keyword") {
		t.Errorf("default Owner: Conflicts = %+v (known %v), findings = %+v", e.Conflicts, e.ConflictsKnown, found)
	}
}

// base is a complete sshd -T output for Evaluate's table tests.
const base = "port 22\naddressfamily any\nlistenaddress [::]:22\nlistenaddress 0.0.0.0:22\npermitrootlogin prohibit-password\n" +
	"pubkeyauthentication yes\npasswordauthentication no\nkbdinteractiveauthentication no\nallowusers alice\n"

func TestEvaluateOutput(t *testing.T) {
	failed := serviceUp
	failed.Service.Active = "failed"
	stopped := openRCUp
	stopped.OpenRC.Started = detect.No
	for _, tc := range []struct {
		name, out string
		ssh       *detect.SSH // nil: serviceUp
		check     func(sshd.Effective) bool
		unknown   int // FindingUnknown findings
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
		{name: "a listenaddress that is not an address keeps the others", out: "listenaddress web-01.example.net\n" + base + "listenaddress 0.0.0.0:2230\n", unknown: 1,
			check: func(e sshd.Effective) bool {
				return !e.Listeners.Known && slices.Equal(e.Listeners.Ports(), []uint16{22, 2230})
			}},
		{name: "no listenaddress", out: strings.Replace(strings.Replace(base, "listenaddress [::]:22\n", "", 1), "listenaddress 0.0.0.0:22\n", "", 1), unknown: 1,
			check: func(e sshd.Effective) bool { return !e.Listeners.Known && e.Listeners.All == nil && e.Port.Known }},
		{name: "SSH mode not established", out: base, ssh: &detect.SSH{}, unknown: 1,
			check: func(e sshd.Effective) bool {
				return !e.Listeners.Known && len(e.Listeners.All) == 2 && !e.Listeners.SocketRead
			}},
		{name: "ssh.service failed", out: base, ssh: &failed, unknown: 1,
			check: func(e sshd.Effective) bool {
				return !e.Listeners.Known && len(e.Listeners.All) == 2 && e.PermitRootLogin.Known
			}},
		{name: "ssh.service's state not read", out: base, ssh: &detect.SSH{Mode: detect.SSHService}, unknown: 1,
			check: func(e sshd.Effective) bool { return !e.Listeners.Known }},
		{name: "sshd not started by OpenRC", out: base, ssh: &stopped, unknown: 1,
			check: func(e sshd.Effective) bool { return !e.Listeners.Known && len(e.Listeners.All) == 2 }},
		{name: "sshd started by OpenRC", out: base, ssh: &openRCUp,
			check: func(e sshd.Effective) bool { return e.Listeners.Known }},
		// UsePAM (next/p4-kbd-pam): dump_config prints usepam only in a build
		// with PAM, before the typed settings.
		{name: "no usepam line: a build without PAM", out: base,
			check: func(e sshd.Effective) bool {
				return e.UsePAM == (sshd.Setting{Value: "no", Known: true}) && e.BuiltWithoutPAM && e.Keywords["usepam"] == nil
			}},
		{name: "usepam no: a build with PAM, not using it", out: base + "usepam no\n",
			check: func(e sshd.Effective) bool {
				return e.UsePAM == (sshd.Setting{Value: "no", Known: true}) && !e.BuiltWithoutPAM
			}},
		{name: "usepam yes, mixed case (OpenSSH 10.4 and later)", out: base + "UsePAM Yes\n",
			check: func(e sshd.Effective) bool {
				return e.UsePAM == (sshd.Setting{Value: "yes", Known: true}) && !e.BuiltWithoutPAM
			}},
		{name: "usepam a value hardn does not know", out: base + "usepam maybe\n", unknown: 1,
			check: func(e sshd.Effective) bool { return e.UsePAM == sshd.Setting{} && !e.BuiltWithoutPAM }},
		{name: "usepam printed twice", out: base + "usepam yes\nusepam no\n", unknown: 1,
			check: func(e sshd.Effective) bool { return e.UsePAM == sshd.Setting{} && !e.BuiltWithoutPAM }},
		{name: "no usepam line, nor a typed setting: not sshd's whole answer", out: "port 22\nlistenaddress [::]:22\nlistenaddress 0.0.0.0:22\n", unknown: 5,
			check: func(e sshd.Effective) bool { return e.UsePAM == sshd.Setting{} && !e.BuiltWithoutPAM && e.Port.Known }},
		{name: "output not read: no build claimed", out: base + "garbage\n", unknown: 1,
			check: func(e sshd.Effective) bool { return e.UsePAM == sshd.Setting{} && !e.BuiltWithoutPAM }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ssh := serviceUp
			if tc.ssh != nil {
				ssh = *tc.ssh
			}
			e, found := sshd.Evaluate(sshd.EffectiveInputs{Effective: []byte(tc.out), SSH: ssh})
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
	failed := socketUp
	failed.Socket.Active = "failed"
	// Both units enabled: detect's Mode is Unknown (ErrConflict), and the
	// socket may hold the listeners.
	both := detect.SSH{Socket: socketUp.Socket, Service: serviceUp.Service}
	for _, tc := range []struct {
		name, out, socket string
		ssh               *detect.SSH // nil: socketUp
		known             bool
		kinds             []sshd.FindingKind
		ports             []uint16 // Ports() of All
	}{
		{name: "one dual-stack socket", socket: "Listen=[::]:22 (Stream)\n", known: true, ports: []uint16{22}},
		{name: "both families", socket: "Listen=0.0.0.0:22 (Stream)\nListen=[::]:22 (Stream)\n", known: true, ports: []uint16{22}},
		{name: "another address", socket: "Listen=192.0.2.10:22 (Stream)\n", kinds: []sshd.FindingKind{sshd.FindingDisagree}, ports: []uint16{22}},
		{name: "a port change not yet in the socket's configuration", socket: "Listen=[::]:2230 (Stream)\n",
			kinds: []sshd.FindingKind{sshd.FindingDisagree}, ports: []uint16{22, 2230}},
		{name: "no listener", kinds: []sshd.FindingKind{sshd.FindingUnknown}, ports: []uint16{22}},
		{name: "an empty property", socket: "Listen=\n", kinds: []sshd.FindingKind{sshd.FindingUnknown}, ports: []uint16{22}},
		{name: "a path", socket: "Listen=/run/sshd.sock (Stream)\n", kinds: []sshd.FindingKind{sshd.FindingUnknown}, ports: []uint16{22}},
		{name: "not a stream", socket: "Listen=[::]:22 (Datagram)\n", kinds: []sshd.FindingKind{sshd.FindingUnknown}, ports: []uint16{22}},
		{name: "ssh.socket failed", socket: "Listen=[::]:22 (Stream)\n", ssh: &failed, kinds: []sshd.FindingKind{sshd.FindingUnknown}, ports: []uint16{22}},
		{name: "no listenaddress: the socket's are kept", out: strings.Replace(strings.Replace(base, "listenaddress [::]:22\n", "", 1), "listenaddress 0.0.0.0:22\n", "", 1), socket: "Listen=[::]:2222 (Stream)\n",
			kinds: []sshd.FindingKind{sshd.FindingUnknown}, ports: []uint16{2222}},
		{name: "sshd -T unreadable: the socket's are kept", out: "garbage\n", socket: "Listen=[::]:2222 (Stream)\n",
			kinds: []sshd.FindingKind{sshd.FindingUnknown}, ports: []uint16{2222}},
		{name: "SSH mode Unknown, socket enabled: read, not compared", socket: "Listen=[::]:2222 (Stream)\n", ssh: &both,
			kinds: []sshd.FindingKind{sshd.FindingUnknown}, ports: []uint16{22, 2222}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ssh := socketUp
			if tc.ssh != nil {
				ssh = *tc.ssh
			}
			out := base
			if tc.out != "" {
				out = tc.out
			}
			e, found := sshd.Evaluate(sshd.EffectiveInputs{Effective: []byte(out), SSH: ssh, Socket: []byte(tc.socket)})
			if l := e.Listeners; l.Known != tc.known || !l.SocketRead || !slices.Equal(l.Ports(), tc.ports) {
				t.Errorf("Listeners = %+v (Ports %v), want Known %v, Ports %v", l, l.Ports(), tc.known, tc.ports)
			}
			var kinds []sshd.FindingKind
			for _, f := range found {
				kinds = append(kinds, f.Kind)
			}
			if !slices.Equal(kinds, tc.kinds) {
				t.Errorf("findings = %+v, want %v", found, tc.kinds)
			}
		})
	}
}

func TestSocketMayListen(t *testing.T) {
	off := detect.UnitState{Enabled: "disabled", Active: "inactive"}
	for _, tc := range []struct {
		name string
		ssh  detect.SSH
		want bool
	}{
		{"socket-activated", detect.SSH{Mode: detect.SSHSocket}, true},
		{"ssh.service", serviceUp, false},
		{"OpenRC", openRCUp, false},
		{"nothing detected (not systemd, or init unknown)", detect.SSH{}, false},
		{"Unknown, socket enabled", detect.SSH{Socket: socketUp.Socket, Service: serviceUp.Service}, true},
		{"Unknown, socket state partly read", detect.SSH{Socket: detect.UnitState{Active: "inactive"}}, true},
		{"Unknown, socket off", detect.SSH{Socket: off, Service: off}, false},
	} {
		if got := sshd.SocketMayListen(tc.ssh); got != tc.want {
			t.Errorf("%s: SocketMayListen = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestEvaluateDisagree: where sshd -T and the files disagree the value is
// Unknown, never one of the two.
func TestEvaluateDisagree(t *testing.T) {
	main := []byte(sshd.IncludeLine + "\n")
	for _, tc := range []struct {
		name, hardn, out string
		disagree         bool
		check            func(sshd.Effective) bool
	}{
		{"a setting", "PasswordAuthentication no\n", strings.Replace(base, "passwordauthentication no", "passwordauthentication yes", 1), true,
			func(e sshd.Effective) bool {
				return e.PasswordAuthentication == sshd.Setting{} && e.PermitRootLogin.Known
			}},
		{"a set", "AllowUsers alice\n", base + "allowusers bob\n", true,
			func(e sshd.Effective) bool { return !e.AllowUsers.Known && e.AllowUsers.Values == nil }},
		{"the port set, and so the listeners", "Port 22\nPort 2230\n", base, true,
			func(e sshd.Effective) bool { return !e.Port.Known && !e.Listeners.Known && len(e.Listeners.All) == 2 }},
		{"a port given as a service name is not predicted", "Port ssh\n", base, false,
			func(e sshd.Effective) bool { return e.Port.Known && e.Listeners.Known }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, found := sshd.Evaluate(sshd.EffectiveInputs{Effective: []byte(tc.out), SSH: serviceUp,
				Main: main, DropIns: []sshd.File{{Path: sshd.HardnDropIn, Data: []byte(tc.hardn)}}})
			if !tc.check(e) {
				t.Errorf("Effective = %+v", e)
			}
			if tc.disagree != (len(found) == 1) || tc.disagree && found[0].Kind != sshd.FindingDisagree || !tc.disagree && len(found) != 0 {
				t.Errorf("findings = %+v, want sources-disagree: %v", found, tc.disagree)
			}
		})
	}
}

// TestEvaluateConflictReport covers what the fixtures do not: hardn's own
// line under a Match block, an Owner that matches nothing, drop-ins given
// out of sshd's order, and SameValue.
func TestEvaluateConflictReport(t *testing.T) {
	main := []byte(sshd.IncludeLine + "\n")
	cloud := sshd.File{Path: drop("50-cloud-init.conf"), Data: []byte("PasswordAuthentication yes\n")}
	hardn := sshd.File{Path: sshd.HardnDropIn, Data: []byte("PasswordAuthentication no\n")}
	pwYes := strings.Replace(base, "passwordauthentication no", "passwordauthentication yes", 1)
	for _, tc := range []struct {
		name   string
		out    string
		main   []byte
		drops  []sshd.File
		owner  sshd.Owner
		known  bool
		kinds  []sshd.FindingKind
		others []string // Other.File of each Conflict
		same   []bool   // SameValue of each Conflict
	}{
		{name: "hardn's line under a Match block", out: base, main: []byte("Match User bob\n" + sshd.IncludeLine + "\n"), drops: []sshd.File{hardn},
			known: true, kinds: []sshd.FindingKind{sshd.FindingMatchOverride}},
		{name: "00-hardn.conf absent", out: pwYes, drops: []sshd.File{cloud},
			kinds: []sshd.FindingKind{sshd.FindingUnknown}},
		{name: "a reversed range", out: pwYes, drops: []sshd.File{hardn}, owner: sshd.Owner{File: sshd.HardnDropIn, First: 2, Last: 1},
			kinds: []sshd.FindingKind{sshd.FindingUnknown, sshd.FindingDisagree}},
		{name: "a range with one end 0", out: base, drops: []sshd.File{hardn}, owner: sshd.Owner{File: sshd.HardnDropIn, First: 1},
			kinds: []sshd.FindingKind{sshd.FindingUnknown}},
		{name: "a relative path", out: base, drops: []sshd.File{hardn}, owner: sshd.Owner{File: "etc/ssh/sshd_config.d/00-hardn.conf"},
			kinds: []sshd.FindingKind{sshd.FindingUnknown}},
		{name: "drop-ins out of order, and one the Include does not read", out: base,
			drops: []sshd.File{cloud, hardn, {Path: drop(".hidden.conf"), Data: []byte("PermitRootLogin yes\n")}, {Path: "/etc/ssh/extra.conf", Data: []byte("PubkeyAuthentication no\n")}},
			known: true, kinds: []sshd.FindingKind{sshd.FindingShadowed}, others: []string{cloud.Path}, same: []bool{false}},
		{name: "an earlier line with hardn's value", out: base,
			drops: []sshd.File{{Path: drop("0-x.conf"), Data: []byte("passwordauthentication NO\nPort 22\n")}, {Path: sshd.HardnDropIn, Data: []byte("PasswordAuthentication no\nPort 0022\n")}},
			known: true, kinds: []sshd.FindingKind{sshd.FindingOverridden, sshd.FindingAccumulates}, others: []string{drop("0-x.conf"), drop("0-x.conf")}, same: []bool{true, true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := main
			if tc.main != nil {
				m = tc.main
			}
			e, found := sshd.Evaluate(sshd.EffectiveInputs{Effective: []byte(tc.out), SSH: serviceUp, Main: m, DropIns: tc.drops, Owner: tc.owner})
			var kinds []sshd.FindingKind
			for _, f := range found {
				kinds = append(kinds, f.Kind)
			}
			if !slices.Equal(kinds, tc.kinds) || e.ConflictsKnown != tc.known {
				t.Errorf("findings = %+v, ConflictsKnown %v; want %v, %v", found, e.ConflictsKnown, tc.kinds, tc.known)
			}
			var others []string
			var same []bool
			for _, c := range e.Conflicts {
				others, same = append(others, c.Other.File), append(same, c.SameValue)
			}
			if !slices.Equal(others, tc.others) || !slices.Equal(same, tc.same) {
				t.Errorf("Conflicts = %+v, want Other %v, SameValue %v", e.Conflicts, tc.others, tc.same)
			}
		})
	}
}

// TestEvaluateProbes: a probe's output that is not sshd -T's is a finding
// at its block, and a set a Match block changes is an Override.
func TestEvaluateProbes(t *testing.T) {
	bob := sshd.DefaultConn()
	bob.User = "bob"
	block := sshd.Block{File: sshd.MainConfig, Line: 2, Text: "Match User bob"}
	e, found := sshd.Evaluate(sshd.EffectiveInputs{Effective: []byte(base), SSH: serviceUp, Probes: []sshd.Probe{
		{Block: block, Conn: bob, Output: []byte("garbage\n")},
		{Block: block, Conn: bob, Output: []byte(base + "allowusers bob\n")},
	}})
	if len(found) != 1 || found[0].Kind != sshd.FindingUnknown || found[0].File != sshd.MainConfig || found[0].Line != 2 {
		t.Errorf("findings = %+v", found)
	}
	if want := []sshd.Override{{Block: block, Conn: bob, Keyword: "allowusers", Value: "alice bob"}}; !slices.Equal(e.Overrides, want) {
		t.Errorf("Overrides = %+v, want %+v", e.Overrides, want)
	}
}

// TestReadEffectiveSocketSources: ReadEffective reads ssh.socket exactly
// when SocketMayListen, and a socket that is not active leaves the
// listeners Unknown with the socket's listeners still in All.
func TestReadEffectiveSocketSources(t *testing.T) {
	failed := socketUp
	failed.Socket.Active = "failed"
	off := detect.UnitState{Enabled: "disabled", Active: "inactive"}
	for _, tc := range []struct {
		name  string
		ssh   detect.SSH
		runs  [][]string
		ports []uint16
	}{
		{"ssh.socket failed", failed, [][]string{sshdT, listen}, []uint16{22, 2222}},
		{"SSH mode Unknown, both units enabled", detect.SSH{Socket: socketUp.Socket, Service: serviceUp.Service}, [][]string{sshdT, listen}, []uint16{22, 2222}},
		{"SSH mode Unknown, socket off", detect.SSH{Socket: off, Service: off}, [][]string{sshdT}, []uint16{22}},
		{"SSH mode Unknown, nothing detected", detect.SSH{}, [][]string{sshdT}, []uint16{22}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := fixture(t, "byteorder") // sshd -T: port 22; no Match block
			h.Respond(hostfake.Response{Stdout: "Listen=[::]:2222 (Stream)\n"}, listen[0], listen[1:]...)
			e, found, err := sshd.ReadEffective(context.Background(), h, sshd.EffectiveRequest{SSH: tc.ssh})
			if err != nil {
				t.Fatalf("ReadEffective: %v", err)
			}
			checkRuns(t, h, tc.runs...)
			checkReadOnly(t, h)
			if l := e.Listeners; l.Known || !slices.Equal(l.Ports(), tc.ports) {
				t.Errorf("Listeners = %+v (Ports %v), want Unknown, Ports %v", l, l.Ports(), tc.ports)
			}
			if !slices.ContainsFunc(found, func(f sshd.Finding) bool { return f.Kind == sshd.FindingUnknown }) {
				t.Errorf("findings = %+v, want an effective-unknown", found)
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
		{"an Owner that is not a range of lines", context.Background(), sshd.EffectiveRequest{Owner: sshd.Owner{File: sshd.MainConfig, First: 3}}, nil,
			func(err error) bool { return errors.Is(err, fs.ErrInvalid) }, 0},
		{"a Match probe sshd refuses", context.Background(), sshd.EffectiveRequest{SSH: serviceUp},
			func(h *hostfake.System) {
				h.AddFile(sshd.MainConfig, []byte(sshd.IncludeLine+"\nMatch User bob\n\tPasswordAuthentication yes\n"), 0o644, 0, 0)
				bob := sshd.DefaultConn()
				bob.User = "bob"
				h.Respond(hostfake.Response{Stdout: base}, sshdT[0], sshdT[1:]...)
				h.Respond(hostfake.Response{Stderr: sshd.MainConfig + " line 3: Bad yes/no argument: x\r\n", ExitCode: 255}, sshdT[0], append(sshdT[1:], "-C", bob.Spec())...)
			},
			func(err error) bool {
				var c *sshd.ConfigError
				return errors.As(err, &c) && errors.Is(err, sshd.ErrConfig)
			}, 2},
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

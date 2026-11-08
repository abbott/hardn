package revert

import (
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host/sshd"
)

// valid is a whole journal: armed, with every kind of step.
func valid() Journal {
	j := lockout()
	j.SchemaVersion, j.Created = SchemaVersion, testNow
	j.Timer = &Timer{Kind: TimerSystemd, Unit: UnitPrefix + testID, Argv: systemdRunArgv(testID), Deadline: j.Deadline(),
		Hardn: testHardn, HardnSHA256: strings.Repeat("ab", 32), Boot: testBootID}
	j.Backup = &Backups{Dir: "/var/backups/hardn/" + testID, Seqs: []int{1, 3}}
	j.Undo = []Step{
		{Op: OpRun, Argv: []string{"ufw", "delete", "allow", "2230/tcp"}},
		{Op: OpRun, Argv: []string{"/usr/sbin/ufw", "disable"}},
		{Op: OpChmod, Path: "/home/alice/.ssh", Mode: permT(0o755)},
		{Op: OpChown, Path: "/home/alice/.ssh", Owner: &Owner{UID: 1000, GID: -1}},
		{Op: OpRemoveDir, Path: "/home/bob/.ssh"},
		{Op: OpPlan, Unit: "sudoers", Plan: json.RawMessage(`{"tool":"sudo"}`)},
	}
	j.Reload = []Reload{{Service: ReloadSSH, Keywords: []string{"Port"}, After: []Listener{{Addr: netip.IPv4Unspecified(), Port: 22}}, IfUp: true,
		SocketDropIns: []string{listenConf}}}
	j.Applied = &Applied{At: testNow.Add(5 * time.Second), SSHD: []ProcStart{sshdOf(applySSHD)}}
	return j
}

func TestJournalCheck(t *testing.T) {
	if err := valid().Check(); err != nil {
		t.Fatalf("valid journal: %v", err)
	}
	binary := valid()
	binary.Reload = append(binary.Reload, Reload{Service: ReloadSSH, Binary: true, IfUp: true})
	if err := binary.Check(); err != nil {
		t.Fatalf("an openssh upgrade's reload, no keywords: %v", err)
	}
	tests := []struct {
		name   string
		change func(j *Journal)
	}{
		{"schema", func(j *Journal) { j.SchemaVersion = 2 }},
		{"id", func(j *Journal) { j.ID = "a/b" }},
		{"no created", func(j *Journal) { j.Created = time.Time{} }},
		{"timeout below the floor", func(j *Journal) { j.TimeoutSeconds = 59 }},
		{"risk", func(j *Journal) { j.Risk = "low" }},
		{"other with a timeout", func(j *Journal) { j.Risk = RiskOther }},
		{"other with a timer", func(j *Journal) { j.Risk, j.TimeoutSeconds = RiskOther, 0 }},
		{"lockout with no timer", func(j *Journal) { j.Timer = nil }},
		{"two-line summary", func(j *Journal) { j.Summary = "a\nb" }},
		{"bad session", func(j *Journal) { j.Session.Conn = "192.0.2.10" }},
		{"session not read", func(j *Journal) { j.Session = Session{} }},
		{"confirmVia port 0", func(j *Journal) { j.ConfirmVia = []Listener{{Addr: netip.IPv4Unspecified()}} }},
		{"confirmVia no address", func(j *Journal) { j.ConfirmVia = []Listener{{Port: 2230}} }},
		{"timer of another change", func(j *Journal) { j.Timer.Unit = UnitPrefix + otherID }},
		{"timer kind", func(j *Journal) { j.Timer.Kind = "at" }},
		{"timer no argv", func(j *Journal) { j.Timer.Argv = nil }},
		{"setsid pidfile relative", func(j *Journal) { j.Timer.Kind, j.Timer.Unit, j.Timer.PIDFile = TimerSetsid, "", "run/x.pid" }},
		{"timer hardn not in its argv", func(j *Journal) { j.Timer.Hardn = "/usr/sbin/hardn" }},
		{"timer hardn relative", func(j *Journal) { j.Timer.Hardn = "hardn" }},
		{"timer hardn with no sha256", func(j *Journal) { j.Timer.HardnSHA256 = "" }},
		{"timer sha256 with no hardn", func(j *Journal) { j.Timer.Hardn = "" }},
		{"timer sha256 upper case", func(j *Journal) { j.Timer.HardnSHA256 = strings.Repeat("AB", 32) }},
		{"timer sha256 short", func(j *Journal) { j.Timer.HardnSHA256 = "abab" }},
		{"timer boot with a newline", func(j *Journal) { j.Timer.Boot = testBootID + "\n" }},
		{"backup dir relative", func(j *Journal) { j.Backup.Dir = "backups" }},
		{"backup seqs not ascending", func(j *Journal) { j.Backup.Seqs = []int{3, 1} }},
		{"backup seq 0", func(j *Journal) { j.Backup.Seqs = []int{0} }},
		{"backup no seqs", func(j *Journal) { j.Backup.Seqs = nil }},
		{"run no argv", func(j *Journal) { j.Undo[0].Argv = nil }},
		{"run relative path", func(j *Journal) { j.Undo[0].Argv = []string{"bin/ufw"} }},
		{"run unclean path", func(j *Journal) { j.Undo[0].Argv = []string{"/usr/sbin/../sbin/ufw"} }},
		{"run with a path", func(j *Journal) { j.Undo[0].Path = "/etc" }},
		{"chmod without mode", func(j *Journal) { j.Undo[2].Mode = nil }},
		{"chmod of /", func(j *Journal) { j.Undo[2].Path = "/" }},
		{"chmod mode too wide", func(j *Journal) { j.Undo[2].Mode = permT(0o17777) }},
		{"chown with a mode", func(j *Journal) { j.Undo[3].Mode = permT(0o700) }},
		{"chown uid -2", func(j *Journal) { j.Undo[3].Owner.UID = -2 }},
		{"rmdir with argv", func(j *Journal) { j.Undo[4].Argv = []string{"rm"} }},
		{"unknown op", func(j *Journal) { j.Undo[4].Op = "rm-rf" }},
		{"reload service", func(j *Journal) { j.Reload[0].Service = "ufw" }},
		{"reload no keywords", func(j *Journal) { j.Reload[0].Keywords = nil }},
		{"reload binary with keywords", func(j *Journal) { j.Reload[0].Binary = true }},
		{"reload binary with after", func(j *Journal) {
			j.Reload[0] = Reload{Service: ReloadSSH, Binary: true, After: []Listener{{Addr: netip.IPv4Unspecified(), Port: 22}}}
		}},
		{"reload of a port with no after", func(j *Journal) { j.Reload[0].After = nil }},
		{"socket drop-in for a reload that moves no listener", func(j *Journal) { j.Reload[0].Keywords = []string{"PasswordAuthentication"} }},
		{"socket drop-in relative", func(j *Journal) { j.Reload[0].SocketDropIns = []string{"ssh.socket.d/listen.conf"} }},
		{"socket drop-in unclean", func(j *Journal) {
			j.Reload[0].SocketDropIns = []string{"/etc/systemd/system/ssh.socket.d/../listen.conf"}
		}},
		{"socket drop-in of another unit", func(j *Journal) {
			j.Reload[0].SocketDropIns = []string{"/etc/systemd/system/ssh.service.d/listen.conf"}
		}},
		{"socket drop-in not a .conf", func(j *Journal) { j.Reload[0].SocketDropIns = []string{"/etc/systemd/system/ssh.socket.d/listen"} }},
		{"socket drop-in twice", func(j *Journal) { j.Reload[0].SocketDropIns = []string{listenConf, listenConf} }},
		{"socket drop-in with no backups to restore it", func(j *Journal) { j.Backup = nil }},
		{"progress reason", func(j *Journal) { j.Rollback = &Progress{Started: testNow, Reason: "bored"} }},
		{"progress past the steps", func(j *Journal) { j.Rollback = &Progress{Started: testNow, Reason: ReasonTimeout, UndoDone: 7} }},
		{"progress runner pid -1", func(j *Journal) {
			j.Rollback = &Progress{Started: testNow, Reason: ReasonTimeout, Runner: ProcStart{PID: -1}}
		}},
		{"plan with no unit", func(j *Journal) { j.Undo[5].Unit = "" }},
		{"plan unit a path", func(j *Journal) { j.Undo[5].Unit = "../sudoers" }},
		{"plan not JSON", func(j *Journal) { j.Undo[5].Plan = json.RawMessage(`{`) }},
		{"plan empty", func(j *Journal) { j.Undo[5].Plan = nil }},
		{"plan with argv", func(j *Journal) { j.Undo[5].Argv = []string{"visudo"} }},
		{"run with a plan", func(j *Journal) { j.Undo[0].Plan = json.RawMessage(`{}`) }},
		{"chmod with a unit", func(j *Journal) { j.Undo[2].Unit = "sudoers" }},
		{"applied with no time", func(j *Journal) { j.Applied.At = time.Time{} }},
		{"applied sshd pid 0", func(j *Journal) { j.Applied.SSHD = []ProcStart{{}} }},
		{"applied, a change with no timer", func(j *Journal) { j.Risk, j.TimeoutSeconds, j.Timer = RiskOther, 0, nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := valid()
			tt.change(&j)
			if err := j.Check(); !errors.Is(err, ErrJournal) {
				t.Fatalf("Check = %v, want ErrJournal", err)
			}
		})
	}
}

func TestDecodeJournal(t *testing.T) {
	data, err := encode(valid())
	if err != nil {
		t.Fatal(err)
	}
	j, err := decodeJournal(data)
	if err != nil || j.ID != testID || len(j.Undo) != 6 || *j.Undo[2].Mode != 0o755 || !j.Reload[0].IfUp || j.Applied == nil || !reflect.DeepEqual(j.Applied, valid().Applied) ||
		!reflect.DeepEqual(j.Reload, valid().Reload) {
		t.Fatalf("round trip: %+v, %v", j, err)
	}
	var plan struct{ Tool string }
	if err := json.Unmarshal(j.Undo[5].Plan, &plan); err != nil || plan.Tool != "sudo" {
		t.Errorf("the plan round trip: %s, %v", j.Undo[5].Plan, err)
	}
	for name, bad := range map[string]string{
		"not JSON":       "{",
		"two documents":  string(data) + "{}",
		"unknown field":  `{"schemaVersion":1,"preRollback":["sh","-c","id"]}`,
		"empty document": `{}`,
	} {
		if _, err := decodeJournal([]byte(bad)); !errors.Is(err, ErrJournal) {
			t.Errorf("%s: %v, want ErrJournal", name, err)
		}
	}
}

// A journal as Phase 2 wrote it, without the timer's hardn, hardnSHA256
// and boot (testdata/pending-p2.json, the golden of next/p2-revert), still
// decodes: the fields are optional, and only the boot command needs them
// (it leaves such a journal alone).
func TestDecodePhase2Journal(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "pending-p2.json"))
	if err != nil {
		t.Fatal(err)
	}
	j, err := decodeJournal(data)
	if err != nil || j.Timer == nil || j.Timer.Hardn != "" || j.Timer.HardnSHA256 != "" || j.Timer.Boot != "" {
		t.Fatalf("decodeJournal = %+v, %v", j.Timer, err)
	}
	if !j.Deadline().Equal(j.Created.Add(j.Timeout())) {
		t.Errorf("deadline %v", j.Deadline())
	}
}

func TestDecodeOutcome(t *testing.T) {
	o, err := decodeOutcome([]byte(`{"schemaVersion":1,"id":"` + testID + `","summary":"s","later":"a field a later version adds"}`))
	if err != nil || o.ID != testID {
		t.Fatalf("decodeOutcome = %+v, %v; want lenient", o, err)
	}
	for _, bad := range []string{`{`, `{"schemaVersion":2,"id":"` + testID + `"}`, `{"schemaVersion":1,"id":"../x"}`} {
		if _, err := decodeOutcome([]byte(bad)); !errors.Is(err, ErrJournal) {
			t.Errorf("decodeOutcome(%s) = %v", bad, err)
		}
	}
}

func TestListenersOf(t *testing.T) {
	got := ListenersOf([]sshd.Listener{{Addr: netip.IPv6Unspecified(), Port: 2230, RDomain: "mgmt"}})
	if len(got) != 1 || got[0] != (Listener{Addr: netip.IPv6Unspecified(), Port: 2230, RDomain: "mgmt"}) || got[0].String() != "[::]:2230" {
		t.Errorf("ListenersOf = %+v", got)
	}
}

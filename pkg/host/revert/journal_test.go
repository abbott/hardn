package revert

import (
	"errors"
	"net/netip"
	"testing"
	"time"
)

// valid is a whole journal: armed, with every kind of step.
func valid() Journal {
	j := lockout()
	j.SchemaVersion, j.Created = SchemaVersion, testNow
	j.Timer = &Timer{Kind: TimerSystemd, Unit: UnitPrefix + testID, Argv: systemdRunArgv(testID), Deadline: j.Deadline()}
	j.Backup = &Backups{Dir: "/var/backups/hardn/" + testID, Seqs: []int{1, 3}}
	j.Undo = []Step{
		{Op: OpRun, Argv: []string{"ufw", "delete", "allow", "2230/tcp"}},
		{Op: OpRun, Argv: []string{"/usr/sbin/ufw", "disable"}},
		{Op: OpChmod, Path: "/home/alice/.ssh", Mode: permT(0o755)},
		{Op: OpChown, Path: "/home/alice/.ssh", Owner: &Owner{UID: 1000, GID: -1}},
		{Op: OpRemoveDir, Path: "/home/bob/.ssh"},
	}
	j.Reload = []Reload{{Service: ReloadSSH, Keywords: []string{"Port"}, After: []Listener{{Addr: netip.IPv4Unspecified(), Port: 22}}}}
	return j
}

func TestJournalCheck(t *testing.T) {
	if err := valid().Check(); err != nil {
		t.Fatalf("valid journal: %v", err)
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
		{"reload of a port with no after", func(j *Journal) { j.Reload[0].After = nil }},
		{"progress reason", func(j *Journal) { j.Rollback = &Progress{Started: testNow, Reason: "bored"} }},
		{"progress past the steps", func(j *Journal) { j.Rollback = &Progress{Started: testNow, Reason: ReasonTimeout, UndoDone: 6} }},
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
	if err != nil || j.ID != testID || len(j.Undo) != 5 || *j.Undo[2].Mode != 0o755 {
		t.Fatalf("round trip: %+v, %v", j, err)
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

package runall

import (
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	fwctl "github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
)

// TestNoSSHWords: a planned Enable reads "Enable" where SSH Login is Not
// Applicable for want of an SSH server, its plan letting no SSH through;
// "Enable, SSH allowed" otherwise, and a row not planned keeps its words.
func TestNoSSHWords(t *testing.T) {
	login := func(s engine.State, d string) engine.Report {
		return engine.Report{Control: ssh.Login{}.ID(), Status: engine.Status{State: s, Detail: d}}
	}
	for _, tc := range []struct {
		name  string
		login engine.Report
		kind  Kind
		want  string
	}{
		{"no SSH server", login(engine.NotApplicable, ssh.DetailNoServer), Planned, wordsEnableNoSSH},
		{"sshd unread", login(engine.Unknown, "no sshd_config"), Planned, wordsEnableSSH},
		{"another Not Applicable", login(engine.NotApplicable, "elsewhere"), Planned, wordsEnableSSH},
		{"not planned", login(engine.NotApplicable, ssh.DetailNoServer), Refused, wordsEnableSSH},
	} {
		r := Reading{Reports: []engine.Report{tc.login}, Rows: []Row{{ID: fwctl.ID, Kind: tc.kind, Words: wordsEnableSSH}}}
		r.noSSHWords()
		if got := r.Rows[0].Words; got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

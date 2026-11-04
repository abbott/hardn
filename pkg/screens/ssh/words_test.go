package ssh

import (
	"fmt"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/screens/screenkit"
)

// TestPlanWords: a change waiting for hardn confirm in the screen's words;
// an unfinished rollback, or a change that did not finish, in revert's,
// with its hardn rollback; a port in use the screen's.
func TestPlanWords(t *testing.T) {
	journal := func(risk revert.Risk, applied bool) *revert.Journal {
		j := &revert.Journal{ID: "c-0001", Summary: "SSH port 22 to 2230", Risk: risk, Rollback: &revert.Progress{}}
		if applied {
			j.Applied = &revert.Applied{}
		}
		return j
	}
	for _, tt := range []struct {
		name string
		st   revert.State
		want string // "" for revert's own, with its hardn rollback
	}{
		{"waiting for confirm", revert.State{Pending: journal(revert.RiskLockout, true)}, wordsPending},
		{"lockout-risk, not applied", revert.State{Pending: journal(revert.RiskLockout, false)}, ""},
		{"did not finish", revert.State{Pending: journal(revert.RiskOther, true)}, ""},
		{"rollback unfinished", revert.State{RollingBack: journal(revert.RiskLockout, true)}, ""},
	} {
		err := fmt.Errorf("engine: %w", &revert.PendingError{State: tt.st})
		got := planWords(err)
		if tt.want != "" && got != tt.want || tt.want == "" && (got != screenkit.RefusalWords(err) || !strings.Contains(got, "sudo hardn rollback c-0001")) {
			t.Errorf("%s: %q", tt.name, got)
		}
	}
	if got := planWords(fmt.Errorf("engine: %w", reload.ErrInUse)); got != wordsInUse {
		t.Errorf("in use: %q", got)
	}
}

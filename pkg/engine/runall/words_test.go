package runall

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/screens/screenkit"
)

// TestWords holds words to the screens' (screenkit.RefusalWords), which
// the engine cannot import: a refusal's why and fix, a bare why, a plain
// error; a change waiting for hardn confirm, the SSH Login screen's
// words; any other change journalled, revert's own, with its hardn rollback.
func TestWords(t *testing.T) {
	for _, err := range []error{
		&steps.Refusal{Why: "ufw isn't installed", Fix: "install it, then try again"},
		fmt.Errorf("wrapped: %w", &steps.Refusal{Why: "sudo isn't installed.", Fix: "Install sudo."}),
		&steps.Refusal{Why: "root stays key-only"},
		&steps.Refusal{Fix: "no why"},
		errors.New("engine: something  "),
	} {
		if got, want := words(err), screenkit.RefusalWords(err); got != want {
			t.Errorf("%v: %q, the screens' %q", err, got, want)
		}
	}
	for _, tt := range []struct {
		name string
		st   revert.State
		want string // "" for revert's own, with its hardn rollback
	}{
		{"waiting for confirm", revert.State{Pending: journal(revert.RiskLockout, true)}, PendingWords},
		{"lockout-risk, not applied", revert.State{Pending: journal(revert.RiskLockout, false)}, ""},
		{"did not finish", revert.State{Pending: journal(revert.RiskOther, true)}, ""},
		{"rollback unfinished", revert.State{RollingBack: journal(revert.RiskLockout, true)}, ""},
		{"rollback unfinished, a change waiting", revert.State{Pending: journal(revert.RiskLockout, true), RollingBack: journal(revert.RiskLockout, true)}, ""},
	} {
		err := fmt.Errorf("engine: %w", &revert.PendingError{State: tt.st})
		got := words(err)
		if tt.want != "" && got != tt.want || tt.want == "" && (got != screenkit.RefusalWords(err) || !strings.Contains(got, "sudo hardn rollback c-0001")) {
			t.Errorf("%s: %q", tt.name, got)
		}
	}
}

// journal is a journalled change of risk, applied or not.
func journal(risk revert.Risk, applied bool) *revert.Journal {
	j := &revert.Journal{ID: "c-0001", Summary: "SSH port 22 to 2230", Risk: risk, Rollback: &revert.Progress{}}
	if applied {
		j.Applied = &revert.Applied{}
	}
	return j
}

// TestKindString names each kind.
func TestKindString(t *testing.T) {
	for k := Planned; k <= Later; k++ {
		if s := k.String(); s == "" || s[0] == 'K' {
			t.Errorf("%d: %q", k, s)
		}
	}
	if Kind(0).String() != "Kind(0)" {
		t.Errorf("zero: %q", Kind(0).String())
	}
}

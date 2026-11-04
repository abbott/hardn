package runall

import (
	"errors"
	"fmt"
	"testing"

	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/screens/screenkit"
)

// TestWords holds words to the screens' (screenkit.RefusalWords), which
// the engine cannot import: a refusal's why and fix, a bare why, a plain
// error; and a change pending, the SSH Login screen's words.
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
	if got := words(&revert.PendingError{}); got != PendingWords {
		t.Errorf("pending: %q, want %q", got, PendingWords)
	}
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

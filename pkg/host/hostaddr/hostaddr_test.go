package hostaddr_test

import (
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/hostaddr"
)

// TestValid: lower-case letters, digits, dots and hyphens alone, so nothing
// that ends a quoted value or a word, and never an option.
func TestValid(t *testing.T) {
	for _, ok := range []string{"192.0.2.50", "logs.example.net", "a", "ntp-1", strings.Repeat("a", 253)} {
		if !hostaddr.Valid(ok) {
			t.Errorf("Valid(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "-R", "logs.", ".logs", "Logs", "a b", "a\"b", "a\nb", "a$b", "a;b", "a_b", "2001:db8::1", "a/b", "a\\b", strings.Repeat("a", 254)} {
		if hostaddr.Valid(bad) {
			t.Errorf("Valid(%q) = true", bad)
		}
	}
}

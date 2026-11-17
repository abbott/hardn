package output_test

import (
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/output"
)

// TestFormatLogin: a login from a host, one at a terminal alone, one in
// another year, none recorded, and nothing known.
func TestFormatLogin(t *testing.T) {
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	at := time.Date(2026, 11, 16, 22, 31, 5, 0, time.UTC)
	for _, c := range []struct {
		l    accounts.LastLogin
		want string
	}{
		{accounts.LastLogin{State: accounts.LoginAt, At: at, TTY: "pts/0", From: "192.0.2.10"}, "Nov 16 22:31 from 192.0.2.10"},
		{accounts.LastLogin{State: accounts.LoginAt, At: at, TTY: "pts/0"}, "Nov 16 22:31 on pts/0"},
		{accounts.LastLogin{State: accounts.LoginAt, At: at.AddDate(-1, 0, 0), TTY: "tty1"}, "Nov 16 2025 on tty1"},
		{accounts.LastLogin{State: accounts.LoginNone}, "None recorded"},
		{accounts.LastLogin{}, ""},
	} {
		if got := output.FormatLogin(c.l, now); got != c.want {
			t.Errorf("%+v: %q, want %q", c.l, got, c.want)
		}
	}
}

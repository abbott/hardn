package ssh_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/ui/nav"
)

// TestKeysItem: item 6, SSH keys, counts the keys sshd reads and opens the
// SSH Keys screen (pkg/screens/sshkeys) on the host, under plan only too; 0
// there returns to SSH Login and q quits; with sshd -T not read both say
// Can't read. It plans nothing, makes no engine and writes nothing.
func TestKeysItem(t *testing.T) {
	for _, tt := range []struct {
		host  string
		entry string
		keys  []string
	}{
		{"debian13", "3 keys, 2 to review", []string{"  Summary ---", "  root ---", "  george ---", "Shared with george", "Shared with root"}},
		{"rsa-small", "2 keys, 1 to review", []string{"  [ ] RSA 1024             george@laptop-1 Weak, refused by sshd\n", "   -  Refused by sshd      1 key\n"}},
		{"not-read", "Can't read", []string{"  [?] Keys                 Can't read\n"}},
	} {
		for _, dryRun := range []bool{false, true} {
			h := ready(t, tt.host)
			n := len(h.Calls())
			p := play(h, dryRun, "6", "0", "0")
			if p.err != nil || p.unread != 0 || p.engines != 0 || len(writes(h, n)) > 0 {
				t.Errorf("%s, plan only %v: Run = %v, %d unread, %d engines, wrote %v", tt.host, dryRun, p.err, p.unread, p.engines, writes(h, n))
			}
			login, keys, _ := strings.Cut(p.out, "# SSH Keys ###")
			keys, back, _ := strings.Cut(keys, "# SSH Login ###")
			if !strings.Contains(login, "   6) SSH keys             "+tt.entry+"\n") || !strings.Contains(back, "   6) SSH keys             "+tt.entry+"\n") {
				t.Errorf("%s: no item %q in:\n%s", tt.host, tt.entry, p.out)
			}
			for _, w := range append(tt.keys, "   0) Return", "[0 or q]:") {
				if !strings.Contains(keys, w) {
					t.Errorf("%s: no %q in:\n%s", tt.host, w, keys)
				}
			}
			if asked(p.out) {
				t.Errorf("%s: asked:\n%s", tt.host, p.out)
			}
		}
	}
	h := ready(t, "debian13")
	if p := play(h, false, "6", "q", "0"); !errors.Is(p.err, nav.ErrQuit) || p.unread != 1 {
		t.Errorf("q: Run = %v, %d unread", p.err, p.unread)
	}
}

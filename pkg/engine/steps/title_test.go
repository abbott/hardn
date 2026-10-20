package steps

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// The titles this package writes fit one step line (engine.TitleMax), so
// that the apply's progress never cuts them, while Summary keeps the path.
func TestTitles(t *testing.T) {
	d := func(k string, args ...string) migrate.Directive { return migrate.Directive{Keyword: k, Args: args} }
	allow := func(p uint16) ufw.Step { return ufw.Step{Kind: ufw.StepAllow, Port: p} }
	for _, tt := range []struct{ name, got, want string }{
		{"a port change adds its port; the held one is the clean-up's", addPortTitle(2230, 0), "Add SSH port 2230"},
		{"a port change with other settings", addPortTitle(2230, 2), "Add SSH port 2230 and 2 more"},
		{"root login, in the user's words", setTitle([]migrate.Directive{d("PermitRootLogin", "no")}, []string{"PermitRootLogin"}), "Disallow root login"},
		{"root by key only", setTitle([]migrate.Directive{d("PermitRootLogin", "prohibit-password")}, []string{"PermitRootLogin"}), "Disallow root password login"},
		{"key-only login: the two password prompts are one setting", setTitle([]migrate.Directive{d("PasswordAuthentication", "no"), d("KbdInteractiveAuthentication", "no")},
			[]string{"PasswordAuthentication", "KbdInteractiveAuthentication"}), "Disallow password login"},
		{"its check", checkTitle([]migrate.Directive{d("PasswordAuthentication", "no"), d("KbdInteractiveAuthentication", "no")}), "sshd -T shows no password login"},
		{"root's check", checkTitle([]migrate.Directive{d("PermitRootLogin", "prohibit-password")}), "sshd -T shows no root password login"},
		{"a setting with no words of its own keeps its directive", checkTitle([]migrate.Directive{d("AllowUsers", "alice")}), "sshd -T shows AllowUsers alice"},
		{"a value with no words of its own keeps its directive", setTitle([]migrate.Directive{d("PermitRootLogin", "forced-commands-only")}, []string{"PermitRootLogin"}),
			"Set PermitRootLogin forced-commands-only"},
		{"Port lines as one group", setTitle([]migrate.Directive{d("Port", "22"), d("Port", "2230")}, []string{"Port"}), "Set ports 22, 2230"},
		{"one Port line", setTitle([]migrate.Directive{d("Port", "2230")}, []string{"Port"}), "Set port 2230"},
		{"the check names both ports", "sshd -T shows " + directiveWords([]migrate.Directive{d("Port", "22"), d("Port", "2230")}), "sshd -T shows ports 22, 2230"},
		{"Port lines grouped where the first stands", setTitle([]migrate.Directive{d("Port", "22"), d("Port", "2230"), d("PermitRootLogin", "no")},
			[]string{"Port", "PermitRootLogin"}), "Set ports 22, 2230, PermitRootLogin no"},
		{"keyonly: the directives do not fit, nor the keywords",
			setTitle([]migrate.Directive{d("PubkeyAuthentication", "yes"), d("AuthenticationMethods", "publickey")},
				[]string{"PubkeyAuthentication", "AuthenticationMethods"}), "Set PubkeyAuthentication and 1 more"},
		{"the keywords fit where the directives do not",
			setTitle([]migrate.Directive{d("AllowUsers", "alice", "bob", "carol", "dave", "erin", "frank")}, []string{"AllowUsers"}), "Set AllowUsers"},
		{"ufw: allows and an enable", ufwTitle(ufw.Plan{Steps: []ufw.Step{allow(22), allow(2230), {Kind: ufw.StepEnable}}}), "ufw: allow 22/tcp, 2230/tcp; enable"},
		{"ufw: one allow", ufwTitle(ufw.Plan{Steps: []ufw.Step{allow(2230)}}), "ufw: allow 2230/tcp"},
		{"ufw: too many ports for one line", ufwTitle(ufw.Plan{Steps: []ufw.Step{allow(2201), allow(2202), allow(2203), allow(2204), allow(2205), allow(2206)}}), "ufw: 6 steps"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("title %q, want %q", tt.got, tt.want)
			}
			if n := utf8.RuneCountInString(tt.got); n > engine.TitleMax || strings.Contains(tt.got, "/etc/") {
				t.Errorf("title %q: %d cells (TitleMax %d), or a path", tt.got, n, engine.TitleMax)
			}
		})
	}
}

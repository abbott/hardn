package configscreen_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	configscreen "github.com/abbott/hardn/pkg/screens/config"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The Configuration group (group.go): main menu 8, which leads to hardn.yml, DNS
// Settings, NTP Settings and Syslog Settings, each cmd/hardn's screen.

// groupItems are the group's rows, plain at 80 columns.
const groupItems = "   1) hardn.yml            View or edit the file hardn reads\n" +
	"   2) DNS Settings         Configure nameservers\n" +
	"   3) NTP Settings         Keep the clock in sync\n" +
	"   4) Syslog Settings      Send logs to a syslog server\n"

// playGroup runs the group, plain at 80 columns, the user typing lines: each screen
// records its name and returns what returns has for it.
func playGroup(returns map[string]error, lines ...string) (played, []string) {
	var out bytes.Buffer
	var opened []string
	env := off()
	ty := uitest.NewTypist(&out, lines...)
	screen := func(name string) func() error {
		return func() error { opened = append(opened, name); return returns[name] }
	}
	err := configscreen.RunGroup(context.Background(), configscreen.GroupOptions{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		File:     screen("file"), DNS: screen("dns"), TimeSync: screen("timeSync"), LogForwarding: screen("logForwarding"),
	})
	return played{out.String(), err, ty.Unread()}, opened
}

// TestGroupKeys: 1 to 4 open hardn.yml, DNS Settings, NTP Settings and Syslog
// Settings, and the
// group is drawn again when each returns, by nil or by nav.ErrBack; 0 returns nil and
// q nav.ErrQuit, opening nothing; a key no item has is refused under the prompt, the
// group not drawn again; the end of the input aborts.
func TestGroupKeys(t *testing.T) {
	for _, tc := range []struct {
		name    string
		lines   []string
		returns map[string]error
		opened  []string
		drawn   int
		want    error
	}{
		{"1 opens hardn.yml", []string{"1", "0"}, nil, []string{"file"}, 2, nil},
		{"2 opens DNS Settings", []string{"2", "0"}, nil, []string{"dns"}, 2, nil},
		{"3 opens NTP Settings", []string{"3", "0"}, nil, []string{"timeSync"}, 2, nil},
		{"4 opens Syslog Settings", []string{"4", "0"}, nil, []string{"logForwarding"}, 2, nil},
		{"each in turn", []string{"1", "2", "3", "4", "0"}, nil, []string{"file", "dns", "timeSync", "logForwarding"}, 5, nil},
		{"a screen left by back", []string{"4", "3", "q"}, map[string]error{"logForwarding": nav.ErrBack}, []string{"logForwarding", "timeSync"}, 3, nav.ErrQuit},
		{"0 returns", []string{"0"}, nil, nil, 1, nil},
		{"q quits", []string{"q"}, nil, nil, 1, nav.ErrQuit},
		{"Q quits", []string{"Q"}, nil, nil, 1, nav.ErrQuit},
		{"keys no item has", []string{"5", "8", "x", "", "0"}, nil, nil, 1, nil},
		{"the end of the input", nil, nil, nil, 1, prompt.ErrAborted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, opened := playGroup(tc.returns, tc.lines...)
			if !errors.Is(p.err, tc.want) || (tc.want == nil && p.err != nil) || p.unread != 0 {
				t.Errorf("= %v, %d lines unread; want %v, none", p.err, p.unread, tc.want)
			}
			if !slices.Equal(opened, tc.opened) {
				t.Errorf("opened %q, want %q", opened, tc.opened)
			}
			if n := strings.Count(p.out, groupItems+"\n   0) Return\n"); n != tc.drawn {
				t.Errorf("the group drawn %d times, want %d:\n%s", n, tc.drawn, p.out)
			}
			if n, refused := strings.Count(p.out, nav.InvalidOption), map[string]int{"keys no item has": 4}[tc.name]; n != refused {
				t.Errorf("%d lines refused, want %d:\n%s", n, refused, p.out)
			}
			has(t, tc.name, p.out, "# Configuration ####", "    > Option [0-4 or q]: ")
		})
	}
}

// TestGroupEnds: a screen's q, its abort and any other error end the group with it,
// the group not drawn again and no other screen opened.
func TestGroupEnds(t *testing.T) {
	broken := errors.New("the screen failed")
	for _, end := range []error{nav.ErrQuit, prompt.ErrAborted, broken} {
		for key, name := range map[string]string{"1": "file", "2": "dns", "3": "timeSync", "4": "logForwarding"} {
			p, opened := playGroup(map[string]error{name: end}, key)
			if !errors.Is(p.err, end) || !slices.Equal(opened, []string{name}) || strings.Count(p.out, groupItems) != 1 {
				t.Errorf("%s ending with %v: = %v, opened %q:\n%s", name, end, p.err, opened, p.out)
			}
		}
	}
}

// TestGroupNeeds: without a Prompter's In and Out, or one of its screens, RunGroup
// writes nothing and returns an error wrapping fs.ErrInvalid.
func TestGroupNeeds(t *testing.T) {
	var out bytes.Buffer
	env := off()
	run := func() error { return nil }
	whole := configscreen.GroupOptions{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out), false), Out: env.Writer(&out)},
		File:     run, DNS: run, TimeSync: run, LogForwarding: run,
	}
	for name, strip := range map[string]func(*configscreen.GroupOptions){
		"no input":           func(o *configscreen.GroupOptions) { o.Prompter.In = nil },
		"no output":          func(o *configscreen.GroupOptions) { o.Prompter.Out = nil },
		"no hardn.yml":       func(o *configscreen.GroupOptions) { o.File = nil },
		"no Syslog Settings": func(o *configscreen.GroupOptions) { o.LogForwarding = nil },
		"no NTP Settings":    func(o *configscreen.GroupOptions) { o.TimeSync = nil },
		"no DNS Settings":    func(o *configscreen.GroupOptions) { o.DNS = nil },
	} {
		o := whole
		strip(&o)
		if err := configscreen.RunGroup(context.Background(), o); !errors.Is(err, fs.ErrInvalid) || out.Len() != 0 {
			t.Errorf("%s: = %v, wrote %q", name, err, out.String())
		}
	}
}

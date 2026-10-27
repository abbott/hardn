package screenkit_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The tests run one change on hostfake: a field asking for a nameserver,
// and a control whose plan is the save of hardn.yml's list alone
// (steps.PlanSave), as a screen's control plans its save. Synthetic values
// only: web-01.example.net, 192.0.2.0/24.

const hardnYML = "/etc/hardn/hardn.yml"

var now = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

var facts = detect.Facts{Platform: detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
	Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}, Container: detect.Container{Detected: detect.No}}

// newHost is a host whose hardn.yml lists 192.0.2.53, and hardn.yml as
// read from it.
func newHost(t *testing.T) (*hostfake.System, *config.Config) {
	t.Helper()
	h := hostfake.New(host.Options{})
	for _, d := range []string{"/etc", "/etc/hardn", "/var/lib", "/var/backups", "/run", "/var/log"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddFile(hardnYML, []byte("# hardn's configuration (web-01.example.net)\nusername: alice\nnameservers:\n  - \"192.0.2.53\"\n"), 0o600, 0, 0)
	cfg, err := config.Read(context.Background(), h, hardnYML)
	if err != nil {
		t.Fatal(err)
	}
	return h, cfg
}

// saveControl plans the save of hardn.yml's list from the configuration
// the engine plans with.
type saveControl struct{}

func (saveControl) ID() string                          { return "kit" }
func (saveControl) Title() string                       { return "Kit" }
func (saveControl) Applies(detect.Facts) (bool, string) { return true, "" }
func (saveControl) Check(context.Context, host.System, detect.Facts) (engine.Status, error) {
	return engine.Status{State: engine.NotApplicable}, nil
}
func (saveControl) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	return steps.PlanSave(ctx, sys, "kit/save", in.Config, []string{"nameservers"})
}

// run is one Session.Change on h with cfg: what it wrote, the runs Ran was
// given, how many engines it made and what Controls saw.
type run struct {
	out     string
	err     error
	runs    []output.Run
	engines int
	edited  []*config.Config
	unread  int
}

// change runs one change on h with cfg, the user typing lines; set, when
// not nil, changes the session first.
func change(h *hostfake.System, cfg *config.Config, dryRun bool, set func(*screenkit.Session), lines ...string) run {
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	ty := uitest.NewTypist(&out, lines...)
	var r run
	s := screenkit.Session{
		Name:     "kit screen",
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		System:   h, Config: cfg, DryRun: dryRun, LogPath: output.LegacyLogPath,
		Now: func() time.Time { return now },
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			r.engines++
			return engine.New(h, engine.Options{
				Revert:   revert.Options{Now: func() time.Time { return now }, Hardn: "/usr/local/bin/hardn"},
				Detect:   func(context.Context, host.System) (detect.Facts, error) { return facts, nil },
				Hold:     func() func() { return func() {} },
				PID:      5000,
				Backups:  backup.Settings{Path: backup.DefaultPath, Enabled: true},
				Progress: progress,
			})
		},
		Ran: func(run output.Run) { r.runs = append(r.runs, run) },
	}
	if set != nil {
		set(&s)
	}
	var entry string
	r.err = s.Change(context.Background(), screenkit.Change{
		Step: screen.WorkflowStep{Title: component.TitleBar{Title: "Add Nameserver"}},
		Flow: flow.Flow{
			Form:    form.Form{Fields: []form.Field{&form.Text{Title: "Enter nameserver IP address", Value: &entry}}},
			Review:  func() flow.Review { return flow.Review{Title: "DNS Configuration"} },
			Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: "Add nameserver " + entry + "?"} },
		},
		Controls: func(edited *config.Config) []engine.Control {
			r.edited = append(r.edited, edited)
			edited.Nameservers = append(slices.Clone(edited.Nameservers), entry)
			return []engine.Control{saveControl{}}
		},
	})
	r.out, r.unread = out.String(), ty.Unread()
	return r
}

// listOf is the list hardn.yml at its path holds now.
func listOf(t *testing.T, h *hostfake.System) []string {
	t.Helper()
	c, err := config.Read(context.Background(), h, hardnYML)
	if err != nil {
		t.Fatal(err)
	}
	return c.Nameservers
}

// TestChange: a change through the flow on the engine, for each way it
// ends: applied, its save adopted, so that the next change's save is not
// refused CONFIG_CHANGED; declined, a dry run and a quit at the question,
// planned, given to Ran, nothing written or adopted; back from the form's
// first field, nothing planned, no engine made, nothing given to Ran.
func TestChange(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dryRun  bool
		lines   []string
		err     error
		planned bool
		saved   bool
	}{
		{name: "applied", lines: []string{"192.0.2.54", "y"}, planned: true, saved: true},
		{name: "declined", lines: []string{"192.0.2.54", "n"}, planned: true},
		{name: "a dry run", dryRun: true, lines: []string{"192.0.2.54"}, planned: true},
		{name: "quit at the question", lines: []string{"192.0.2.54", "q"}, err: nav.ErrQuit, planned: true},
		{name: "back from the form", lines: []string{"0"}, err: nav.ErrBack},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, cfg := newHost(t)
			loaded := *cfg
			r := change(h, cfg, tc.dryRun, nil, tc.lines...)
			if !errors.Is(r.err, tc.err) || (tc.err == nil) != (r.err == nil) || r.unread != 0 {
				t.Fatalf("Change = %v, %d unread; want %v:\n%s", r.err, r.unread, tc.err, r.out)
			}
			want := 0
			if tc.planned {
				want = 1
			}
			if len(r.runs) != want || r.engines != want || len(r.edited) != want {
				t.Fatalf("%d runs, %d engines, Controls called %d times; want %d", len(r.runs), r.engines, len(r.edited), want)
			}
			if tc.planned && r.edited[0] == cfg {
				t.Error("Controls edited the session's configuration, not a copy")
			}
			list := []string{"192.0.2.53"}
			if tc.saved {
				list = append(list, "192.0.2.54")
			}
			if got := listOf(t, h); !slices.Equal(got, list) {
				t.Errorf("hardn.yml lists %q, want %q", got, list)
			}
			if !slices.Equal(cfg.Nameservers, list) || cfg.Username != loaded.Username {
				t.Errorf("the configuration lists %q (username %q), want %q, adopted only when saved", cfg.Nameservers, cfg.Username, list)
			}
			if !tc.saved {
				return
			}
			again := change(h, cfg, false, nil, "192.0.2.55", "y")
			if again.err != nil || len(again.runs) != 1 || again.runs[0].Err != nil {
				t.Fatalf("the next change: %v, runs %+v:\n%s", again.err, again.runs, again.out)
			}
			if got := listOf(t, h); !slices.Equal(got, append(list, "192.0.2.55")) {
				t.Errorf("after the next change hardn.yml lists %q", got)
			}
		})
	}
}

// TestApplyContext: an apply, and nothing else, runs under the context
// ApplyContext makes, stopped once the apply has returned.
func TestApplyContext(t *testing.T) {
	for _, tc := range []struct {
		name   string
		dryRun bool
		lines  []string
		made   int
	}{
		{"applied", false, []string{"192.0.2.54", "y"}, 1},
		{"declined", false, []string{"192.0.2.54", "n"}, 0},
		{"a dry run", true, []string{"192.0.2.54"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, cfg := newHost(t)
			made, stopped := 0, 0
			r := change(h, cfg, tc.dryRun, func(s *screenkit.Session) {
				s.ApplyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
					made++
					c, cancel := context.WithCancel(ctx)
					return c, func() { stopped++; cancel() }
				}
			}, tc.lines...)
			if r.err != nil || made != tc.made || stopped != made {
				t.Fatalf("Change = %v, %d contexts made, %d stopped; want %d:\n%s", r.err, made, stopped, tc.made, r.out)
			}
		})
	}
}

// TestAdopt: the configuration adopts a change's copy only when hardn.yml
// at its path holds the bytes the copy saved and not the ones it records
// itself; not when the file is as the configuration read it, when it
// cannot be read, or when the configuration was read from no file.
// (pkg/screens/dns's, moved with adopt.)
func TestAdopt(t *testing.T) {
	ctx := context.Background()
	list := []string{"192.0.2.53", "192.0.2.54"}
	for _, tt := range []struct {
		name  string
		save  bool
		setup func(h *hostfake.System)
		load  bool
		adopt bool
	}{
		{name: "saved", save: true, load: true, adopt: true},
		{name: "not saved", load: true},
		{name: "saved, then unreadable", save: true, load: true, setup: func(h *hostfake.System) {
			e, _ := h.Entry(hardnYML)
			h.AddFile("/etc/hardn/site.yml", e.Data, 0o600, 0, 0)
			h.AddSymlink(hardnYML, "/etc/hardn/site.yml") // ReadFileNoFollow refuses a link
		}},
		{name: "saved, then gone", save: true, load: true, setup: func(h *hostfake.System) { _ = h.Remove(ctx, hardnYML) }},
		{name: "no file read", load: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h, cfg := newHost(t)
			if !tt.load {
				cfg = config.DefaultConfig()
				cfg.Nameservers = []string{"192.0.2.53"}
			}
			edited := *cfg
			edited.Nameservers = list
			if tt.save {
				if err := config.Save(ctx, h, &edited, "nameservers"); err != nil {
					t.Fatal(err)
				}
			}
			if tt.setup != nil {
				tt.setup(h)
			}
			screenkit.Adopt(ctx, h, cfg, &edited)
			if got := slices.Equal(cfg.Nameservers, list); got != tt.adopt {
				t.Errorf("adopted %t (the configuration lists %q); want %t", got, cfg.Nameservers, tt.adopt)
			}
		})
	}
}

// TestNotLoaded: with no hardn.yml read, the save's refusal in its words;
// none with one read.
func TestNotLoaded(t *testing.T) {
	h, cfg := newHost(t)
	ctx := context.Background()
	if got := screenkit.NotLoaded(ctx, h, cfg, "kit/save", "nameservers"); got != nil {
		t.Errorf("hardn.yml read: %q", got)
	}
	want := []string{"No hardn.yml was read, so there is none to save to. Create /etc/hardn/hardn.yml, then try again."}
	if got := screenkit.NotLoaded(ctx, h, config.DefaultConfig(), "kit/save", "nameservers"); !slices.Equal(got, want) {
		t.Errorf("no hardn.yml read: %q, want %q", got, want)
	}
}

// TestRefusalWords: a refusal's Why then its Fix, each a sentence, the
// Fix's first letter a capital; its Why alone with no Fix; an error that
// is no refusal as its text.
func TestRefusalWords(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want string
	}{
		{&steps.Refusal{Code: "DNS_MANAGED", Why: "NetworkManager manages DNS here", Fix: "set the nameservers with nmcli"},
			"NetworkManager manages DNS here. Set the nameservers with nmcli."},
		{&steps.Refusal{Why: "resolvconf manages DNS here.", Fix: "édit it by hand."}, "resolvconf manages DNS here. Édit it by hand."},
		{&steps.Refusal{Why: "the list is empty"}, "the list is empty."},
		{fmt.Errorf("plan: %w", &steps.Refusal{Why: "wrapped", Fix: "unwrap it"}), "wrapped. Unwrap it."},
		{errors.New("lstat /etc/resolv.conf: permission denied"), "lstat /etc/resolv.conf: permission denied."},
	} {
		if got := screenkit.RefusalWords(tt.err); got != tt.want {
			t.Errorf("RefusalWords(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
	for in, want := range map[string]string{"done": "done.", "done.": "done.", " done. ": "done.", "": "."} {
		if got := screenkit.FullStop(in); got != want {
			t.Errorf("FullStop(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSay: Say writes the messages set apart as an action's result, one
// warning callout for Warnings; a write that fails names the screen.
func TestSay(t *testing.T) {
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 80})
	c := component.NewCtx(env)
	s := screenkit.Session{Name: "kit screen", Prompter: nav.Prompter{Ctx: c, Out: &out}}
	msgs := screenkit.Warnings("Please add nameservers first.", "Then try again.")
	if len(msgs) != 2 || msgs[0].Kind != component.MessageWarn || msgs[1].Kind != component.MessageWarn {
		t.Fatalf("Warnings = %+v", msgs)
	}
	if err := s.Say(msgs)(); err != nil {
		t.Fatal(err)
	}
	if want := "\n" + msgs.Render(c) + "\n"; out.String() != want || screenkit.Apart(c, msgs) != want {
		t.Errorf("Say wrote %q, want %q", out.String(), want)
	}
	s.Prompter.Out = failing{}
	if err := s.Say(msgs)(); err == nil || !strings.HasPrefix(err.Error(), "kit screen: write: ") || !errors.Is(err, errBroken) {
		t.Errorf("a failed write: %v", err)
	}
}

var errBroken = errors.New("broken pipe")

type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errBroken }

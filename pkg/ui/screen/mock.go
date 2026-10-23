package screen

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// Mock is one screen mock of the dev-only gallery (brief 1D): a screen
// drawn by its real layout from fixture data. A menu screen is one
// drawing. A workflow is shown as sequences, each a run of the canonical
// flow on a fixture plan, and each sequence as its steps.
type Mock struct {
	// Name is "<layout>/<screen>", the screen named after its Figma PNG,
	// in lower case with "_" as "-": "sub-menu/01-user" for 01-USER.png.
	Name string
	// Source says where the screen's design comes from.
	Source Source
	// Screen is a menu screen's layout, a MainMenu or a SubMenu; nil for a
	// workflow.
	Screen MenuScreen
	// Refusals are what a menu screen's items say, by key, in place of an
	// action that hardn refuses before it asks or plans anything, as the
	// DNS mocks refused every item on a host whose resolver hardn only
	// reports (P-37 (a)) until the real DNS screen replaced them
	// (next/s2-screen); nil for none. The playable mode writes the
	// Message, then the menu again under it.
	Refusals map[string]component.Message
	// Sequences are a workflow's paths, such as the normal path, the
	// dry-run path, which stops at the plan, and the failure path, which
	// shows what was rolled back.
	Sequences []Sequence
}

// MenuScreen is a menu screen's layout, a MainMenu or a SubMenu: Render
// draws it, and Screen returns what nav.Run draws for it, its menu's
// options bound to their actions.
type MenuScreen interface {
	Render(c component.Ctx) string
	Screen(c component.Ctx, m nav.Menu) nav.Screen
}

// Source says where a mock's design comes from (refactor plan, the
// next/p1d-gallery-screens row).
type Source struct {
	// Label is "Figma <file>" for a screen after a Figma frame; for one
	// with no frame, "inferred from origin/main", "inferred from v0.3.2",
	// or "inferred from origin/main (unreleased)" for a screen v0.3.2
	// never had (refactor plan §3.25, pending).
	Label string
	// Delta says how origin/main's screen differs from v0.3.2's.
	Delta string
	// Pending says where the mock departs from its source until a
	// proposal is decided, beyond the layouts' own departures (DEV-053,
	// DEV-054); empty for nowhere.
	Pending string
}

// Sequence is one path through a workflow: its screen and flow, and the
// lines the user types.
type Sequence struct {
	// Name is the path, such as "normal", "dry-run" or "failure".
	Name string
	// Build returns the screen and a fresh flow, whose form binds its
	// answers to variables of its own.
	Build func() (WorkflowStep, flow.Flow)
	// Answers are the lines typed, in order.
	Answers []Answer
	// Ends is how the flow ends, which names the last step.
	Ends flow.Outcome
}

// Answer is a line the user types, and the name of the step it ends,
// such as "confirm".
type Answer struct{ Step, Line string }

// Mocks returns the gallery's screen mocks, by layout, then by name: each
// Figma frame that fits a layout. SSH_LOGIN-SSH_SETTINGS_DISPLAY fits none
// (P-28), nor does origin/main's Firewall screen, of which the Figma
// export holds a screenshot (P-30). origin/main's DNS screen, mocked on
// eight hosts by next/s2-gallery, is retired from them: the real screen
// (DNS, drawn from the host by pkg/screens/dns) replaced it, and the
// gallery plays it on the same hosts (docs/engine/gallery-dns.md §11).
// The workflows' sequences run on fixture plans (flow.Fixture).
func Mocks() []Mock {
	out := frameMocks()
	slices.SortStableFunc(out, func(a, b Mock) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// frameMocks are the mocks of Figma's frames, and of the main menu.
func frameMocks() []Mock {
	figma := func(png, delta, pending string) Source {
		return Source{Label: "Figma " + png + ".png", Delta: delta, Pending: pending}
	}
	confirm := func(line string) []Answer { return []Answer{{Step: "confirm", Line: line}} }
	on := func(build func(flow.Engine, bool) (WorkflowStep, flow.Flow), e flow.Engine, dryRun bool) func() (WorkflowStep, flow.Flow) {
		return func() (WorkflowStep, flow.Flow) { return build(e, dryRun) }
	}
	creating := func(failing, dryRun bool) func() (WorkflowStep, flow.Flow) {
		return func() (WorkflowStep, flow.Flow) { return createUser(failing, dryRun) }
	}
	// george are Figma's answers to the create-user form, then the
	// confirmation's, if any.
	george := func(confirmed ...string) []Answer {
		out := []Answer{{"username", "george"}, {"sudo", "y"}, {"sudo-password", "n"}, {"ssh-key", "n"}}
		for _, l := range confirmed {
			out = append(out, Answer{"confirm", l})
		}
		return out
	}
	const (
		unreleased = "v0.3.2 has no such screen; origin/main adds it (pkg/menu/user_options.go)."
		figmaWords = "Figma's words where origin/main's differ, pending P-31."
	)
	return []Mock{
		{Name: "main-menu/00-tui-main", Screen: MainMenuFixture(), Source: figma("00-TUI-MAIN",
			"v0.3.2 (pkg/menu/menu.go:72-265) opens with an ASCII \"Hardn Linux\" logo under a ### rule, no version, "+
				"where the header stands (pkg/utils/utils.go:35-47), and has no host badge or divider: the OS name "+
				"sits in a ~~~ rule over the risk, the status rows and a dry-run line, then twelve items, the "+
				"package screens and Help among them, and \"Enter your choice [0-12 or q]\". origin/main has this "+
				"layout and ten items: the package screens and Help gone, User Management, SSH Login, Environment "+
				"and System Details new.",
			"Item 2 and the prompt say origin/main's words, pending P-07 (a), P-12 and P-29 (a).")},
		{Name: "sub-menu/01-user", Screen: UserFixture(), Source: figma("01-USER",
			"v0.3.2 has no User Management menu: its item 1, Sudo User, creates the configured user at once, "+
				"asking only for a username when none is configured, with no confirmation (pkg/menu/menu.go:317-349). "+
				"origin/main adds this menu.",
			"Figma's words where origin/main's differ, pending P-29 (b).")},
		{Name: "sub-menu/02-ssh-login", Screen: sshLoginFixture(), Source: figma("02-SSH_LOGIN",
			"v0.3.2's Root SSH item warns and asks \"Do you want to continue? (y/n)\", then restarts ssh "+
				"(pkg/menu/menu.go:352-381). origin/main adds the root-login state, a checklist and two items, "+
				"to disable root SSH access and to view the SSH configuration (pkg/menu/disable_root_menu.go).",
			figmaWords+" Item 1's description, pending P-12.")},
		{Name: "sub-menu/05-backup", Screen: backupFixture(), Source: figma("05-BACKUP",
			"v0.3.2's Backup Settings prints the two settings and two items, to toggle backups and change the path "+
				"(pkg/menu/menu.go:710-770). origin/main adds a status block and a third item, to verify the "+
				"directory (pkg/menu/backup_menu.go).",
			figmaWords)},
		{Name: "sub-menu/user-manage-user-management", Screen: manageUserFixture(),
			Source: figma("USER-MANAGE_USER-MANAGEMENT", unreleased, "The prompt, Figma's, pending P-29 (a).")},
		{Name: "sub-menu/user-manage-user-select-user", Screen: selectUserFixture(),
			Source: figma("USER-MANAGE_USER-SELECT_USER", unreleased, "The prompt, Figma's, pending P-29 (a).")},
		{Name: "sub-menu/user-ssh-keys", Screen: sshKeysFixture(), Source: figma("USER-SSH_KEYS", unreleased, figmaWords)},
		{Name: "workflow-step/ssh-login-toggle-confirmation", Source: figma("SSH_LOGIN-TOGGLE_CONFIRMATION",
			"v0.3.2 warns, asks \"Do you want to continue? (y/n)\" and restarts ssh (pkg/menu/menu.go:352-381). "+
				"origin/main asks \"Are you sure you want to disable root SSH access? (y/n)\" under the SSH Login "+
				"screen's state and checklist (pkg/menu/disable_root_menu.go).",
			"Confirmed by typing the hostname, as brief 1C asks for a lockout-risk change, where Figma asks (y/n): "+
				"pending P-10 (DEV-057). "+figmaWords),
			Sequences: []Sequence{
				{Name: "normal", Build: on(rootLogin, flow.Fixture{Planned: rootLoginPlan()}, false), Answers: confirm(hostname), Ends: flow.Applied},
				{Name: "dry-run", Build: on(rootLogin, flow.Fixture{Planned: rootLoginPlan()}, true), Ends: flow.Planned},
				{Name: "failure", Build: on(rootLogin, rootLoginFails(), false), Answers: confirm(hostname), Ends: flow.Failed},
			}},
		{Name: "workflow-step/user-create-user", Source: figma("USER-CREATE_USER",
			"v0.3.2's User Creation Menu asks for a username only when none is configured, then creates the user "+
				"at once (pkg/menu/menu.go:317-349). origin/main adds this form, a summary and \"Create user 'x'? (y/n)\".",
			"The confirmation after the plan, unnumbered, pending P-10 (DEV-048). \"Require sudo password?\" "+
				"asked y/N, as Figma asks it, pending P-11. "+figmaWords),
			Sequences: []Sequence{
				{Name: "normal", Build: creating(false, false), Answers: george("y"), Ends: flow.Applied},
				{Name: "dry-run", Build: creating(false, true), Answers: george(), Ends: flow.Planned},
				{Name: "failure", Build: creating(true, false), Answers: george("y"), Ends: flow.Failed},
				{Name: "cancelled", Build: creating(false, false), Answers: george("n"), Ends: flow.Cancelled},
			}},
		{Name: "workflow-step/user-manage-sudo-confirmation", Source: figma("USER-MANAGE_SUDO-CONFIRMATION", unreleased+
			" It asks \"Require password for sudo? (y/n)\" only when moving to a password, and changes the other "+
			"way with no confirmation.",
			"A y/N question, as Figma asks; the typed sequence confirms by typing the hostname, as P-10 proposes "+
				"for sudo-method changes, pending P-10. "+figmaWords),
			Sequences: []Sequence{
				{Name: "normal", Build: on(manageSudo, flow.Fixture{Planned: sudoPlan(false)}, false), Answers: confirm("y"), Ends: flow.Applied},
				{Name: "dry-run", Build: on(manageSudo, flow.Fixture{Planned: sudoPlan(false)}, true), Ends: flow.Planned},
				{Name: "failure", Build: on(manageSudo, sudoFails(), false), Answers: confirm("y"), Ends: flow.Failed},
				{Name: "typed", Build: on(manageSudo, flow.Fixture{Planned: sudoPlan(true)}, false), Answers: confirm(hostname), Ends: flow.Applied},
			}},
	}
}

// endings name a sequence's last step by how its flow ends.
var endings = map[flow.Outcome]string{
	flow.NotRun:    "not-run",
	flow.Applied:   "applied",
	flow.Failed:    "failed",
	flow.Cancelled: "cancelled",
	flow.Planned:   "planned",
	flow.Unchanged: "unchanged",
}

// Ending names how a flow ends, as a sequence's last step is named after
// it: "applied", "cancelled", "planned" and so on.
func Ending(o flow.Outcome) string { return endings[o] }

// Steps returns the names of the sequence's steps, in order: "1-confirm",
// a step for each answer, named after it, then the last step, named after
// how the flow ends, such as "2-applied".
func (s Sequence) Steps() []string {
	out := make([]string, 0, len(s.Answers)+1)
	for i, a := range s.Answers {
		out = append(out, fmt.Sprintf("%d-%s", i+1, a.Step))
	}
	return append(out, fmt.Sprintf("%d-%s", len(s.Answers)+1, endings[s.Ends]))
}

// Names returns the names of the mock's drawings, in the order Render
// returns them: the mock's own for a menu screen; for a workflow,
// "<name>/<sequence>/<step>" for each step of each sequence.
func (m Mock) Names() []string {
	if m.Screen != nil {
		return []string{m.Name}
	}
	var out []string
	for _, s := range m.Sequences {
		for _, step := range s.Steps() {
			out = append(out, m.Name+"/"+s.Name+"/"+step)
		}
	}
	return out
}

// Render returns the mock's drawings in c, in the order Names names them:
// the menu screen's Render, or each sequence's steps (Sequence.Play).
func (m Mock) Render(c component.Ctx) ([]string, error) {
	if m.Screen != nil {
		return []string{m.Screen.Render(c)}, nil
	}
	var out []string
	for _, s := range m.Sequences {
		steps, err := s.Play(c)
		if err != nil {
			return nil, fmt.Errorf("screen: %s: %w", m.Name, err)
		}
		out = append(out, steps...)
	}
	return out, nil
}

// Diverged is the error Sequence.Play returns when the flow, with no
// error, ends otherwise than the sequence says: as when the terminal is
// too narrow to ask a typed confirmation, and the flow cancels without
// asking it (flow.Cancelled, P-27 (e)). Transcript is what the run wrote,
// whole and without its last line break, since its steps are not the
// sequence's.
type Diverged struct {
	// Sequence is the sequence's name.
	Sequence string
	// Ended is how the flow ended, and Want how the sequence says it
	// ends.
	Ended, Want flow.Outcome
	// Transcript is what the run wrote.
	Transcript string
}

// Error says how the sequence ended.
func (e *Diverged) Error() string {
	return fmt.Sprintf("sequence %s ended %s, want %s", e.Sequence, endings[e.Ended], endings[e.Want])
}

// Play runs the sequence in c: its screen and flow (WorkflowStep.Run) on
// input that types each answer as a terminal would, echoing it after its
// prompt. It returns what each step writes, through c.Env.Writer, each
// without its last line break: a step for each answer, up to its prompt
// and the answer's line, then the last step, what the flow writes after
// the last answer. Played in order, the steps are the whole transcript.
// It fails when the flow returns an error, ends otherwise than s.Ends (a
// *Diverged), or leaves an answer unread.
func (s Sequence) Play(c component.Ctx) ([]string, error) {
	step, f := s.Build()
	out := new(transcript)
	in := &script{out: out}
	for _, a := range s.Answers {
		in.lines = append(in.lines, a.Line)
	}
	p := nav.Prompter{Ctx: c, In: prompt.New(in, false), Out: c.Env.Writer(out)}
	o, err := step.Run(context.Background(), p, f)
	switch {
	case err != nil:
		return nil, fmt.Errorf("sequence %s: %w", s.Name, err)
	case o != s.Ends:
		return nil, &Diverged{Sequence: s.Name, Ended: o, Want: s.Ends, Transcript: strings.TrimSuffix(out.String(), "\n")}
	case len(in.lines) > 0:
		return nil, fmt.Errorf("sequence %s left %d answers unread", s.Name, len(in.lines))
	}
	text, start := out.String(), 0
	var steps []string
	for _, at := range in.starts {
		// The answer's line ends at the line break the line reader
		// writes after it (prompt.Reader.Ask): the echo holds none.
		end := strings.IndexByte(text[at:], '\n')
		if end < 0 {
			return nil, fmt.Errorf("sequence %s: an answer's line has no end", s.Name)
		}
		steps = append(steps, text[start:at+end])
		start = at + end + 1
	}
	return append(steps, strings.TrimSuffix(text[start:], "\n")), nil
}

// script types a sequence's answers: each Read that needs a new line
// echoes it to out, as a terminal shows it, notes where it starts in the
// transcript, and returns it with its line break.
type script struct {
	lines   []string
	pending []byte
	out     *transcript
	starts  []int
}

func (s *script) Read(p []byte) (int, error) {
	if len(s.pending) == 0 {
		if len(s.lines) == 0 {
			return 0, io.EOF
		}
		line := s.lines[0]
		s.lines = s.lines[1:]
		s.starts = append(s.starts, s.out.echo(line))
		s.pending = []byte(line + "\n")
	}
	n := copy(p, s.pending)
	s.pending = s.pending[n:]
	return n, nil
}

// transcript is what a sequence writes, safe for the flow's reports from
// any goroutine.
type transcript struct {
	mu sync.Mutex
	b  strings.Builder
}

func (t *transcript) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.b.Write(p)
}

// echo writes line and returns where it starts.
func (t *transcript) echo(line string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	at := t.b.Len()
	t.b.WriteString(line)
	return at
}

func (t *transcript) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.b.String()
}

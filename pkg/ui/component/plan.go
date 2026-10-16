package component

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/diff"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// dryRunDone is the footer of a plan shown in dry-run mode: origin/main's
// words after a dry run (pkg/menu run_all_menu.go:288).
const dryRunDone = "No actual changes were made to your system."

// PlanChange is one change of a plan, as plain data: what brief Phase 3's
// Change gives through its Summary, Diff and Risk, with the target and the
// commands it runs.
type PlanChange struct {
	// Title says what the change does, such as "Set Port 2230". A plan
	// never hides a change, so an empty title shows "Unknown".
	Title string
	// Target is the file or service the change acts on, such as
	// "/etc/ssh/sshd_config.d/00-hardn.conf" or "ufw"; for a file it is
	// the file Diff changes. Empty shows none.
	Target string
	// LockoutRisk marks a change that could lock the user out (brief
	// Phase 3's LockoutRisk): an SSH or firewall change.
	LockoutRisk bool
	// Diff is the change to a file, as a unified diff such as
	// diff.Unified writes; empty shows none.
	Diff string
	// Before and After are a service's or setting's state before and after
	// the change, such as "inactive" and "active", for a change with no
	// file to diff. Each of their lines shows as a removed or an added
	// line; empty shows none.
	Before, After string
	// Commands are the commands the change runs, in order, such as
	// "systemctl reload ssh"; empty ones are skipped.
	Commands []string
}

// PlanView is the plan of the canonical flow's Plan step (brief 1C): the
// changes an action will make, before the user confirms them, and in
// dry-run mode the last thing shown. Here with no colour and ASCII glyphs,
// a lockout-risk change to an SSH drop-in:
//
//	0 2   6
//	  [*] Set Port 2230
//	      Lockout risk
//	      /etc/ssh/sshd_config.d/00-hardn.conf
//	      @@ -1,2 +1,2 @@
//	       # Managed by hardn
//	      -Port 22
//	      +Port 2230
//	      $ systemctl reload ssh
//
// Each change starts with Glyphs.Bullet in the marker column, or for a
// lockout-risk change Glyphs.Risk, and its title at theme.LabelCol,
// wrapped under itself. A lockout-risk change stands out by its glyph,
// the words "Lockout risk", the menu's (DEV-028), and their colour,
// Plan.LockoutRisk (DEV-038). Under the title come the target, the diff,
// the before and after states as removed and added lines, and the
// commands after Glyphs.Command, all at theme.LabelCol. A diff's lines
// keep their "+", "-" or space, so they never rest on colour alone, and
// take Plan.Added, Plan.Removed, Plan.Context or, for its hunk headers and
// markers, Plan.Meta; its "---" and "+++" file headers are left out when
// there is a target, which names the file. These lines are code: shown as
// they are, never cut, broken where the line ends, the rest after
// Glyphs.Wrap (DEV-038). Long plans are rendered in full; the screen
// decides paging.
//
// Changes are separated by theme.SectionGap blank lines. In dry-run mode
// a footer says that nothing was changed, as a Message in origin/main's
// words. A plan with no changes renders as the empty string: the flow
// says that nothing needs changing, and asks nothing.
type PlanView struct {
	// Changes are the planned changes, in the order they apply.
	Changes []PlanChange
	// DryRun adds the footer saying that nothing was changed.
	DryRun bool
}

// Render renders the plan for c. See PlanView for the layout.
func (vm PlanView) Render(c Ctx) string {
	var blocks []string
	for _, ch := range vm.Changes {
		blocks = append(blocks, ch.render(c))
	}
	if len(blocks) == 0 {
		return ""
	}
	if vm.DryRun {
		blocks = append(blocks, Message{Text: dryRunDone}.Render(c))
	}
	return strings.Join(blocks, strings.Repeat("\n", theme.SectionGap+1))
}

// render renders one change for c.
func (ch PlanChange) render(c Ctx) string {
	t, g, w := c.Theme.Plan, c.Theme.Glyphs, c.width()
	lead := marker(g.Bullet, t.Context)
	if ch.LockoutRisk {
		lead = marker(g.Risk, t.LockoutRisk)
	}
	lead = lead.to(theme.LabelCol)
	title, style := clean(ch.Title), t.Title
	if title == "" {
		title, style = unknown, c.Theme.Status.Unknown
	}
	var out []string
	for _, l := range wrap(title, w-theme.LabelCol) {
		out = append(out, c.fit(append(lead, span{l, style}), w))
		lead = line{gap(theme.LabelCol)}
	}
	if ch.LockoutRisk {
		out = append(out, c.fit(line{gap(theme.LabelCol), {lockoutRisk, t.LockoutRisk}}, w))
	}
	add := func(s string, style lipgloss.Style) {
		out = append(out, c.code(theme.LabelCol, s, style)...)
	}
	target := clean(ch.Target)
	if target != "" {
		add(target, t.Target)
	}
	kinds := map[diff.Kind]lipgloss.Style{
		diff.Context: t.Context, diff.Removed: t.Removed, diff.Added: t.Added,
	}
	for _, l := range diff.Classify(ch.Diff) {
		if target != "" && l.Kind == diff.Header && (strings.HasPrefix(l.Text, "--- ") || strings.HasPrefix(l.Text, "+++ ")) {
			continue // the target line names the file
		}
		style, ok := kinds[l.Kind]
		if !ok {
			style = t.Meta
		}
		add(raw(l.Text), style)
	}
	for _, s := range []struct {
		text, sign string
		style      lipgloss.Style
	}{{ch.Before, "-", t.Removed}, {ch.After, "+", t.Added}} {
		if s.text == "" {
			continue
		}
		for _, l := range strings.Split(strings.TrimSuffix(s.text, "\n"), "\n") {
			add(s.sign+raw(l), s.style)
		}
	}
	for _, cmd := range ch.Commands {
		if cmd = strings.TrimSpace(raw(cmd)); cmd != "" {
			add(g.Command+" "+cmd, t.Command)
		}
	}
	return strings.Join(out, "\n")
}

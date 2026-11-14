package output

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/abbott/hardn/pkg/engine/risk"
)

// MarkdownOptions is what the Markdown report says beside its run.
type MarkdownOptions struct {
	// Now is when the report was made; it is written in UTC.
	Now time.Time
	// NotRoot: hardn ran as a user other than root, so what only root reads
	// shows Unknown. The report says so.
	NotRoot bool
}

// The Markdown report's own words.
const (
	mdNotRoot    = "Not root: what only root can read shows Unknown."
	mdNone       = "None"
	mdNotRead    = "Not read"
	mdNotKnown   = "Not known"
	mdUnknown    = "Unknown"
	mdNoLogin    = "None recorded"
	mdNoFindings = "No findings."
)

// The words of the support levels, the schema's (supportWords) for a person.
var mdSupport = map[string]string{"supported": "Supported", "best-effort": "Best effort", "out-of-scope": "Out of scope",
	"unlisted": "Unlisted"}

// WriteMarkdown writes run r to w as one Markdown document, in one write: the
// report to hand over (hardn system-details --output markdown). It holds what
// Build's document does, for a person: the host, its platform and support
// level, the risk level and its factors (r.RiskLevel), each control's row in
// the main menu's order (StatusViewOf) with its evidence, the findings, the
// checks' errors, and the System Details report (r.Details). A part r does not
// have is left out.
//
// It has no colour and no escape sequence. Every string that is not hardn's
// own constant goes through mdText or mdCode, so nothing the host holds is
// markup, a link or HTML (markdown_test.go).
func WriteMarkdown(w io.Writer, r Run, o MarkdownOptions) error {
	if _, err := io.WriteString(w, Markdown(r, o)); err != nil {
		return fmt.Errorf("output: write: %w", err)
	}
	return nil
}

// Markdown is WriteMarkdown's document. It is pure.
func Markdown(r Run, o MarkdownOptions) string {
	d := Build(r)
	var b strings.Builder
	title := "# hardn report"
	if name := mdCode(d.Host.Hostname, false); name != "" {
		title += ": " + name
	}
	b.WriteString(title + "\n\n")
	made := "Generated " + o.Now.UTC().Format("2006-01-02 15:04") + " UTC"
	if d.HardnVersion != "" {
		made += " by hardn " + mdText(d.HardnVersion)
	}
	b.WriteString(made + ".\n")
	if o.NotRoot {
		b.WriteString("\n> " + mdNotRoot + "\n")
	}
	mdHost(&b, d)
	titles := map[string]string{}
	for _, rw := range risk.Rows() {
		titles[rw.ID] = rw.Title
	}
	for _, rep := range r.Reports {
		titles[rep.Control] = rep.Title
	}
	if r.RiskLevel != nil {
		mdRisk(&b, *r.RiskLevel, titles)
	}
	if r.Reports != nil {
		l := risk.Level{}
		if r.RiskLevel != nil {
			l = *r.RiskLevel
		}
		mdStatus(&b, StatusViewOf(r.Reports, l), d.Status)
	}
	mdFindings(&b, d, titles)
	if d.Details != nil {
		mdDetails(&b, *d.Details)
	}
	return b.String()
}

// mdItem writes one "- key: value" line; value is already made safe.
func mdItem(b *strings.Builder, key, value string) { b.WriteString("- " + key + ": " + value + "\n") }

// mdRow writes one table row; each cell is already made safe.
func mdRow(b *strings.Builder, cells ...string) {
	b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
}

// mdTable writes a table's header.
func mdTable(b *strings.Builder, heads ...string) {
	mdRow(b, heads...)
	b.WriteString("|" + strings.Repeat("---|", len(heads)) + "\n")
}

// mdTri is a yes, no or unknown of the schema, for a person.
func mdTri(t string) string {
	switch t {
	case "yes":
		return "Yes"
	case "no":
		return "No"
	}
	return mdUnknown
}

func mdHost(b *strings.Builder, d Document) {
	h := d.Host
	b.WriteString("\n## Host\n\n")
	mdItem(b, "Hostname", mdCodeOr(h.Hostname, mdNotRead, false))
	mdItem(b, "OS", mdCodeOr(h.OS.PrettyName, mdUnknown, false))
	platform := mdUnknown
	if h.Platform.Distro != "unknown" {
		platform = mdText(strings.TrimSpace(h.Platform.Distro + " " + h.Platform.Version))
	}
	mdItem(b, "Platform", platform)
	support, ok := mdSupport[h.Platform.Support]
	if !ok {
		support = mdUnknown
	}
	mdItem(b, "Support", support)
	if d.Details != nil {
		kernel, uptime := mdNotRead, mdNotRead
		if k := d.Details.Kernel; k != nil {
			kernel = mdCode(*k, false)
		}
		if u := d.Details.Uptime; u != nil {
			uptime = mdText(*u)
		}
		mdItem(b, "Kernel", kernel)
		mdItem(b, "Uptime", uptime)
		mdTime(b, d.Details.Time)
	}
	mdItem(b, "Init", mdOr(h.Init))
	mdItem(b, "sshd started by", mdOr(h.SSH))
	container := mdTri(h.Container.Detected)
	if h.Container.Manager != "" {
		container += " (" + mdText(h.Container.Manager) + ")"
	}
	mdItem(b, "Container", container)
	pve := mdTri(h.Proxmox.Installed)
	if h.Proxmox.Version != "" {
		pve += " (" + mdText(h.Proxmox.Version) + ")"
	}
	mdItem(b, "Proxmox VE", pve)
	sudo := mdTri(h.Elevation.Sudo)
	if h.Elevation.Sudo == "yes" && h.Elevation.SudoFlavor != "unknown" {
		sudo += " (" + mdText(h.Elevation.SudoFlavor) + ")"
	}
	mdItem(b, "sudo", sudo)
	mdItem(b, "doas", mdTri(h.Elevation.Doas))
}

// mdTime is the time service and the clock: "Time service", with "not
// running" where it is not, "Clock", and "Synced to" where the service
// names a server.
func mdTime(b *strings.Builder, t DetailTime) {
	service := mdUnknown
	if t.Service != nil {
		service = mdNone
		if *t.Service != "none" {
			service = mdText(*t.Service)
		}
		if t.Running != nil && !*t.Running {
			service += " (not running)"
		}
	}
	mdItem(b, "Time service", service)
	clock := mdNotRead
	if t.Clock != nil {
		clock = map[string]string{ClockInSync: "In sync", ClockNotInSync: "Not in sync"}[*t.Clock]
	}
	mdItem(b, "Clock", clock)
	if t.SyncedTo != nil {
		mdItem(b, "Synced to", mdCode(*t.SyncedTo, false))
	}
}

// mdOr is one of the schema's open words, Unknown for "unknown".
func mdOr(w string) string {
	if w == "" || w == "unknown" {
		return mdUnknown
	}
	return mdText(w)
}

// mdRisk is the risk level as the main menu words it (RiskSummaryOf), then
// each factor, under its row's title; the pending reboot's has no row.
func mdRisk(b *strings.Builder, l risk.Level, titles map[string]string) {
	s := RiskSummaryOf(l)
	b.WriteString("\n## Risk level\n\n")
	if s.Level == "" {
		b.WriteString("**" + mdUnknown + "**\n")
	} else {
		b.WriteString("**" + s.Level + "**: " + s.Gloss + "\n")
	}
	if len(l.Factors) == 0 {
		return
	}
	b.WriteString("\n")
	for _, f := range l.Factors {
		title := titles[f.Control]
		if title == "" {
			title = "Host" // the pending reboot's: no row
		}
		mdItem(b, mdText(title), mdText(f.Detail))
	}
}

// mdStatus is each control's row, as the main menu orders and words them, then
// the evidence of each, in the document's order.
func mdStatus(b *strings.Builder, v StatusView, status []Status) {
	b.WriteString("\n## Status\n\n")
	mdTable(b, "Control", "State", "Detail")
	for _, row := range v.Status.Rows {
		mdRow(b, mdCell(row.Label), row.State.String(), mdCell(row.Detail))
	}
	var evidence bool
	for _, s := range status {
		for _, e := range s.Evidence {
			if !evidence {
				b.WriteString("\n### Evidence\n\n")
				evidence = true
			}
			b.WriteString("- " + mdText(s.Title) + ": " + mdCodeOr(e, mdNone, false) + "\n")
		}
	}
}

// mdFindings is the document's findings, then its errors: a check that failed,
// whose row is Unknown.
func mdFindings(b *strings.Builder, d Document, titles map[string]string) {
	control := func(id string) string {
		if t := titles[id]; t != "" {
			return mdCell(t)
		}
		return mdCell(id)
	}
	b.WriteString("\n## Findings\n\n")
	if len(d.Findings) == 0 {
		b.WriteString(mdNoFindings + "\n")
	} else {
		mdTable(b, "Code", "Finding", "Source", "Control")
		for _, f := range d.Findings {
			source := ""
			if f.Source != "" {
				source = mdCode(f.Source, true)
			}
			mdRow(b, mdCode(f.Code, true), mdCell(f.Detail), source, control(f.Control))
		}
	}
	if len(d.Errors) == 0 {
		return
	}
	b.WriteString("\n## Errors\n\n")
	mdTable(b, "Code", "Error", "Control")
	for _, e := range d.Errors {
		mdRow(b, mdCode(e.Code, true), mdCell(e.Message), control(e.Control))
	}
}

// mdDetails is the System Details report's sections, the host block's rows
// aside (mdHost): a section not read says so, as its finding does.
func mdDetails(b *strings.Builder, d Details) {
	b.WriteString("\n## Addresses\n\n")
	switch {
	case d.Addresses == nil:
		b.WriteString(mdNotRead + "\n")
	case len(d.Addresses) == 0:
		b.WriteString(mdNone + "\n")
	default:
		for _, a := range d.Addresses {
			mdItem(b, mdCodeOr(a.Interface, mdUnknown, false), mdCodeOr(a.Address, mdUnknown, false))
		}
	}
	b.WriteString("\n## Users\n\n")
	switch {
	case d.Users == nil:
		b.WriteString(mdNotRead + "\n")
	case len(d.Users) == 0:
		b.WriteString("None but root\n")
	default:
		mdTable(b, "User", "Admin rights")
		for _, u := range d.Users {
			tool := mdCell(u.Tool)
			switch u.Tool {
			case "":
				tool = mdNone
			case "unknown":
				tool = mdNotKnown
			}
			mdRow(b, mdCodeOr(u.Name, mdUnknown, true), tool)
		}
	}
	b.WriteString("\n## CPU and load\n\n")
	if c := d.CPU; c == nil {
		b.WriteString(mdNotRead + "\n")
	} else {
		if c.CPUs != nil {
			model := mdNotKnown
			if c.Model != nil {
				model = mdCode(*c.Model, false)
			}
			mdItem(b, "Model", model)
			cpus := strconv.Itoa(*c.CPUs)
			switch {
			case c.Sockets != nil && *c.Sockets == 1:
				cpus += ", 1 socket"
			case c.Sockets != nil:
				cpus += fmt.Sprintf(", %d sockets", *c.Sockets)
			}
			mdItem(b, "CPUs", cpus)
		}
		if c.Load != nil {
			mdItem(b, "Load (1, 5, 15 min)", mdText(strings.Join(c.Load[:], ", ")))
		}
	}
	b.WriteString("\n## Disk\n\n")
	if k := d.Disk; k == nil {
		b.WriteString(mdNotRead + "\n")
	} else {
		if k.Source != nil {
			root := mdCode(*k.Source, false)
			if k.Type != nil {
				root += " (" + mdText(*k.Type) + ")"
			}
			mdItem(b, "`/`", root)
		}
		if k.Used != nil && k.Total != nil {
			mdItem(b, "Used", FormatUsed(*k.Used, *k.Total))
		}
		if k.Pool != "" {
			mdItem(b, "Pool "+mdCode(k.Pool, false), mdText(k.Health))
		}
	}
	b.WriteString("\n## Memory\n\n")
	switch m := d.Memory; {
	case m == nil:
		b.WriteString(mdNotRead + "\n")
	case m.Used == nil:
		mdItem(b, "Used", mdNotKnown)
	default:
		mdItem(b, "Used", FormatUsed(*m.Used, m.Total))
	}
	b.WriteString("\n## Last login\n\n")
	if d.Logins == nil {
		b.WriteString(mdNotRead + "\n")
		return
	}
	mdTable(b, "User", "When", "From")
	for _, l := range d.Logins {
		when, from := mdNoLogin, ""
		switch {
		case l.Unknown:
			when = mdNotKnown
		case l.When != "":
			when = mdCell(l.When)
			if from = l.From; from == "" {
				from = l.TTY
			}
			if from != "" {
				from = mdCode(from, true)
			}
		}
		mdRow(b, mdCodeOr(l.User, mdUnknown, true), when, from)
	}
}

// FormatUsed says how much of total is used: "4.2 of 38.6 GiB (11%)"; in whole
// MiB where total is under 1 GiB, "87 of 256 MiB (34%)", as one decimal of GiB
// there reads as another percentage than the one shown. The System Details
// screen's words, and the Markdown report's.
func FormatUsed(used, total uint64) string {
	percent := "0%"
	if total > 0 {
		percent = fmt.Sprintf("%d%%", (used*100+total/2)/total)
	}
	if total < 1<<30 {
		mib := func(n uint64) string { return strconv.FormatUint((n+1<<19)>>20, 10) }
		return fmt.Sprintf("%s of %s MiB (%s)", mib(used), mib(total), percent)
	}
	gib := func(n uint64) string { return strconv.FormatFloat(float64(n)/(1<<30), 'f', 1, 64) }
	return fmt.Sprintf("%s of %s GiB (%s)", gib(used), gib(total), percent)
}

// mdLine is s on one line, with nothing a reader's terminal or renderer acts
// on unseen: each run of white space, a line end among it, is one space; any
// other control or format character (C0, DEL, C1, a bidi override) is written
// out as \uXXXX, as WriteJSON escapes it; invalid UTF-8 is U+FFFD. The ends
// are trimmed.
func mdLine(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			space = true
			continue
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			if r > 0xffff {
				fmt.Fprintf(&b, `\U%08x`, r)
			} else {
				fmt.Fprintf(&b, `\u%04x`, r)
			}
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// mdEscaped are the ASCII characters that start or end an inline construct of
// CommonMark or GitHub's Markdown: an escape, a code span, emphasis,
// strikethrough, a link or image, an autolink or raw HTML, an entity, a table's
// cell, mathematics; and # and >, which mdText's callers never put at a line's
// start, escaped all the same. A backslash before any of them makes it text
// (CommonMark 2.4, backslash escapes).
const mdEscaped = "\\`*_[]<>&|~$#!"

// mdLinked are what a renderer may turn into a link with no markup at all
// (GitHub's extended autolinks): a scheme's "://", "www.", an address's "@",
// and "mailto:" and "xmpp:". No escape stops that everywhere, so a string
// holding one is written as code (mdText).
var mdLinked = []string{"://", "www.", "@", "mailto:", "xmpp:"}

// mdText is s as Markdown text that shows s and nothing else: on one line
// (mdLine), each character of mdEscaped behind a backslash. A string a
// renderer could link (mdLinked) is a code span instead, where nothing is
// linked. It is for a sentence in a paragraph or a list item after its label,
// never at a line's start; mdCell is the same for a table's cell. "" stays "".
func mdText(s string) string { return mdInline(s, false) }

// mdCell is mdText for a table's cell (mdCode's cell).
func mdCell(s string) string { return mdInline(s, true) }

func mdInline(s string, cell bool) string {
	s = mdLine(s)
	lower := strings.ToLower(s)
	for _, l := range mdLinked {
		if strings.Contains(lower, l) {
			return mdCode(s, cell)
		}
	}
	var b strings.Builder
	for _, r := range s {
		if r < 0x80 && strings.ContainsRune(mdEscaped, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// mdCode is s as a code span, where Markdown reads no markup, link or HTML
// (CommonMark 6.1): on one line (mdLine), between backtick strings one longer
// than its longest run of backticks, padded with a space where it starts or
// ends with one. cell is true wherever a table's cell may hold it: a pipe is
// then "\|", which a table reads as a pipe inside a code span too (GitHub's
// Markdown, tables), and which shows its backslash outside a table in a
// renderer without them. "" is "": use mdCodeOr.
func mdCode(s string, cell bool) string {
	s = mdLine(s)
	if s == "" {
		return ""
	}
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		s = " " + s + " "
	}
	if cell {
		s = strings.ReplaceAll(s, "|", `\|`)
	}
	return fence + s + fence
}

// mdCodeOr is mdCode, or none, hardn's own word, for a string with nothing to
// show.
func mdCodeOr(s, none string, cell bool) string {
	if c := mdCode(s, cell); c != "" {
		return c
	}
	return none
}

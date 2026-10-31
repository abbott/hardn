// Package systemdetails runs hardn's System Details screen on a host (refactor
// plan slice 1, System Details and Logs, unit (c), next/s1-screen): main menu 9
// and hardn system-details, as the maintainer adopted the mock on 2026-10-30
// (docs/engine/gallery-system-details.md §5, decisions 2 (a), 3 (a), 4 (a), 5 (a)
// and the Made here calls, with §6's words), drawn by pkg/ui/screen
// (screen.SystemDetails) from what details.Read reads of the host, writing
// nothing, under plan only too. Run shows the screen; Print writes the report with
// no menu, for hardn system-details, which unit (d) wires.
//
// The package reaches the host only through host.System and pkg/engine/details
// (depguard rule screens-through-host) and renders only through pkg/ui and
// pkg/output. Its parity table's tests are beside it; they hold origin/main's two
// read-only screens and -p, pkg/screens/logs' among them.
package systemdetails

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/engine/details"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The rows' words (gallery-system-details.md §6, decided with decision 11;
// readers-system-details.md §9, adopted 2026-10-30).
const (
	rowOS       = "OS"
	rowKernel   = "Kernel"
	rowUptime   = "Uptime"
	rowHostname = "Hostname"
	rowModel    = "Model"
	rowCPUs     = "CPUs"
	rowLoad     = "Load (1, 5, 15 min)"
	rowUsed     = "Used"
	rowPool     = "Pool %s"
	rowRoot     = "/"
	rowLogins   = "Logins"
	noAddresses = "No addresses"
	noUsers     = "None but root"
	noSudo      = "No sudo"
	noDoas      = "No doas"
	noLogin     = "None recorded"
	notKnown    = "Not known"
	cantRead    = "Can't read %s"
	noLslogins  = "Not known: lslogins isn't installed"
	notRootRow  = "Not root: %s not read"
	fromHost    = "%s from %s"
	onTTY       = "%s on %s"
)

// Options is what the System Details screen runs with: what cmd/hardn has when its
// System Details menu item is chosen.
type Options struct {
	// Prompter asks and writes the screen through the one line reader, rendering
	// with its Ctx, whose Env is the terminal's.
	Prompter nav.Prompter
	// System is the host. A draw takes a host with no run log, as each command
	// details.Read runs is the host's to log (readers-system-details.md item 24).
	System host.System
	// Facts are detect's, the main menu's on the screen's path, from which the report
	// takes the OS and the elevation tool.
	Facts detect.Facts
	// Now is the clock a login's row reads the year against, its only use: nil is
	// time.Now.
	Now func() time.Time
}

// Run shows the System Details screen until 0 (nil) or q (nav.ErrQuit); the end of
// the input and Ctrl-C abort (prompt.ErrAborted). Each round reads the host once
// with details.Read and draws the report whole, its top scrolling off a short
// terminal, under what the last round left, writing nothing and running nothing but
// its reads. Plan only draws the same, as nothing is written: Options has no mode.
// Item 1, Refresh, draws again. Run returns an error wrapping fs.ErrInvalid, writing
// nothing, without a Prompter's In and Out or a System.
func Run(ctx context.Context, o Options) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.System == nil {
		return fmt.Errorf("system details screen: %w: it needs a Prompter with In and Out, and a System", fs.ErrInvalid)
	}
	return o.Prompter.Run(func() nav.Screen {
		sub := read(ctx, o.System, o.Facts, o.Now).SubMenu()
		menu := nav.Menu{Exit: sub.Menu.Exit}
		for _, it := range sub.Menu.Items {
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return nil }})
		}
		return sub.Screen(o.Prompter.Ctx, menu)
	})
}

// PrintOptions is what Print runs with: what hardn system-details has.
type PrintOptions struct {
	// Out is hardn's stdout, and Env its render environment: Env.TTY picks the form.
	Out io.Writer
	Env term.Env
	// System and Facts are as Options has them; Now too.
	System host.System
	Facts  detect.Facts
	Now    func() time.Time
}

// Print writes the report, with no menu and no prompt, reading the host once with
// details.Read and writing nothing else. To a terminal it is as the screen draws it
// (cli-terminal); off one, plain, no colour and no clear, each value whole however
// long (cli-pipe, DEV-067), whatever the environment says of its width or colour. It
// returns an error wrapping fs.ErrInvalid, writing nothing, without Out or a System,
// and a failed write.
func Print(ctx context.Context, o PrintOptions) error {
	if o.Out == nil || o.System == nil {
		return fmt.Errorf("system details report: %w: it needs Out and a System", fs.ErrInvalid)
	}
	c, env := component.NewCtx(o.Env), o.Env
	if !o.Env.TTY {
		c = component.OffTerminal()
		env = c.Env
	}
	view := read(ctx, o.System, o.Facts, o.Now)
	if _, err := io.WriteString(env.Writer(o.Out), view.Report(c)+"\n"); err != nil {
		return fmt.Errorf("system details report: write: %w", err)
	}
	return nil
}

// read reads the host and fills the view.
func read(ctx context.Context, sys host.System, facts detect.Facts, now func() time.Time) screen.SystemDetails {
	if now == nil {
		now = time.Now
	}
	return viewOf(details.Read(ctx, sys, facts), now())
}

// viewOf is r as the screen draws it. Every row goes through logview.Safe, on and
// off a terminal, as the components' clean leaves a format character such as U+202E
// (readers-system-details.md §12, decision 32): what a host says is shown, and made
// safe, here alone. now's year decides a login's form.
func viewOf(r details.Report, now time.Time) screen.SystemDetails {
	return screen.SystemDetails{
		OS: safeRows(osRows(r)), Host: safeRows(hostRows(r)), Users: safeRows(userRows(r)), CPU: safeRows(cpuRows(r)),
		Disk: safeRows(diskRows(r)), Memory: safeRows(memoryRows(r)), Logins: safeRows(loginRows(r, now)),
	}
}

// safeRows are rows with each string made safe to show.
func safeRows(rows []component.KeyValue) []component.KeyValue {
	for i := range rows {
		rows[i].Key, rows[i].Value, rows[i].Note = logview.Safe(rows[i].Key), logview.Safe(rows[i].Value), logview.Safe(rows[i].Note)
	}
	return rows
}

// notKnownRow is a row whose fact is not established, though read.
func notKnownRow(key string) component.KeyValue {
	return component.KeyValue{Key: key, Value: notKnown, State: component.ValueUnknown}
}

// cantReadRows are the rows that say what of a section was not read, which follow
// the facts it did read: a fact keeps its section's others, and the rest of the
// report stays (gallery-system-details.md §5, Made here; readers §12 decision 36).
func cantReadRows(unread []details.Unread) []component.KeyValue {
	var rows []component.KeyValue
	for _, u := range unread {
		rows = append(rows, component.KeyValue{Key: fmt.Sprintf(cantRead, u.Source), State: component.ValueWarn})
	}
	return rows
}

// osWords is the distribution as the header's bar names it: "Debian 13 (Trixie)".
func osWords(r details.Report) string {
	head := output.HeaderViewOf("", header.Header{OS: r.OS.OS}).Context
	name := strings.TrimSpace(head.Name + " " + head.Version)
	if head.Codename != "" {
		name += " (" + head.Codename + ")"
	}
	return name
}

func osRows(r details.Report) []component.KeyValue {
	var rows []component.KeyValue
	if name := osWords(r); name != "" {
		rows = append(rows, component.KeyValue{Key: rowOS, Value: name})
	}
	if r.OS.Kernel != "" {
		rows = append(rows, component.KeyValue{Key: rowKernel, Value: r.OS.Kernel})
	}
	if r.OS.Uptime.Known {
		rows = append(rows, component.KeyValue{Key: rowUptime, Value: output.FormatUptime(r.OS.Uptime.Duration)})
	}
	return append(rows, cantReadRows(r.OS.Unread)...)
}

func hostRows(r details.Report) []component.KeyValue {
	var rows []component.KeyValue
	if r.Host.Hostname != "" {
		rows = append(rows, component.KeyValue{Key: rowHostname, Value: r.Host.Hostname})
	}
	for _, a := range r.Host.Addresses {
		rows = append(rows, component.KeyValue{Key: a.Interface, Value: a.Address})
	}
	if r.Host.Addresses != nil && len(r.Host.Addresses) == 0 {
		rows = append(rows, component.KeyValue{Key: noAddresses, State: component.ValueNone})
	}
	return append(rows, cantReadRows(r.Host.Unread)...)
}

// userRows are the Users section's rows: each user and what lets them administer, as
// the users check reads it. As a user other than root the rules refused are one row,
// naming each: where they are not read a user's row says Not known, never a group's
// guess.
func userRows(r details.Report) []component.KeyValue {
	if r.Users.List == nil {
		return cantReadRows(r.Users.Unread)
	}
	noTool := noSudo
	if r.Users.Tool == sudoers.ToolDoas.String() {
		noTool = noDoas
	}
	var rows []component.KeyValue
	for _, u := range r.Users.List {
		switch tool, t := u.Admin(); t {
		case detect.Yes:
			rows = append(rows, component.KeyValue{Key: u.Name, Value: tool})
		case detect.No:
			rows = append(rows, component.KeyValue{Key: u.Name, Value: noTool})
		default:
			rows = append(rows, notKnownRow(u.Name))
		}
	}
	if len(rows) == 0 {
		rows = []component.KeyValue{{Key: noUsers, State: component.ValueNone}}
	}
	unread := r.Users.Unread
	if refused := r.Users.NotRead(); r.Users.Root == detect.No && len(refused) > 0 {
		rows = append(rows, component.KeyValue{Key: fmt.Sprintf(notRootRow, strings.Join(refused, ", ")), State: component.ValueWarn})
		unread = slices.DeleteFunc(slices.Clone(unread), func(u details.Unread) bool { return slices.Contains(refused, u.Source) })
	}
	return append(rows, cantReadRows(unread)...)
}

// cpuRows are the CPU and Load section's rows: Model says Not known where a
// /proc/cpuinfo read has no model name, as on arm64 [I, not run].
func cpuRows(r details.Report) []component.KeyValue {
	var rows []component.KeyValue
	if c := r.CPU; c.CPUs > 0 {
		model := component.KeyValue{Key: rowModel, Value: c.Model}
		if c.Model == "" {
			model = notKnownRow(rowModel)
		}
		rows = append(rows, model, component.KeyValue{Key: rowCPUs, Value: cpusWords(c)})
	}
	if r.CPU.Load[0] != "" {
		rows = append(rows, component.KeyValue{Key: rowLoad, Value: strings.Join(r.CPU.Load[:], ", ")})
	}
	return append(rows, cantReadRows(r.CPU.Unread)...)
}

// cpusWords is "4, 1 socket", the CPUs alone where the sockets are not known.
func cpusWords(c details.CPU) string {
	switch c.Sockets {
	case 0:
		return strconv.Itoa(c.CPUs)
	case 1:
		return strconv.Itoa(c.CPUs) + ", 1 socket"
	}
	return fmt.Sprintf("%d, %d sockets", c.CPUs, c.Sockets)
}

func diskRows(r details.Report) []component.KeyValue {
	var rows []component.KeyValue
	d := r.Disk
	if d.Source != "" {
		rows = append(rows, component.KeyValue{Key: rowRoot, Value: d.Source + " " + d.Type})
	}
	if d.Known {
		rows = append(rows, component.KeyValue{Key: rowUsed, Value: usedOf(d.Used, d.Total)})
	}
	if d.Pool != "" {
		pool := component.KeyValue{Key: fmt.Sprintf(rowPool, d.Pool), Value: d.Health, State: component.ValueWarn}
		switch d.Health {
		case "":
			pool = notKnownRow(pool.Key)
		case "ONLINE":
			pool.State = component.ValueOK
		}
		rows = append(rows, pool)
	}
	return append(rows, cantReadRows(d.Unread)...)
}

func memoryRows(r details.Report) []component.KeyValue {
	var rows []component.KeyValue
	if r.Memory.Total > 0 {
		if used, ok := r.Memory.Used(); ok {
			rows = append(rows, component.KeyValue{Key: rowUsed, Value: usedOf(used, r.Memory.Total)})
		} else {
			rows = append(rows, notKnownRow(rowUsed))
		}
	}
	return append(rows, cantReadRows(r.Memory.Unread)...)
}

// gib is n bytes in GiB, one decimal.
func gib(n uint64) string { return strconv.FormatFloat(float64(n)/(1<<30), 'f', 1, 64) }

// usedOf says how much of total is used: "4.2 of 38.6 GiB (11%)".
func usedOf(used, total uint64) string {
	return fmt.Sprintf("%s of %s GiB (%s)", gib(used), gib(total), percent(used, total))
}

// percent is used of total as a whole percentage.
func percent(used, total uint64) string {
	if total == 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", (used*100+total/2)/total)
}

// loginRows are the Last Login section's rows: one where no answer holds any, its
// words by why.
func loginRows(r details.Report, now time.Time) []component.KeyValue {
	switch r.Logins.State {
	case details.LoginsNoTool:
		return []component.KeyValue{{Key: rowLogins, Value: noLslogins, State: component.ValueUnknown}}
	case details.LoginsUnread:
		return cantReadRows(r.Logins.Unread)
	}
	var rows []component.KeyValue
	for _, l := range r.Logins.List {
		rows = append(rows, loginRow(l, now))
	}
	return rows
}

// loginRow is a login's row. When is the year in place of the clock, as ls does,
// where it was not this year.
func loginRow(l details.Login, now time.Time) component.KeyValue {
	when := l.At.Format("Jan 2 15:04")
	if l.At.Year() != now.Year() {
		when = l.At.Format("Jan 2 2006")
	}
	switch {
	case l.State == details.LoginUnknown:
		return notKnownRow(l.User)
	case l.State == details.LoginNone:
		return component.KeyValue{Key: l.User, Value: noLogin, State: component.ValueNone}
	case l.From == "":
		return component.KeyValue{Key: l.User, Value: fmt.Sprintf(onTTY, when, l.TTY)}
	}
	return component.KeyValue{Key: l.User, Value: fmt.Sprintf(fromHost, when, l.From)}
}

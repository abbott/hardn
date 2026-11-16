package configscreen

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	usersctl "github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/authkeys"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/logfwd"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// Editing hardn.yml (docs/engine/design-config-edit.md, adopted 2026-11-15,
// every (a) of its §4 and decision 13): "Edit a setting", the keys numbered;
// a key's entry, checked under its field and asked again; its save through
// the flow (screenkit.Session.Change), whose plan is steps.PlanSave of that
// key alone, so that -u's username and -n's mode, held in memory, are never
// written; then one line saying where the key acts. Nothing is applied to
// the host: the change is hardn.yml's.

// The fields' words. The mocks' (design §3) where it has them; the rest
// chosen here (design §7).
const (
	askPort     = "Enter SSH port [%d]"
	askUsername = "Enter username (Enter for none)"
	askTimeout  = "Enter confirm timeout (Enter for %s)"
	askKey      = "Enter SSH public key"
	askNTP      = "Enter NTP server"
	askSyslog   = "Enter syslog server"
	askProto    = "Enter protocol [tcp]"
	askLogPort  = "Enter port [%d]"
	askCAFile   = "Enter CA file"
	pickKey     = "Select a key to remove"
	pickServer  = "Select a server to remove"
	pickChange  = "Select a server"
	askNewProto = "Enter protocol [%s]"
	// The questions.
	askSave      = "Save %s?"
	askSaveValue = "Save %s: %t?"
	// The refusals under a field.
	badPort    = "Not a port between 1 and 65535"
	badTimeout = "Not a time of %s or more, such as 90s or 5m"
	badKey     = "Not an SSH public key hardn takes"
	sameKey    = "This key is in sshKeys already"
	badIPv6    = "An IPv6 address, which hardn does not set yet"
	badHost    = "Not an IPv4 address or a host name"
	badProto   = "Not udp, tcp or tls"
	// Where a saved key acts (decision 5 (a)).
	actsRunAll = "Run All applies it."
	actsNTP    = "Set them under NTP Settings, or with Run All."
	actsSyslog = "Set them under Syslog Settings, or with Run All."
	actsPort   = "sshd's port is not changed. Change it under " + screen.SSHTitle + " (2)."
	actsStart  = "From the next start."
	// udpOnly is decision 13's line: Syslog Settings' words for busybox
	// syslogd's refusal of a TCP or TLS server, then where and how to change
	// it: Change protocol, under Edit a setting (2026-11-16).
	udpOnly = "busybox syslogd is UDP only. Change protocol under Edit a setting."
)

// acts is the line after a key's save; a key with none says nothing:
// confirmTimeout acts at once.
var acts = map[string]string{
	"username": actsRunAll, "sshKeys": actsRunAll, "disableRootSSH": actsRunAll, "enableUfwSshPolicy": actsRunAll,
	"configureDns": actsRunAll, "kernel": actsRunAll, "enableAutoUpdates": actsRunAll,
	"ntpServers": actsNTP, "syslogServers": actsSyslog, "sshPort": actsPort, "checkReleases": actsStart,
}

// toggles are the yes/no keys, each with its field of the configuration.
var toggles = map[string]func(*config.Config) *bool{
	"disableRootSSH":     func(c *config.Config) *bool { return &c.DisableRootSSH },
	"enableUfwSshPolicy": func(c *config.Config) *bool { return &c.EnableUfwSshPolicy },
	"configureDns":       func(c *config.Config) *bool { return &c.ConfigureDns },
	"kernel":             func(c *config.Config) *bool { return &c.KernelSettings },
	"checkReleases":      func(c *config.Config) *bool { return &c.CheckReleases },
	"enableAutoUpdates":  func(c *config.Config) *bool { return &c.EnableAutoUpdates },
}

// saveID is the save's change, and saveControl the control that plans it:
// the save of keys alone, from the configuration the engine plans with.
const saveID = "config/save"

type saveControl struct{ keys []string }

func (saveControl) ID() string                          { return "config" }
func (saveControl) Title() string                       { return "Configuration" }
func (saveControl) Applies(detect.Facts) (bool, string) { return true, "" }
func (saveControl) Check(context.Context, host.System, detect.Facts) (engine.Status, error) {
	return engine.Status{State: engine.NotApplicable}, nil
}
func (c saveControl) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	return steps.PlanSave(ctx, sys, saveID, in.Config, c.keys)
}

// runner is one run of the screen.
type runner struct {
	o     Options
	s     screenkit.Session
	saved bool // the last change saved hardn.yml
}

// draw returns the hardn.yml screen as the file is now, its item bound.
func (r *runner) draw(ctx context.Context) nav.Screen {
	v := View(r.o.Config, r.o.Found)
	if note := r.note(ctx, r.o.Config); note != "" {
		v.Notes = map[string]string{"syslogServers": note}
	}
	sub := v.SubMenu()
	menu := nav.Menu{Exit: sub.Menu.Exit}
	for _, it := range sub.Menu.Items {
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return r.keys(ctx) }})
	}
	return sub.Screen(r.o.Prompter.Ctx, menu)
}

// note is LoggerNote on the screen's host.
func (r *runner) note(ctx context.Context, cfg *config.Config) string {
	return LoggerNote(ctx, r.o.System, r.o.Facts, cfg)
}

// LoggerNote is decision 13's line: that busybox syslogd is UDP only, and
// where to change the server, where the host's logger is busybox syslogd
// and cfg lists a server that is not UDP; "" otherwise, and where the list
// is not valid or the logger is not read. The host is read only for a list
// with such a server.
func LoggerNote(ctx context.Context, sys host.System, f detect.Facts, cfg *config.Config) string {
	servers, err := cfg.SyslogServers.Resolve()
	if err != nil || !slices.ContainsFunc(servers, func(s config.LogServer) bool { return s.Protocol != config.LogUDP }) {
		return ""
	}
	if rd := logfwd.Read(ctx, sys, f); rd.Err != nil || rd.Logger != logfwd.Busybox {
		return ""
	}
	return udpOnly
}

// keys is "Edit a setting": the keys and their values, until the user goes
// back.
func (r *runner) keys(ctx context.Context) error {
	p := r.o.Prompter
	return p.Run(func() nav.Screen {
		values := texts(r.o.Config)
		for k, v := range values {
			values[k] = logview.Safe(v)
		}
		sub := screen.ConfigEdit{Values: values}.SubMenu()
		menu := nav.Menu{Exit: sub.Menu.Exit}
		keys := screen.ConfigEditKeys()
		for i, it := range sub.Menu.Items {
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return r.edit(ctx, keys[i]) }})
		}
		return sub.Screen(p.Ctx, menu)
	})
}

// edit is one key: a yes/no key turned over, a value asked, or a list's
// screen.
func (r *runner) edit(ctx context.Context, key string) error {
	cfg := r.o.Config
	if field, ok := toggles[key]; ok {
		to := !*field(cfg)
		return r.save(ctx, key, form.Form{}, fmt.Sprintf(askSaveValue, key, to), func(c *config.Config) { *field(c) = to })
	}
	var entry string
	switch key {
	case "sshPort":
		f := &form.Text{Title: fmt.Sprintf(askPort, cfg.SshPort), Default: strconv.Itoa(cfg.SshPort), Value: &entry, Validate: func(s string) error {
			if !isPort(s) {
				return errors.New(badPort)
			}
			return nil
		}}
		return r.save(ctx, key, one(f), fmt.Sprintf(askSave, key), func(c *config.Config) { c.SshPort, _ = strconv.Atoi(entry) })
	case "username":
		f := &form.Text{Title: askUsername, Value: &entry, Validate: func(s string) error {
			if err := usersctl.CheckNameForm(s); s != "" && err != nil {
				return errors.New(strings.TrimSuffix(screenkit.RefusalWords(err), "."))
			}
			return nil
		}}
		return r.save(ctx, key, one(f), fmt.Sprintf(askSave, key), func(c *config.Config) { c.Username = entry })
	case "confirmTimeout":
		f := &form.Text{Title: fmt.Sprintf(askTimeout, short(revert.DefaultTimeout.String())), Value: &entry, Validate: func(s string) error {
			if s == "" {
				return nil
			}
			d, err := revert.ParseTimeout(s)
			if err == nil {
				_, err = revert.ResolveTimeout(nil, &d)
			}
			if err != nil {
				return fmt.Errorf(badTimeout, short(revert.MinTimeout.String()))
			}
			return nil
		}}
		return r.save(ctx, key, one(f), fmt.Sprintf(askSave, key), func(c *config.Config) { c.ConfirmTimeout = entry })
	}
	return r.list(ctx, key)
}

// one is a form of one field.
func one(f form.Field) form.Form { return form.Form{Fields: []form.Field{f}} }

// short is a duration's text without its zero units: "3m0s" is "3m".
func short(d string) string {
	if strings.HasSuffix(d, "m0s") {
		d = strings.TrimSuffix(d, "0s")
	}
	return d
}

// isPort reports whether s is a port, 1 to 65535, in digits alone.
func isPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535 && strconv.Itoa(n) == s
}

// save runs one key's change through the flow: the form, each answer
// checked; the plan, the save of key alone from a copy of hardn.yml edited
// by set; the question; the save. Once saved it says where the key acts,
// and decision 13's line for syslogServers. -u's username stays the
// session's when the file's is the one saved.
func (r *runner) save(ctx context.Context, key string, f form.Form, question string, set func(*config.Config)) error {
	cfg := r.o.Config
	session, flagged := cfg.Username, cfg.Username != cfg.FileUsername()
	r.saved = false
	err := r.s.Change(ctx, screenkit.Change{
		Step: screen.ConfigStep(key),
		Flow: flow.Flow{Form: f, Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: question} }},
		Controls: func(edited *config.Config) []engine.Control {
			set(edited)
			return []engine.Control{saveControl{keys: []string{key}}}
		},
	})
	if !r.saved {
		return err
	}
	if flagged {
		cfg.Username = session
	}
	var msgs component.Messages
	if line := acts[key]; line != "" {
		msgs = append(msgs, component.Message{Text: line})
	}
	if key == "syslogServers" {
		if note := r.note(ctx, cfg); note != "" {
			msgs = append(msgs, component.Message{Kind: component.MessageWarn, Text: note})
		}
	}
	if len(msgs) == 0 {
		return err
	}
	if werr := r.s.Say(msgs)(); err == nil {
		err = werr
	}
	return err
}

// ran records whether a change saved hardn.yml, then gives the run on.
func (r *runner) ran(run output.Run) {
	r.saved = run.Mode == output.ModeApply && run.Result != nil && run.Err == nil && !run.Declined && !run.Result.Nothing
	if r.o.Ran != nil {
		r.o.Ran(run)
	}
}

// list is a list key's screen: its entries, Add and Remove, until the user
// goes back; one entry a save.
func (r *runner) list(ctx context.Context, key string) error {
	p := r.o.Prompter
	return p.Run(func() nav.Screen {
		cfg := r.o.Config
		v := screen.ConfigList{Key: key, Entries: entries(cfg, key)}
		if key == "syslogServers" {
			v.Note = r.note(ctx, cfg)
		}
		sub := v.SubMenu()
		menu := nav.Menu{Exit: sub.Menu.Exit}
		for _, it := range sub.Menu.Items {
			action := func() error { return r.add(ctx, key) }
			switch it.Key {
			case screen.ConfigListRemove:
				action = func() error { return r.remove(ctx, key, v.Entries) }
			case screen.ConfigListChange:
				action = func() error { return r.changeSyslog(ctx, v.Entries) }
			}
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: action})
		}
		return sub.Screen(p.Ctx, menu)
	})
}

// entries are key's list as its screen shows it, each of the file's words
// made safe: a server; a syslog server with its port and protocol; an SSH
// key's type, comment and fingerprint, never the key.
func entries(cfg *config.Config, key string) []string {
	var out []string
	switch key {
	case "sshKeys":
		for _, line := range cfg.SshKeys {
			k, err := authkeys.ParsePublic(line)
			if err != nil {
				out = append(out, notValid)
				continue
			}
			out = append(out, logview.Safe(strings.Join(strings.Fields(k.Type+" "+k.Comment+" "+k.Fingerprint), " ")))
		}
	case "ntpServers":
		for _, s := range cfg.NTPServers {
			out = append(out, logview.Safe(s))
		}
	case "syslogServers":
		for _, s := range cfg.SyslogServers {
			proto, port := s.Protocol, s.Port
			if proto == "" {
				proto = config.LogTCP
			}
			if port == 0 {
				port = defaultPort(proto)
			}
			out = append(out, logview.Safe(fmt.Sprintf("%s:%d %s", s.Server, port, strings.ToUpper(proto))))
		}
	}
	return out
}

// defaultPort is a syslog server's port where none is given.
func defaultPort(proto string) int {
	if proto == config.LogTLS {
		return config.LogPortTLS
	}
	return config.LogPort
}

// remove is a list's Remove: the entry picked by number, then the save.
func (r *runner) remove(ctx context.Context, key string, shown []string) error {
	title := pickServer
	if key == "sshKeys" {
		title = pickKey
	}
	opts := make([]form.Option[int], len(shown))
	for i, e := range shown {
		opts[i] = form.Option[int]{Label: e, Value: i}
	}
	var gone int
	f := &form.Select[int]{Title: title, Options: opts, Value: &gone}
	return r.save(ctx, key, one(f), fmt.Sprintf(askSave, key), func(c *config.Config) {
		switch key {
		case "sshKeys":
			c.SshKeys = slices.Delete(slices.Clone(c.SshKeys), gone, gone+1)
		case "ntpServers":
			c.NTPServers = slices.Delete(slices.Clone(c.NTPServers), gone, gone+1)
		case "syslogServers":
			c.SyslogServers = slices.Delete(slices.Clone(c.SyslogServers), gone, gone+1)
		}
	})
}

// add is a list's Add: one entry asked and checked, the whole list with it
// by the key's own check, then the save.
func (r *runner) add(ctx context.Context, key string) error {
	cfg := r.o.Config
	question := fmt.Sprintf(askSave, key)
	var entry string
	switch key {
	case "sshKeys":
		f := &form.Text{Title: askKey, Value: &entry, Validate: func(s string) error {
			k, err := authkeys.ParsePublic(s)
			if err != nil {
				return errors.New(badKey)
			}
			for _, line := range cfg.SshKeys {
				if o, err := authkeys.ParsePublic(line); err == nil && o.Same(k) {
					return nav.Warn(sameKey)
				}
			}
			return nil
		}}
		return r.save(ctx, key, one(f), question, func(c *config.Config) { c.SshKeys = append(slices.Clone(c.SshKeys), entry) })
	case "ntpServers":
		f := &form.Text{Title: askNTP, Value: &entry, Validate: func(s string) error {
			if err := checkHost(s); err != nil {
				return err
			}
			_, err := append(slices.Clone(cfg.NTPServers), s).Resolve()
			return words(err, config.ErrNTPServers, len(cfg.NTPServers)+1)
		}}
		return r.save(ctx, key, one(f), question, func(c *config.Config) { c.NTPServers = append(slices.Clone(c.NTPServers), entry) })
	}
	return r.addSyslog(ctx)
}

// addSyslog is syslogServers' Add: the address, the protocol [tcp], the
// port [514, or 6514 for tls] and, for tls, the CA file; each checked as it
// is typed, the whole list with the new block by SyslogServers.Resolve at
// the last field. The block written holds only what differs from the
// defaults.
func (r *runner) addSyslog(ctx context.Context) error {
	cfg := r.o.Config
	var server, proto, port, ca string
	block := func() config.SyslogServer {
		b := config.SyslogServer{Server: server, CAFile: ca}
		if proto != config.LogTCP {
			b.Protocol = proto
		}
		if n, _ := strconv.Atoi(port); n != defaultPort(proto) {
			b.Port = n
		}
		return b
	}
	whole := func() error {
		_, err := append(slices.Clone(cfg.SyslogServers), block()).Resolve()
		return words(err, config.ErrSyslogServers, len(cfg.SyslogServers)+1)
	}
	tls := func() bool { return proto == config.LogTLS }
	f := form.Form{Fields: []form.Field{
		&form.Text{Title: askSyslog, Value: &server, Validate: checkHost},
		&form.Text{Title: askProto, Default: config.LogTCP, Value: &proto, Validate: func(s string) error {
			switch s {
			case config.LogUDP, config.LogTCP:
				return nil
			case config.LogTLS:
				if _, isAddr, _ := config.ResolveHost(server); isAddr {
					return errors.New("TLS needs the server's name, as its certificate has it")
				}
				return nil
			}
			return errors.New(badProto)
		}},
		&form.Text{TitleFunc: func() string { return fmt.Sprintf(askLogPort, defaultPort(proto)) }, Value: &port, Validate: func(s string) error {
			if s == "" {
				s = strconv.Itoa(defaultPort(proto))
			}
			if !isPort(s) {
				return errors.New(badPort)
			}
			if tls() {
				return nil // the CA file is asked next, and the list checked there
			}
			was := port
			port = s
			defer func() { port = was }()
			return whole()
		}},
		&form.Text{Title: askCAFile, Value: &ca, Skip: func() bool { return !tls() }, Validate: func(s string) error {
			was := ca
			ca = s
			defer func() { ca = was }()
			return whole()
		}},
	}}
	return r.save(ctx, "syslogServers", f, fmt.Sprintf(askSave, "syslogServers"), func(c *config.Config) {
		if !tls() {
			ca = ""
		}
		c.SyslogServers = append(slices.Clone(c.SyslogServers), block())
	})
}

// changeSyslog is syslogServers' Change protocol: the server picked by
// number, as Remove picks it; its protocol [the current]; the port
// [the new protocol's default] only where the protocol changed and the
// server's port was the old protocol's default or unset, an explicit other
// port kept as it is; for tls, the CA file. The whole list, the server's
// block changed in place, is checked by SyslogServers.Resolve at the last
// field asked. Only that block changes; the same protocol changes nothing.
func (r *runner) changeSyslog(ctx context.Context, shown []string) error {
	cfg := r.o.Config
	opts := make([]form.Option[int], len(shown))
	for i, e := range shown {
		opts[i] = form.Option[int]{Label: e, Value: i}
	}
	var at int
	var proto, port, ca string
	old := func() config.SyslogServer { return cfg.SyslogServers[at] }
	oldProto := func() string {
		if p := old().Protocol; p != "" {
			return p
		}
		return config.LogTCP
	}
	cur := func() string { // Enter keeps the protocol
		if proto == "" {
			return oldProto()
		}
		return proto
	}
	changed := func() bool { return cur() != oldProto() }
	followsDefault := func() bool { p := old().Port; return p == 0 || p == defaultPort(oldProto()) }
	tls := func() bool { return cur() == config.LogTLS }
	block := func() config.SyslogServer {
		b := old()
		if !changed() {
			return b
		}
		b.Protocol, b.CAFile = "", ""
		if cur() != config.LogTCP {
			b.Protocol = cur()
		}
		if tls() {
			b.CAFile = ca
		}
		if followsDefault() {
			b.Port = 0
			if n, _ := strconv.Atoi(port); port != "" && n != defaultPort(cur()) {
				b.Port = n
			}
		}
		return b
	}
	whole := func() error {
		l := slices.Clone(cfg.SyslogServers)
		l[at] = block()
		_, err := l.Resolve()
		return words(err, config.ErrSyslogServers, at+1)
	}
	f := form.Form{Fields: []form.Field{
		&form.Select[int]{Title: pickChange, Options: opts, Value: &at},
		&form.Text{TitleFunc: func() string { return fmt.Sprintf(askNewProto, oldProto()) }, Value: &proto, Validate: func(s string) error {
			if s == "" {
				s = oldProto()
			}
			switch s {
			case config.LogUDP, config.LogTCP, config.LogTLS:
			default:
				return errors.New(badProto)
			}
			was := proto
			proto = s
			defer func() { proto = was }()
			if s == config.LogTLS && changed() {
				if _, isAddr, _ := config.ResolveHost(old().Server); isAddr {
					return errors.New("TLS needs the server's name, as its certificate has it")
				}
				return nil // the port or the CA file is asked next
			}
			if changed() && followsDefault() {
				return nil // the port is asked next
			}
			return whole()
		}},
		&form.Text{TitleFunc: func() string { return fmt.Sprintf(askLogPort, defaultPort(cur())) }, Value: &port,
			Skip: func() bool { return !changed() || !followsDefault() }, Validate: func(s string) error {
				if s == "" {
					s = strconv.Itoa(defaultPort(cur()))
				}
				if !isPort(s) {
					return errors.New(badPort)
				}
				if tls() {
					return nil // the CA file is asked next, and the list checked there
				}
				was := port
				port = s
				defer func() { port = was }()
				return whole()
			}},
		&form.Text{Title: askCAFile, Value: &ca, Skip: func() bool { return !tls() || !changed() }, Validate: func(s string) error {
			was := ca
			ca = s
			defer func() { ca = was }()
			return whole()
		}},
	}}
	return r.save(ctx, "syslogServers", f, fmt.Sprintf(askSave, "syslogServers"), func(c *config.Config) {
		l := slices.Clone(c.SyslogServers)
		l[at] = block()
		c.SyslogServers = l
	})
}

// checkHost refuses a server that is not an IPv4 address or a host name, an
// IPv6 address in its own words.
func checkHost(s string) error {
	switch _, _, err := config.ResolveHost(s); {
	case errors.Is(err, config.ErrHostIPv6):
		return errors.New(badIPv6)
	case err != nil:
		return errors.New(badHost)
	}
	return nil
}

// words is a list's refusal (NTPServers.Resolve's, SyslogServers.Resolve's)
// as a field says it: config's own words for what is not valid, without the
// sentinel's. The entry's number goes only where the entry is the one typed,
// the list's entry typed ("server 2: tls needs caFile" is "TLS needs
// caFile"); a fault of an entry the file already holds keeps its number
// ("Server 1: tls needs caFile"), so that it does not read as the typed
// one's. nil for nil.
func words(err, sentinel error, typed int) error {
	if err == nil {
		return nil
	}
	why := strings.TrimPrefix(err.Error(), sentinel.Error()+": ")
	if rest, ok := strings.CutPrefix(why, fmt.Sprintf("server %d: ", typed)); ok {
		why = rest
	}
	switch {
	case strings.HasPrefix(why, "tls "):
		why = "TLS " + strings.TrimPrefix(why, "tls ")
	case !strings.HasPrefix(why, "caFile"):
		why = strings.ToUpper(why[:1]) + why[1:]
	}
	return errors.New(why)
}

package updates

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// What apt's daily job is read from (apt 2.6.1 and 3.0.3:
// debian/apt.systemd.daily, the apt-daily and apt-daily-upgrade units;
// check-updates.md §3).
const (
	unattended = "unattended-upgrades" // installs unattended-upgrade, which apt.systemd.daily runs
	upgradeJob = "apt-daily-upgrade"   // .timer starts .service: apt.systemd.daily install, unattended-upgrade
	listsJob   = "apt-daily"           // .timer starts .service: apt.systemd.daily update, apt-get update
	dumpSource = "apt-config dump"
)

// The configuration names read. apt compares names without case
// (apt.conf(5)), and apt-config dump prints each as its first writer
// spelt it.
const (
	keyEnable  = "APT::Periodic::Enable"
	keyUpgrade = "APT::Periodic::Unattended-Upgrade"
	keyLists   = "APT::Periodic::Update-Package-Lists"
	keyPattern = "Unattended-Upgrade::Origins-Pattern"
	keyAllowed = "Unattended-Upgrade::Allowed-Origins"
)

// dumpArgs dump the subtrees read, each printed with its root, one item a
// line: `name "value";`, a list's items named after it with "::".
var dumpArgs = []string{"dump", "APT::Periodic", keyPattern, keyAllowed}

// What systemctl's states say of a timer, running now (is-active) and
// started at the next boot (is-enabled), and of the service it starts
// (is-enabled): masked, in /etc or in /run, or not found, it cannot be
// started; in any other state is-enabled documents but bad, it can.
// enabled is an enabling link in /etc/systemd/system; the -runtime states,
// links in /run, say nothing of what stays (as apparmor's nextBoot reads
// them, check-apparmor.md §2.8). Any other state systemctl(1) documents
// is not ranked.
var (
	running = map[string]detect.Tri{"active": detect.Yes, "inactive": detect.No, "failed": detect.No}
	atBoot  = map[string]detect.Tri{
		"enabled":  detect.Yes,
		"disabled": detect.No, "masked": detect.No, "linked": detect.No, "not-found": detect.No,
	}
	startable = map[string]detect.Tri{
		"masked": detect.No, "masked-runtime": detect.No, "not-found": detect.No,
		"static": detect.Yes, "enabled": detect.Yes, "enabled-runtime": detect.Yes, "disabled": detect.Yes,
		"linked": detect.Yes, "linked-runtime": detect.Yes, "indirect": detect.Yes, "generated": detect.Yes,
		"transient": detect.Yes, "alias": detect.Yes,
	}
)

// checkApt reads apt's daily job. Upgrades run when unattended-upgrades
// is installed, APT::Periodic::Enable is not 0, APT::Periodic::
// Unattended-Upgrade is on, an origin is allowed and apt-daily-upgrade.timer
// runs; otherwise the line is NotConfigured. With upgrades running, it is
// Partial while the lists are not refreshed (APT::Periodic::
// Update-Package-Lists off, or apt-daily.timer not running, or
// apt-daily.service masked) or a running timer is not enabled for the
// next boot. A masked apt-daily-upgrade.service is NotConfigured too: its
// timer runs, and starts nothing. With upgrades running, Proxmox VE
// carries CodeProxmoxNotUpgraded unless an origin matches Proxmox VE's
// repository, and a derivative hardn does not list is Unknown with
// CodeOriginsUnknown: whether its origins match its own archives is not
// read.
func checkApt(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	var ev []string
	d, err := dump(ctx, sys)
	enable, upgrade, lists, origins := fact{err: err}, fact{err: err}, fact{err: err}, fact{err: err}
	var allowed, patterns []string
	if err == nil {
		enable = d.flag(keyEnable, &ev)
		upgrade, lists = d.interval(keyUpgrade, &ev), d.interval(keyLists, &ev)
		allowed, patterns = d.children(keyAllowed), d.children(keyPattern)
		ev = append(ev, fmt.Sprintf("%s: Allowed-Origins %d, Origins-Pattern %d", dumpSource, len(allowed), len(patterns)))
		origins = known(len(allowed)+len(patterns) > 0)
	}
	t, err := detect.DpkgInstalled(ctx, sys, unattended)
	pkg := fact{t: t, err: wrap(err)}
	if t != detect.Unknown {
		ev = append(ev, "dpkg-query --show "+unattended+": "+map[detect.Tri]string{detect.Yes: "installed", detect.No: "not installed"}[t])
	}
	up, refresh := timers(ctx, sys, f.Init, &ev)
	st, err := decide(ev,
		[]cond{{pkg, DetailNoUnattended}, {enable, DetailUpgradesOff}, {upgrade, DetailUpgradesOff}, {origins, DetailNoOrigins}, {up.now, DetailTimerOff}, {up.job, DetailJobMasked}},
		[]cond{{lists, DetailListsStale}, {refresh.now, DetailListsStale}, {refresh.job, DetailListsStale}, {up.boot, DetailOffAfterRestart}, {refresh.boot, DetailOffAfterRestart}},
		DetailUnattended)
	if err != nil || (st.State != engine.Configured && st.State != engine.Partial) {
		return st, err
	}
	switch f.Platform.Distro {
	case detect.DistroProxmox:
		n := 0
		for _, o := range allowed {
			if p, ok := allowedPattern(o); ok && coversProxmox(p, f.OS.VersionCodename) {
				n++
			}
		}
		for _, p := range patterns {
			if coversProxmox(p, f.OS.VersionCodename) {
				n++
			}
		}
		st.Evidence = append(st.Evidence, fmt.Sprintf("%s: origins matching Proxmox VE's repository %d", dumpSource, n))
		if n == 0 {
			st.Findings = append(st.Findings, engine.Finding{Code: CodeProxmoxNotUpgraded, Detail: "Proxmox packages not upgraded", Source: dumpSource})
		}
	case detect.DistroOther:
		// apt's job runs, and what it takes is the distribution's own
		// archives' Origin against the origins, which hardn does not know
		// for a distribution it does not list (Raspbian's is not Debian's).
		why := engine.Finding{Code: CodeOriginsUnknown, Detail: fmt.Sprintf("%s, like %s", f.OS.ID, f.Platform.Like), Source: osReleaseSource(f.OS.Path)}
		return engine.Status{State: engine.Unknown, Detail: DetailCantRead, Evidence: st.Evidence, Findings: append(st.Findings, why)}, nil
	case detect.DistroUnknown:
		st.Evidence = append(st.Evidence, "Proxmox VE: not established, origins not compared")
	}
	return st, nil
}

// osReleaseSource is the os-release file read, or its name when none was.
func osReleaseSource(file string) string {
	if file == "" {
		return "os-release"
	}
	return file
}

// dumped is apt-config dump's output: each item's value and each list's
// items, by the name in lower case.
type dumped struct {
	values map[string]string
	lists  map[string][]string
}

// dump runs apt-config dump for dumpArgs. Its environment is host.Real's,
// so APT_CONFIG is unset and the files apt reads are the host's own.
func dump(ctx context.Context, sys host.System) (dumped, error) {
	res, err := sys.Run(ctx, "apt-config", dumpArgs...)
	if err != nil {
		return dumped{}, wrap(err)
	}
	d, err := parseDump(res.Stdout)
	return d, wrap(err)
}

// parseDump reads apt-config dump's default format, `%F "%v";` a line
// (apt-pkg Configuration::Dump), each name one of dumpArgs' roots or
// under one. A value is printed as it is, and apt.conf allows no quote
// and no line break in one (apt.conf(5)); a line in another form (a
// Dump::Format of the host's, say), a name outside the roots, or an item
// printed twice, is not established.
func parseDump(out []byte) (dumped, error) {
	d := dumped{values: map[string]string{}, lists: map[string][]string{}}
	text := strings.TrimSuffix(string(out), "\n")
	if text == "" {
		return d, nil // none of the subtrees is set
	}
	for _, line := range strings.Split(text, "\n") {
		name, rest, ok := strings.Cut(line, ` "`)
		value, closed := strings.CutSuffix(rest, `";`)
		if !ok || !closed || name == "" || strings.ContainsAny(name, " \t\"") || strings.Contains(value, `"`) {
			return d, fmt.Errorf("%s: line %q: %w", dumpSource, line, detect.ErrUnrecognised)
		}
		name = strings.ToLower(name)
		if !slices.ContainsFunc(dumpArgs[1:], func(root string) bool {
			root = strings.ToLower(root)
			return name == root || strings.HasPrefix(name, root+"::")
		}) {
			return d, fmt.Errorf("%s: line %q: not under %s: %w", dumpSource, line, strings.Join(dumpArgs[1:], ", "), detect.ErrUnrecognised)
		}
		if list, item := strings.CutSuffix(name, "::"); item {
			d.lists[list] = append(d.lists[list], value)
			continue
		}
		if _, twice := d.values[name]; twice {
			return d, fmt.Errorf("%s: %s printed twice: %w", dumpSource, name, detect.ErrUnrecognised)
		}
		d.values[name] = value
	}
	return d, nil
}

// lookup is name's value, and whether it is set; its evidence is the
// item as printed, or that it is unset.
func (d dumped) lookup(name string, ev *[]string) (string, bool) {
	v, set := d.values[strings.ToLower(name)]
	if set {
		*ev = append(*ev, fmt.Sprintf("%s: %s %q", dumpSource, name, v))
	} else {
		*ev = append(*ev, fmt.Sprintf("%s: %s unset", dumpSource, name))
	}
	return v, set
}

// flag reads APT::Periodic::Enable as apt.systemd.daily does: unset is 1,
// and the job exits when `[ $AutoAptEnable -eq 0 ]`. A decimal is ranked
// (0, or 00, is off); another form is not.
func (d dumped) flag(name string, ev *[]string) fact {
	v, set := d.lookup(name, ev)
	switch {
	case !set:
		return known(true)
	case decimal(v):
		return known(strings.Trim(v, "0") != "")
	}
	return notRanked(name, v)
}

// interval reads an APT::Periodic interval as check_stamp does: unset is
// "0", and "0" never runs; "always" runs at every start of the job, and a
// decimal with no unit (days) or with s, m, h or d runs once that period
// has passed ("0d" and "00" at every start). Another form is not ranked.
func (d dumped) interval(name string, ev *[]string) fact {
	v, set := d.lookup(name, ev)
	switch n := strings.TrimRight(v, "smhd"); {
	case !set, v == "0":
		return known(false)
	case v == "always", decimal(n) && len(v)-len(n) <= 1:
		return known(true)
	}
	return notRanked(name, v)
}

// children is what unattended-upgrade reads of root's origins
// (get_allowed_origins, python-apt's value_list): the values of root's
// direct children, items and named children alike, in no order of note,
// but the empty ones, which match nothing (match_whitelist_string).
func (d dumped) children(root string) []string {
	root = strings.ToLower(root)
	var named []string
	for name, v := range d.values {
		if seg, ok := strings.CutPrefix(name, root+"::"); ok && !strings.Contains(seg, "::") && strings.TrimSpace(v) != "" {
			named = append(named, v)
		}
	}
	slices.Sort(named)
	items := slices.DeleteFunc(slices.Clone(d.lists[root]), func(v string) bool { return strings.TrimSpace(v) == "" })
	return append(items, named...)
}

// proxmoxRelease is Proxmox VE 9's repository as its Release file says
// (download.proxmox.com debian/pve dists/trixie/Release, read 2026-10-20),
// by the keys of unattended-upgrade's patterns; its codename is the
// host's. Debian's default origins match none of it (P-43 (6) decision 5).
var proxmoxRelease = map[string]string{
	"o": "Proxmox", "origin": "Proxmox",
	"l": "Proxmox Debian Repository", "label": "Proxmox Debian Repository",
	"a": "stable", "suite": "stable", "archive": "stable",
}

// coversProxmox is whether an Origins-Pattern entry matches Proxmox VE's
// repository as unattended-upgrade matches one (match_whitelist_string):
// every comma-separated key=value token, its value an fnmatch pattern,
// with ${distro_codename} the host's codename. A token it does not model
// (c, site, another variable, an escape, a bracket) is not a match, so
// the finding stays.
func coversProxmox(pattern, codename string) bool {
	if codename != "" {
		pattern = strings.ReplaceAll(pattern, "${distro_codename}", codename)
	}
	if strings.TrimSpace(pattern) == "" || strings.ContainsAny(pattern, `$\[`) {
		return false
	}
	for _, tok := range strings.Split(pattern, ",") {
		key, val, ok := strings.Cut(tok, "=")
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		want, modelled := proxmoxRelease[key]
		if key == "n" || key == "codename" {
			want, modelled = codename, codename != ""
		}
		if m, err := path.Match(val, want); !ok || !modelled || strings.Contains(val, "=") || err != nil || !m {
			return false
		}
	}
	return true
}

// allowedPattern is an Allowed-Origins entry as unattended-upgrade
// converts it (get_allowed_origins_legacy): "origin:archive", or the two
// words, as "o=origin,a=archive"; ok is false for a form it is not.
func allowedPattern(entry string) (string, bool) {
	o, a, colon := strings.Cut(entry, ":")
	switch {
	case strings.ContainsAny(entry, `\,`):
		return "", false
	case colon && !strings.Contains(a, ":"):
	case !colon && len(strings.Fields(entry)) == 2:
		o, a = strings.Fields(entry)[0], strings.Fields(entry)[1]
	default:
		return "", false
	}
	return "o=" + o + ",a=" + a, true
}

func decimal(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// notRanked is a value apt documents no reading for, read: Unknown with
// the finding naming it.
func notRanked(name, v string) fact {
	short := name[strings.LastIndex(name, "::")+2:]
	return fact{why: engine.Finding{Code: CodeAptValueUnknown, Detail: fmt.Sprintf("%s %q", short, v), Source: dumpSource}}
}

// schedule is one of apt's timers: whether it runs now and starts at the
// next boot, and whether the service it starts can be started.
type schedule struct{ now, boot, job fact }

// timers reads apt-daily-upgrade's and apt-daily's timers and services,
// on a host systemd booted. On another init system apt's cron job,
// apt-compat, runs the job instead, which is not read: a finding says so.
// An init system not established is its error.
func timers(ctx context.Context, sys host.System, in detect.Init, ev *[]string) (up, refresh schedule) {
	switch {
	case in.Kind == detect.Systemd && in.Running == detect.Yes:
		return timer(ctx, sys, upgradeJob, ev), timer(ctx, sys, listsJob, ev)
	case in.Kind != detect.InitUnknown:
		*ev = append(*ev, fmt.Sprintf("init: %s, running %s: no systemd timers", in.Kind, in.Running))
		why := fact{why: engine.Finding{Code: CodeScheduleUnknown, Detail: "no systemd timers", Source: in.Source}}
		return schedule{why, why, why}, schedule{why, why, why}
	}
	why := fact{err: fmt.Errorf("updates: init system: %w", errOr(in.Err, detect.ErrNoSignal))}
	return schedule{why, why, why}, schedule{why, why, why}
}

// timer reads name's timer (systemctl is-active, then is-enabled) and
// whether its service is masked (is-enabled). A read that fails is its
// error, with the finding a line decided without it carries.
func timer(ctx context.Context, sys host.System, name string, ev *[]string) schedule {
	read := func(verb, unit string, ranks map[string]detect.Tri) fact {
		get := detect.SystemctlIsEnabled
		if verb == "is-active" {
			get = detect.SystemctlIsActive
		}
		cmd := "systemctl " + verb + " " + unit
		st, err := get(ctx, sys, unit)
		if err != nil {
			return fact{err: wrap(err), why: engine.Finding{Code: CodeScheduleUnknown, Detail: "can't read " + unit, Source: cmd}}
		}
		*ev = append(*ev, cmd+": "+st)
		if t, ok := ranks[st]; ok {
			return fact{t: t}
		}
		return fact{why: engine.Finding{Code: CodeScheduleUnknown, Detail: unit + " " + st, Source: cmd}}
	}
	return schedule{
		now:  read("is-active", name+".timer", running),
		boot: read("is-enabled", name+".timer", atBoot),
		job:  read("is-enabled", name+".service", startable),
	}
}

// wrap prefixes a read's error with the package's name; nil stays nil.
func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("updates: %w", err)
}

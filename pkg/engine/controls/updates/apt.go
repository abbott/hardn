package updates

import (
	"context"
	"fmt"
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
// Unattended-Upgrade is on and apt-daily-upgrade.timer runs; otherwise the
// line is NotConfigured. With upgrades running, it is Partial while the
// lists are not refreshed (APT::Periodic::Update-Package-Lists off, or
// apt-daily.timer not running, or apt-daily.service masked) or a running
// timer is not enabled for the next boot. A masked apt-daily-upgrade.service
// is NotConfigured too: its timer runs, and starts nothing. On Proxmox VE a running job carries CodeProxmoxNotUpgraded
// unless an origin names Proxmox.
func checkApt(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	var ev []string
	d, err := dump(ctx, sys)
	enable, upgrade, lists := fact{err: err}, fact{err: err}, fact{err: err}
	if err == nil {
		enable = d.flag(keyEnable, &ev)
		upgrade, lists = d.interval(keyUpgrade, &ev), d.interval(keyLists, &ev)
	}
	t, err := detect.DpkgInstalled(ctx, sys, unattended)
	pkg := fact{t: t, err: wrap(err)}
	if t != detect.Unknown {
		ev = append(ev, "dpkg-query --show "+unattended+": "+map[detect.Tri]string{detect.Yes: "installed", detect.No: "not installed"}[t])
	}
	up, refresh := timers(ctx, sys, f.Init, &ev)
	st, err := decide(ev,
		[]cond{{pkg, DetailNoUnattended}, {enable, DetailUpgradesOff}, {upgrade, DetailUpgradesOff}, {up.now, DetailTimerOff}, {up.job, DetailJobMasked}},
		[]cond{{lists, DetailListsStale}, {refresh.now, DetailListsStale}, {refresh.job, DetailListsStale}, {up.boot, DetailOffAfterRestart}, {refresh.boot, DetailOffAfterRestart}},
		DetailUnattended)
	if err != nil || (st.State != engine.Configured && st.State != engine.Partial) {
		return st, err
	}
	switch f.Platform.Distro {
	case detect.DistroProxmox:
		n, named := d.origins()
		if named {
			st.Evidence = append(st.Evidence, fmt.Sprintf("%s: %d origins, one naming Proxmox", dumpSource, n))
			break
		}
		st.Evidence = append(st.Evidence, fmt.Sprintf("%s: %d origins, none naming Proxmox", dumpSource, n))
		st.Findings = append(st.Findings, engine.Finding{Code: CodeProxmoxNotUpgraded, Detail: "Proxmox packages not upgraded", Source: dumpSource})
	case detect.DistroUnknown:
		st.Evidence = append(st.Evidence, "Proxmox VE: not established, origins not compared")
	}
	return st, nil
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
// (apt-pkg Configuration::Dump). A value is printed as it is, and apt.conf
// allows no quote and no line break in one (apt.conf(5)); a line in
// another form, or an item printed twice, is not established.
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

// origins counts unattended-upgrades' origins (Origins-Pattern and
// Allowed-Origins), and whether one names Proxmox, whose packages'
// Release files say Origin: Proxmox (P-43 (6) decision 5). The name is
// not matched as unattended-upgrades matches a pattern.
func (d dumped) origins() (n int, proxmox bool) {
	all := slices.Concat(d.lists[strings.ToLower(keyPattern)], d.lists[strings.ToLower(keyAllowed)])
	return len(all), slices.ContainsFunc(all, func(o string) bool { return strings.Contains(o, "Proxmox") })
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
// whether its service is masked (is-enabled).
func timer(ctx context.Context, sys host.System, name string, ev *[]string) schedule {
	read := func(verb, unit string, ranks map[string]detect.Tri) fact {
		get := detect.SystemctlIsEnabled
		if verb == "is-active" {
			get = detect.SystemctlIsActive
		}
		st, err := get(ctx, sys, unit)
		if err != nil {
			return fact{err: wrap(err)}
		}
		cmd := "systemctl " + verb + " " + unit
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

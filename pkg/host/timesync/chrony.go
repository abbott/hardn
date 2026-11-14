package timesync

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// chrony's files (gallery-timesync.md §3.2).
const (
	// ChronyConfDebian is chrony's main file on Debian, Ubuntu, Proxmox VE
	// and Alpine; ChronyConfEL on Rocky Linux, AlmaLinux and RHEL.
	ChronyConfDebian = "/etc/chrony/chrony.conf"
	ChronyConfEL     = "/etc/chrony.conf"
	// SourcesDir is the directory of ".sources" files Debian's and Ubuntu's
	// main file reads, and ChronySources hardn's own file there.
	SourcesDir    = "/etc/chrony/sources.d"
	ChronySources = SourcesDir + "/hardn.sources"
	// ChronyDHCP is where the DHCP hooks write the servers a lease names.
	ChronyDHCP = "/run/chrony-dhcp"
	// Off opens a line of the host's own that hardn switched off: the line
	// follows, as it was.
	Off = "#hardn: "
	// Added opens the comment over hardn's lines in chrony's main file,
	// which says how many they are.
	Added = "# Added by hardn (Time Sync): the "

	chronyDepth = 8 // files that name files, at most
)

// DistroSources are the files of SourcesDir a distribution's package
// installs, by base name: their servers are the service's own, which
// hardn's take the place of. Ubuntu 26.04's alone so far.
var DistroSources = []string{"ubuntu-ntp-pools.sources"}

// ReloadSources is the command that has chronyd read its ".sources" files
// again, with no restart.
var ReloadSources = []string{"chronyc", "reload", "sources"}

// source reports whether line names a time source (server, pool or peer,
// in any case, as chronyd reads them) and its name, "" where the line has
// none; nts is its nts option.
func source(line string) (name string, nts, ok bool) {
	f := strings.Fields(line)
	if len(f) == 0 {
		return "", false, false
	}
	switch strings.ToLower(f[0]) {
	case "server", "pool", "peer":
	default:
		return "", false, false
	}
	if len(f) > 1 {
		name = f[1]
	}
	for _, o := range f[min(len(f), 2):] {
		nts = nts || strings.EqualFold(o, "nts")
	}
	return name, nts, true
}

// added reads hardn's comment over its lines: how many they are; ok is
// false for any other line.
func added(line string) (n int, ok bool) {
	rest, ok := strings.CutPrefix(line, Added)
	if !ok {
		return 0, false
	}
	word, tail, _ := strings.Cut(rest, " ")
	n, err := strconv.Atoi(word)
	if err != nil || n < 1 || n > MaxServers || tail != addedTail(n) {
		return 0, false
	}
	return n, true
}

// addedTail ends hardn's comment over n lines.
func addedTail(n int) string {
	if n == 1 {
		return "line below."
	}
	return "lines below."
}

// ownLine is hardn's line for a server, and ownName the server of a line
// that is exactly one.
func ownLine(server string) string { return "server " + server + " iburst" }

func ownName(line string) (string, bool) {
	f := strings.Fields(line)
	if len(f) != 3 || line != ownLine(f[1]) || check(f[1:2]) != nil {
		return "", false
	}
	return f[1], true
}

// chronyMain is chrony's main file on this distribution: the first of the
// two that is there, the distribution's own first.
func chronyMain(ctx context.Context, sys host.System, f detect.Facts) (string, error) {
	names := []string{ChronyConfDebian, ChronyConfEL}
	if f.Platform.Distro.EL() {
		slices.Reverse(names)
	}
	for _, name := range names {
		if _, ok, err := readFile(ctx, sys, name); err != nil || ok {
			return name, err
		}
	}
	return "", fmt.Errorf("timesync: %s: no such file", names[0])
}

// readChrony reads chrony's servers: every server, pool and peer line of
// its main file and of each file that names: sourcedir's ".sources" files,
// confdir's ".conf" files, include's. A line is hardn's in hardn's own file
// and under hardn's comment in the main file; DHCP's in ChronyDHCP; the
// service's own in the main file and in a distribution's file
// (DistroSources); any other file's is FromOther. A line hardn switched off
// (Off) is a server to put back.
func (r *Reading) readChrony(ctx context.Context, sys host.System, f detect.Facts) error {
	main, err := chronyMain(ctx, sys, f)
	if err != nil {
		return err
	}
	r.Main = main
	return r.chronyFile(ctx, sys, main, false, 0)
}

// chronyFile reads one file; sources says it is a ".sources" file, which
// holds sources alone.
func (r *Reading) chronyFile(ctx context.Context, sys host.System, name string, sources bool, depth int) error {
	if depth > chronyDepth {
		return bad(name, 0, "files that name files, too deep")
	}
	data, ok, err := readFile(ctx, sys, name)
	if err != nil || !ok {
		return err
	}
	from, own := FromOther, false
	switch {
	case name == r.Main, path.Dir(name) == SourcesDir && slices.Contains(DistroSources, path.Base(name)):
		from, own = FromFile, true
	case name == ChronySources && strings.HasPrefix(string(data), Header):
		from, r.File, r.Mine = FromHardn, name, true
	case path.Dir(name) == ChronyDHCP:
		from = FromDHCP
	}
	mine := 0 // lines of hardn's still to come, under its comment
	for i, line := range strings.Split(string(data), "\n") {
		if n, ok := added(line); ok && name == r.Main {
			if mine > 0 || r.File == name {
				return bad(name, i+1, "hardn's comment, twice")
			}
			mine, r.File, r.Mine = n, name, true
			continue
		}
		if mine > 0 {
			server, ok := ownName(line)
			if !ok {
				return bad(name, i+1, "not the line hardn's comment names")
			}
			r.Servers = append(r.Servers, Server{Name: server, From: FromHardn, File: name})
			mine--
			continue
		}
		if rest, ok := strings.CutPrefix(line, Off); ok && own {
			if server, nts, ok := source(rest); ok && server != "" {
				r.Restores, r.Mine = append(r.Restores, Server{Name: server, From: FromFile, File: name, NTS: nts}), true
			}
			continue
		}
		if server, nts, ok := source(line); ok {
			if server == "" {
				return bad(name, i+1, "a server with no name")
			}
			r.Servers = append(r.Servers, Server{Name: server, From: from, File: name, NTS: nts})
			continue
		}
		fields := strings.Fields(line)
		if sources || len(fields) < 2 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "sourcedir":
			for _, dir := range fields[1:] {
				r.DropIn = r.DropIn || path.Clean(dir) == SourcesDir
				if err == nil {
					err = r.chronyDir(ctx, sys, dir, "*.sources", true, depth)
				}
			}
		case "confdir":
			for _, dir := range fields[1:] {
				if err == nil {
					err = r.chronyDir(ctx, sys, dir, "*.conf", false, depth)
				}
			}
		case "include":
			if strings.ContainsAny(path.Dir(fields[1]), "*?[") {
				return bad(name, i+1, "an include hardn does not follow")
			}
			err = r.chronyDir(ctx, sys, path.Dir(fields[1]), path.Base(fields[1]), false, depth)
		}
		if err != nil {
			return err
		}
	}
	if mine > 0 {
		return bad(name, 0, "fewer lines than hardn's comment names")
	}
	return nil
}

// chronyDir reads the files of dir whose names match pattern, in name
// order. A directory that is not there holds nothing.
func (r *Reading) chronyDir(ctx context.Context, sys host.System, dir, pattern string, sources bool, depth int) error {
	if !path.IsAbs(dir) {
		return bad(dir, 0, "not an absolute path")
	}
	entries, err := sys.ReadDir(ctx, dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("timesync: %w", err)
	}
	for _, e := range entries {
		if ok, err := path.Match(pattern, e.Name()); err != nil {
			return bad(dir, 0, "a pattern hardn does not read")
		} else if !ok || e.IsDir() {
			continue
		}
		if err := r.chronyFile(ctx, sys, path.Join(dir, e.Name()), sources, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// chronyClock asks chronyd: "chronyc -N tracking", whose "Leap status" is
// "Not synchronised" while the clock follows no server, and whose
// "Reference ID" names, in brackets, the server followed as chrony's files
// name it. A status hardn does not know is ClockUnknown.
func chronyClock(ctx context.Context, sys host.System) (Clock, string, error) {
	res, err := sys.Run(ctx, "chronyc", "-N", "tracking")
	if err != nil {
		return ClockUnknown, "", fmt.Errorf("timesync: chronyc tracking: %w", err)
	}
	var leap, synced string
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch k, v = strings.TrimSpace(k), strings.TrimSpace(v); k {
		case "Leap status":
			leap = v
		case "Reference ID":
			if i, j := strings.Index(v, "("), strings.LastIndex(v, ")"); i >= 0 && j > i {
				synced = v[i+1 : j]
			}
		}
	}
	switch leap {
	case "Not synchronised":
		return NotInSync, "", nil
	case "Normal", "Insert second", "Delete second":
		if synced == "?" {
			synced = ""
		}
		return InSync, synced, nil
	}
	return ClockUnknown, "", fmt.Errorf("timesync: chronyc tracking: no leap status: %w", detect.ErrUnrecognised)
}

// SourcesFile is hardn's own file of SourcesDir: Header, then a line a
// server. servers are checked (ErrServer).
func SourcesFile(servers []string) ([]byte, error) {
	if err := check(servers); err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString(Header)
	for _, s := range servers {
		b.WriteString(ownLine(s) + "\n")
	}
	return []byte(b.String()), nil
}

// ChronyOn is a chrony file without what ChronyOff did to it: hardn's
// comment and the lines it names taken out, and each server, pool or peer
// line hardn switched off (Off) as it was. A file with nothing of hardn's is returned as it
// is. A comment of hardn's that does not name exactly the lines under it
// is ErrMalformed: hardn takes out nothing it cannot identify.
func ChronyOn(name string, data []byte) ([]byte, error) {
	var out []string
	mine, seen := 0, false
	for i, line := range strings.Split(string(data), "\n") {
		if n, ok := added(line); ok {
			if seen {
				return nil, bad(name, i+1, "hardn's comment, twice")
			}
			mine, seen = n, true
			continue
		}
		if mine > 0 {
			if _, ok := ownName(line); !ok {
				return nil, bad(name, i+1, "not the line hardn's comment names")
			}
			mine--
			continue
		}
		if rest, ok := strings.CutPrefix(line, Off); ok {
			if _, _, ok := source(rest); ok {
				line = rest
			}
		}
		out = append(out, line)
	}
	if mine > 0 {
		return nil, bad(name, 0, "fewer lines than hardn's comment names")
	}
	return []byte(strings.Join(out, "\n")), nil
}

// ChronyOff is a chrony file with every server, pool and peer line of its
// own switched off: Off put before it, the line itself kept, so that
// ChronyOn puts it back byte for byte. With servers, hardn's lines are
// added under its comment, after the last line switched off, or at the
// file's end where there is none: for a main file that reads no directory
// hardn can write its own file in. What ChronyOff did before is undone
// first (ChronyOn), so a second set replaces the first. servers are checked
// (ErrServer).
func ChronyOff(name string, data []byte, servers []string) ([]byte, error) {
	if servers != nil {
		if err := check(servers); err != nil {
			return nil, err
		}
	}
	data, err := ChronyOn(name, data)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	at := len(lines)
	if lines[at-1] == "" && at > 1 {
		at-- // before the file's last line break
	}
	last := -1
	for i, line := range lines {
		if _, _, ok := source(line); ok {
			lines[i], last = Off+line, i
		}
	}
	if servers == nil {
		return []byte(strings.Join(lines, "\n")), nil
	}
	if last >= 0 {
		at = last + 1
	}
	block := []string{Added + strconv.Itoa(len(servers)) + " " + addedTail(len(servers))}
	for _, s := range servers {
		block = append(block, ownLine(s))
	}
	return []byte(strings.Join(slices.Insert(lines, at, block...), "\n")), nil
}

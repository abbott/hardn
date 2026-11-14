package timesync

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// systemd-timesyncd's files (gallery-timesync.md §3.3; timesyncd.conf(5)).
const (
	// TimesyncdDropIn is hardn's own file.
	TimesyncdDropIn = "/etc/systemd/timesyncd.conf.d/60-hardn.conf"

	timesyncdConf = "systemd/timesyncd.conf"
)

// timesyncdRoots are the directories systemd reads timesyncd.conf and its
// ".d" drop-ins from, the one that wins first.
var timesyncdRoots = []string{"/etc", "/run", "/usr/local/lib", "/usr/lib"}

// readTimesyncd reads the servers systemd-timesyncd is set to use: each
// NTP= of [Time] in timesyncd.conf, then in its drop-ins in name order, as
// systemd reads them: a value adds servers and an empty one takes away
// those before it. A server is hardn's in hardn's own file and FromOther in
// any other, the main file too. An empty NTP= of another drop-in read after
// hardn's own (after, by its name) is Cleared: it takes hardn's servers
// away, now or once they are written. Then, from a running service ("timedatectl
// show-timesync"), the server followed, the servers its links were handed
// (FromDHCP), and, where no other is known, the ones built in.
func (r *Reading) readTimesyncd(ctx context.Context, sys host.System) error {
	files, err := timesyncdFiles(ctx, sys)
	if err != nil {
		return err
	}
	for _, name := range files {
		data, ok, err := readFile(ctx, sys, name)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		from := FromOther
		if name == TimesyncdDropIn && strings.HasPrefix(string(data), Header) {
			from, r.File, r.Mine = FromHardn, name, true
		}
		section := ""
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "[") {
				section = line
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok || section != "[Time]" || strings.TrimSpace(k) != "NTP" {
				continue
			}
			names := strings.Fields(v)
			if len(names) == 0 {
				r.Servers = nil
				if from == FromOther && afterOwn(name) {
					r.Cleared = name
				}
			}
			for _, n := range names {
				r.Servers = append(r.Servers, Server{Name: n, From: from, File: name})
			}
		}
	}
	if len(r.Own()) == 0 {
		r.File = ""
	}
	if r.Running != detect.Yes {
		return nil
	}
	res, err := sys.Run(ctx, "timedatectl", "show-timesync")
	if err != nil {
		return fmt.Errorf("timesync: timedatectl show-timesync: %w", err)
	}
	p := properties(res.Stdout)
	r.Synced = p["ServerName"]
	for _, n := range strings.Fields(p["LinkNTPServers"]) {
		r.Servers = append(r.Servers, Server{Name: n, From: FromDHCP})
	}
	var builtIn []Server
	for _, n := range strings.Fields(p["FallbackNTPServers"]) {
		builtIn = append(builtIn, Server{Name: n, From: FromBuiltIn})
	}
	switch {
	case len(r.Servers) == 0:
		r.Servers = builtIn
	case len(r.Own()) > 0 && r.Outside() == nil:
		r.Restores = builtIn
	}
	return nil
}

// afterOwn reports whether name is a drop-in systemd reads after hardn's
// own: one of a timesyncd.conf.d whose base name sorts after its.
func afterOwn(name string) bool {
	return path.Base(path.Dir(name)) == path.Base(timesyncdConf)+".d" && path.Base(name) > path.Base(TimesyncdDropIn)
}

// timesyncdFiles are the files systemd-timesyncd reads, in its order: the
// first timesyncd.conf of timesyncdRoots, then every "*.conf" of their
// timesyncd.conf.d directories by name, a name in two of them read from the
// first.
func timesyncdFiles(ctx context.Context, sys host.System) ([]string, error) {
	var files []string
	for _, root := range timesyncdRoots {
		if _, ok, err := readFile(ctx, sys, path.Join(root, timesyncdConf)); err != nil {
			return nil, err
		} else if ok {
			files = append(files, path.Join(root, timesyncdConf))
			break
		}
	}
	dropIns := map[string]string{}
	var names []string
	for _, root := range timesyncdRoots {
		dir := path.Join(root, timesyncdConf+".d")
		entries, err := sys.ReadDir(ctx, dir)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return nil, fmt.Errorf("timesync: %w", err)
		}
		for _, e := range entries {
			if _, seen := dropIns[e.Name()]; seen || e.IsDir() || !strings.HasSuffix(e.Name(), ".conf") {
				continue
			}
			dropIns[e.Name()] = path.Join(dir, e.Name())
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	for _, n := range names {
		files = append(files, dropIns[n])
	}
	return files, nil
}

// TimesyncdFile is hardn's own drop-in: Header, then [Time] and NTP= with
// the servers, which systemd-timesyncd then uses in place of the ones built
// into it. servers are checked (ErrServer).
func TimesyncdFile(servers []string) ([]byte, error) {
	if err := check(servers); err != nil {
		return nil, err
	}
	return []byte(Header + "[Time]\nNTP=" + strings.Join(servers, " ") + "\n"), nil
}

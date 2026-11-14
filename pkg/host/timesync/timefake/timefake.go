// Package timefake models a host's time service for tests and the UI
// gallery: how the commands pkg/host/timesync runs answer, and which server
// the clock follows once the service reads its files again. It imports no
// fake host: a caller hands it the host's files (Sim.Read) and turns its
// answers (Response) into its own fake's. It is a model, not a host: the commands' output
// forms are the ones the survey gives (docs/engine/gallery-timesync.md §3).
package timefake

import (
	"strings"

	"github.com/abbott/hardn/pkg/host/timesync"
)

// Response is a command's answer: its output and its exit status.
type Response struct {
	Stdout, Stderr string
	ExitCode       int
}

// Sim is one host's time service.
type Sim struct {
	// Read reads a file of the host, which a restart or a reload reads.
	Read func(name string) ([]byte, error)
	// Service is the service's name under the host's init ("chrony",
	// "chronyd", "systemd-timesyncd", "ntpd"); Stopped says it is not
	// running. Any other name asked about is not running.
	Service string
	Stopped bool
	// Synced is the server the clock follows; "" is a clock not in sync.
	Synced string
	// Stock is the server followed on the service's own servers, and
	// Reachable the servers of hardn's that answer.
	Stock     string
	Reachable []string
	// Fallback and Link are systemd-timesyncd's built-in servers and the
	// ones its links were handed.
	Fallback, Link string
	// Packages, when set, answers dpkg-query and rpm -q: the installed
	// packages.
	Packages map[string]bool
	// Missing are commands that are not on the host, by their first word.
	Missing map[string]bool
	// FailRestarts is how many restarts and reloads fail, from the first.
	FailRestarts int
}

// settle is the service reading its files again: it follows the first
// reachable server hardn's setting names, none where that names no
// reachable one, and Stock without a setting of hardn's.
func (s *Sim) settle() {
	if s.Read == nil {
		return
	}
	for _, name := range []string{timesync.ChronySources, timesync.ChronyConfEL, timesync.ChronyConfDebian, timesync.TimesyncdDropIn, timesync.ConfD} {
		data, err := s.Read(name)
		text := string(data)
		if err != nil || !(strings.HasPrefix(text, timesync.Header) || strings.Contains(text, timesync.Added) || strings.Contains(text, timesync.Mark)) {
			continue
		}
		s.Synced = ""
		for _, r := range s.Reachable {
			if strings.Contains(text, r) {
				s.Synced = r
				break
			}
		}
		return
	}
	s.Synced = s.Stock
}

// units are the time services' names under systemd and OpenRC: Answer
// answers for no other service.
var units = map[string]bool{"chrony": true, "chronyd": true, "systemd-timesyncd": true, "ntpd": true}

// yesNo is b as timedatectl prints it.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// Answer answers a command of the time service's, and reports whether it
// is one: nil answers none.
func (s *Sim) Answer(argv []string) (Response, bool) {
	if s == nil {
		return Response{}, false
	}
	line, last := strings.Join(argv, " "), argv[len(argv)-1]
	known := argv[0] == "chronyc" || argv[0] == "timedatectl" || line == "busybox adjtimex"
	if s.Missing[argv[0]] && known {
		return Response{Stderr: argv[0] + ": not found\n", ExitCode: 127}, true
	}
	synced := s.Synced != "" && !s.Stopped
	switch {
	case s.Packages != nil && argv[0] == "dpkg-query":
		if s.Packages[last] {
			return Response{Stdout: "install ok installed\t1.0-1\n"}, true
		}
		return Response{Stderr: "dpkg-query: no packages found matching " + last + "\n", ExitCode: 1}, true
	case s.Packages != nil && line == "rpm -q "+last:
		if s.Packages[last] {
			return Response{Stdout: last + "-4.6.1-1.el10.x86_64\n"}, true
		}
		return Response{Stdout: "package " + last + " is not installed\n", ExitCode: 1}, true
	case line == "systemctl is-active "+last && units[last]:
		if last == s.Service && !s.Stopped {
			return Response{Stdout: "active\n"}, true
		}
		return Response{Stdout: "inactive\n", ExitCode: 3}, true
	case len(argv) == 3 && argv[0] == "rc-service" && argv[2] == "status" && units[argv[1]]:
		if argv[1] == s.Service && !s.Stopped {
			return Response{Stdout: " * status: started\n"}, true
		}
		return Response{Stdout: " * status: stopped\n", ExitCode: 3}, true
	case line == "chronyc -N tracking":
		if s.Stopped {
			return Response{Stderr: "506 Cannot talk to daemon\n", ExitCode: 1}, true
		}
		ref, leap := "00000000 ()", "Not synchronised"
		if synced {
			ref, leap = "C000020A ("+s.Synced+")", "Normal"
		}
		return Response{Stdout: "Reference ID    : " + ref + "\nStratum         : 3\nRef time (UTC)  : Sat Nov 14 12:00:00 2026\n" +
			"System time     : 0.000012345 seconds fast of NTP time\nUpdate interval : 64.2 seconds\nLeap status     : " + leap + "\n"}, true
	case line == "timedatectl show":
		return Response{Stdout: "Timezone=Etc/UTC\nLocalRTC=no\nCanNTP=yes\nNTP=yes\nNTPSynchronized=" + yesNo(synced) + "\nTimeUSec=Sat 2026-11-14 12:00:00 UTC\n"}, true
	case line == "timedatectl show-timesync":
		out := "FallbackNTPServers=" + s.Fallback + "\n"
		if s.Link != "" {
			out = "LinkNTPServers=" + s.Link + "\n" + out
		}
		if synced {
			out += "ServerName=" + s.Synced + "\nServerAddress=192.0.2.10\n"
		}
		return Response{Stdout: out + "RootDistanceMaxUSec=5s\nPollIntervalMinUSec=32s\n"}, true
	case line == "busybox adjtimex":
		status, ret := "8193 (PLL | NANO)", "0 (clock synchronized)"
		if !synced {
			status, ret = "64 (UNSYNC)", "5 (clock not synchronized)"
		}
		return Response{Stdout: "    mode:         0\n-o  offset:       0 us\n-f  freq.adjust:  0 (65536 = 1ppm)\n    maxerror:     16000000\n" +
			"    esterror:     16000000\n    status:       " + status + "\n-p  timeconstant: 2\n    precision:    1 us\n    tolerance:    32768000\n" +
			"-t  tick:         10000 us\n    time.tv_sec:  1794657600\n    time.tv_usec: 0\n    return value: " + ret + "\n"}, true
	case line == "chronyc reload sources", line == "systemctl restart "+last && last == s.Service, line == "rc-service "+s.Service+" restart":
		if s.FailRestarts > 0 {
			s.FailRestarts--
			return Response{Stderr: "failed\n", ExitCode: 1}, true
		}
		s.Stopped = false
		s.settle()
		return Response{}, true
	}
	return Response{}, false
}

// The services' files as each distribution's package installs them, as far
// as the survey read them (gallery-timesync.md §3.2 and §3.4).
const (
	// UbuntuConf is what matters of Ubuntu 26.04's /etc/chrony/chrony.conf:
	// no pool, and the directories it reads.
	UbuntuConf = "confdir /etc/chrony/conf.d\n\n# Use time sources from DHCP.\nsourcedir /run/chrony-dhcp\n\n" +
		"# Use NTP sources found in /etc/chrony/sources.d.\nsourcedir /etc/chrony/sources.d\n\ndriftfile /var/lib/chrony/chrony.drift\nmakestep 1 3\n"
	// UbuntuPools is Ubuntu 26.04's ubuntu-ntp-pools.sources, to its last
	// pool: the lines after it are comments.
	UbuntuPools = "# Use NTS by default\n" +
		"# NTS uses an additional port to negotiate security: 4460/tcp\n" +
		"# The normal NTP port remains in use: 123/udp\n" +
		"pool 1.ntp.ubuntu.com iburst maxsources 1 nts prefer\n" +
		"pool 2.ntp.ubuntu.com iburst maxsources 1 nts prefer\n" +
		"pool 3.ntp.ubuntu.com iburst maxsources 1 nts prefer\n" +
		"pool 4.ntp.ubuntu.com iburst maxsources 1 nts prefer\n" +
		"# The bootstrap server is needed by systems without a hardware clock, or a very\n" +
		"# large initial clock offset. The specified certificate set is defined in\n" +
		"# /etc/chrony/conf.d/ubuntu-nts.conf.\n" +
		"pool ntp-bootstrap.ubuntu.com iburst maxsources 1 nts certset 1\n"
	// UbuntuPoolsFile is where UbuntuPools is.
	UbuntuPoolsFile = timesync.SourcesDir + "/ubuntu-ntp-pools.sources"
	// DebianConf is what matters of Debian 13's /etc/chrony/chrony.conf: the
	// pool in the main file, and the directories it reads.
	DebianConf = "confdir /etc/chrony/conf.d\n\n# Use Debian vendor zone.\npool 2.debian.pool.ntp.org iburst\n\n" +
		"# Use time sources from DHCP.\nsourcedir /run/chrony-dhcp\n\n# Use NTP sources found in /etc/chrony/sources.d.\nsourcedir /etc/chrony/sources.d\n\nmakestep 1 3\n"
	// ELConf is the head of /etc/chrony.conf as CentOS Stream's package
	// builds it from chrony's example; the pool's name is as that build
	// would make it on Rocky Linux.
	ELConf = "# Use four public NTP servers from the pool.ntp.org project. If this\n" +
		"# host has a static public IP address, please consider joining the pool:\n" +
		"# https://www.ntppool.org/join.html\n" +
		"pool 2.rocky.pool.ntp.org iburst\n" +
		"\n" +
		"# Use NTP servers from DHCP.\n" +
		"sourcedir /run/chrony-dhcp\n"
	// AlpineConfD is Alpine's /etc/conf.d/ntpd as busybox's package
	// installs it.
	AlpineConfD = "# By default ntpd runs as a client. Add -l to run as a server on port 123.\nNTPD_OPTS=\"-N -p pool.ntp.org\"\n"
)

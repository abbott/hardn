// Package sysctlfake is a model of a host's kernel settings and its sysctl
// tool, for tests and the dev tools: the values a host's /proc/sys holds
// (Values, Files), and what "sysctl -p FILE" and "sysctl -w KEY=VALUE" do
// to them (Sim). procps's and busybox's behaviour are as read on hosts on
// 2026-11-14 (docs/engine/gallery-sysctl.md §3): procps reads a "*" in a
// key as every name it matches and exits 1 for a key it cannot set;
// busybox reads no "*", says so on stderr, and exits 0 either way. The
// words of a refused write are the model's own.
package sysctlfake

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/sysctl"
)

// Where the tools are: procps's binary, and busybox's link.
const (
	ProcpsPath  = "/usr/sbin/sysctl"
	BusyboxLink = "/sbin/sysctl"
	BusyboxPath = "/bin/busybox"
)

// Values are a host's keys: every key of the baseline at it but Sending
// redirects, on on every host read, for all, default, lo and ifaces; then
// over's, where a "*" for the interface stands for every name, and a key
// naming one interface goes over it.
func Values(ifaces []string, over map[string]int) map[string]int {
	out := map[string]int{
		"net.ipv4.ip_forward":                        0,
		"net.ipv4.tcp_syncookies":                    1,
		"net.ipv4.icmp_echo_ignore_broadcasts":       1,
		"net.ipv4.icmp_ignore_bogus_error_responses": 1,
		"kernel.kptr_restrict":                       1,
		"kernel.dmesg_restrict":                      1,
		"kernel.randomize_va_space":                  2,
		"kernel.yama.ptrace_scope":                   1,
		"kernel.unprivileged_bpf_disabled":           2,
		"kernel.perf_event_paranoid":                 2,
		"fs.protected_hardlinks":                     1,
		"fs.protected_symlinks":                      1,
		"fs.protected_fifos":                         1,
		"fs.protected_regular":                       1,
	}
	names := append([]string{"all", "default", "lo"}, ifaces...)
	for _, n := range names {
		out["net.ipv4.conf."+n+".accept_redirects"] = 0
		out["net.ipv4.conf."+n+".send_redirects"] = 1
		out["net.ipv4.conf."+n+".rp_filter"] = 1
		out["net.ipv4.conf."+n+".accept_source_route"] = 0
		out["net.ipv4.conf."+n+".forwarding"] = 0
		out["net.ipv6.conf."+n+".accept_redirects"] = 0
		out["net.ipv6.conf."+n+".accept_source_route"] = 0
		out["net.ipv6.conf."+n+".forwarding"] = 0
		out["net.ipv6.conf."+n+".accept_ra"] = 1
	}
	// A key naming one interface goes over a key naming every one.
	for k, v := range over {
		if strings.Contains(k, ".*.") {
			for _, n := range names {
				out[strings.Replace(k, ".*.", "."+n+".", 1)] = v
			}
		}
	}
	for k, v := range over {
		if !strings.Contains(k, ".*.") {
			out[k] = v
		}
	}
	return out
}

// Files are values as /proc/sys's files, by path.
func Files(values map[string]int) map[string]string {
	out := map[string]string{}
	for k, v := range values {
		out[sysctl.ProcPath(k)] = strconv.Itoa(v) + "\n"
	}
	return out
}

// Response is a command's outcome.
type Response struct {
	Stdout, Stderr string
	ExitCode       int
}

// Sim answers the sysctl tool's commands on Sys, whose /proc/sys it reads
// and writes.
type Sim struct {
	Sys host.System
	// Busybox: the tool is busybox's.
	Busybox bool
	// Refuse are keys the kernel refuses to change.
	Refuse map[string]bool
	// Quiet: a refused key is left as it is with nothing said, and the
	// exit status 0.
	Quiet bool
}

// Answer answers argv, and reports whether it is the tool's: "sysctl -p
// FILE" and "sysctl -w KEY=VALUE...".
func (s *Sim) Answer(argv []string) (Response, bool) {
	if len(argv) < 3 || argv[0] != sysctl.Command {
		return Response{}, false
	}
	ctx := context.Background()
	var r Response
	switch argv[1] {
	case "-p":
		data, err := s.Sys.ReadFile(ctx, argv[2])
		if err != nil {
			return Response{Stderr: "sysctl: cannot open \"" + argv[2] + "\": No such file or directory\n", ExitCode: 1}, true
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if k, v, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(line, "#") {
				s.set(ctx, &r, strings.TrimSpace(k), strings.TrimSpace(v))
			}
		}
	case "-w":
		for _, a := range argv[2:] {
			k, v, _ := strings.Cut(a, "=")
			s.set(ctx, &r, k, v)
		}
	default:
		return Response{}, false
	}
	return r, true
}

// set sets key to value as the tool does, saying what it does on r.
func (s *Sim) set(ctx context.Context, r *Response, key, value string) {
	keys := []string{key}
	if strings.Contains(key, "*") {
		if s.Busybox {
			r.Stderr += "sysctl: error: '" + strings.Replace(key, ".*.", ".*/", 1) + "' is an unknown key\n"
			return
		}
		keys = s.match(ctx, key)
	}
	for _, k := range keys {
		name := sysctl.ProcPath(k)
		_, err := s.Sys.ReadFile(ctx, name)
		switch {
		case err != nil && s.Busybox:
			r.Stderr += "sysctl: error: '" + strings.ReplaceAll(k, ".", "/") + "' is an unknown key\n"
		case err != nil:
			r.Stderr += "sysctl: cannot stat " + name + ": No such file or directory\n"
			r.ExitCode = 1
		case s.Refuse[k] && s.Quiet:
		case s.Refuse[k] && s.Busybox:
			r.Stderr += "sysctl: error setting key '" + k + "': Operation not permitted\n"
		case s.Refuse[k]:
			r.Stderr += "sysctl: setting key \"" + k + "\": Operation not permitted\n"
			r.ExitCode = 1
		default:
			if err := s.Sys.WriteFileAtomic(ctx, name, []byte(value+"\n"), 0o644, 0, 0); err != nil {
				r.Stderr += fmt.Sprintf("sysctl: %v\n", err)
				r.ExitCode = 1
				continue
			}
			r.Stdout += k + " = " + value + "\n"
		}
	}
}

// match are the keys pattern stands for: its "*" as each name of the
// directory it is in.
func (s *Sim) match(ctx context.Context, pattern string) []string {
	p := sysctl.ProcPath(pattern)
	dir, leaf, _ := strings.Cut(p, "/*/")
	entries, err := s.Sys.ReadDir(ctx, dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if _, err := s.Sys.Stat(ctx, path.Join(dir, e.Name(), leaf)); err == nil {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	var out []string
	for _, n := range names {
		out = append(out, strings.Replace(pattern, "*", strings.ReplaceAll(n, ".", "/"), 1))
	}
	return out
}

package details_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/engine/details"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// The listening sockets of the gallery's web-01, as ss prints them to root
// (reload's TestParseListening has the forms the VMs printed).
const (
	ssTCP = "LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:((\"sshd\",pid=612,fd=3))\n" +
		"LISTEN 0 128 [::]:22 [::]:* users:((\"sshd\",pid=612,fd=4))\n" +
		"LISTEN 0 244 127.0.0.1:5432 0.0.0.0:* users:((\"postgres\",pid=801,fd=6))\n" +
		"LISTEN 0 4096 *:9100 *:* users:((\"node_exporter\",pid=733,fd=3))\n"
	ssUDP = "UNCONN 0 0 192.0.2.20%eth0:68 0.0.0.0:* users:((\"systemd-network\",pid=593,fd=34))\n"
)

// TestPorts: the Listening Ports section's reading in each state: every socket
// with its program, in port order; as a user, the sockets with no program
// and Root No; no ss; an ss that fails, on either table; a line that is not
// read; a host with nothing listening.
func TestPorts(t *testing.T) {
	const asUser = "Name:\thardn\nUid:\t1000\t1000\t1000\t1000\nGid:\t1000\t1000\t1000\t1000\n"
	noProcess := "LISTEN 0 128 0.0.0.0:22 0.0.0.0:*\nLISTEN 0 128 [::]:22 [::]:*\n"
	for _, tc := range []struct {
		name     string
		tcp, udp hostfake.Response
		status   string
		want     []string
		check    func(details.Ports) bool
	}{
		{name: "root", tcp: hostfake.Response{Stdout: ssTCP}, udp: hostfake.Response{Stdout: ssUDP},
			want:  []string{"tcp 0.0.0.0:22 sshd", "tcp [::]:22 sshd", "udp 192.0.2.20%eth0:68 systemd-network", "tcp 127.0.0.1:5432 postgres", "tcp *:9100 node_exporter"},
			check: func(p details.Ports) bool { return p.Root == detect.Yes && !p.NoSS && !p.Partial && len(p.Unread) == 0 }},
		{name: "a user", tcp: hostfake.Response{Stdout: noProcess}, status: asUser,
			want:  []string{"tcp 0.0.0.0:22 ", "tcp [::]:22 "},
			check: func(p details.Ports) bool { return p.Root == detect.No && !p.Partial }},
		{name: "no ss", tcp: hostfake.Response{Err: fmt.Errorf("host: run ss: %w", host.ErrNotFound)},
			check: func(p details.Ports) bool { return p.NoSS && len(p.Unread) == 0 }},
		{name: "ss fails", tcp: hostfake.Response{Stderr: "ss: netlink error\n", ExitCode: 1},
			check: func(p details.Ports) bool {
				return !p.NoSS && len(p.Unread) == 1 && p.Unread[0].Source == details.SourceSS
			}},
		{name: "the UDP table fails", tcp: hostfake.Response{Stdout: ssTCP}, udp: hostfake.Response{Stderr: "ss: netlink error\n", ExitCode: 1},
			check: func(p details.Ports) bool { return len(p.Unread) == 1 }},
		{name: "a line not read", tcp: hostfake.Response{Stdout: "LISTEN 0 128 nowhere 0.0.0.0:*\n" + noProcess},
			want:  []string{"tcp 0.0.0.0:22 ", "tcp [::]:22 "},
			check: func(p details.Ports) bool { return p.Partial }},
		{name: "nothing listening", check: func(p details.Ports) bool { return !p.NoSS && !p.Partial && len(p.Unread) == 0 }},
	} {
		h := newHost(t)
		h.cmd["ss -Htlnp"], h.cmd["ss -Hulnp"] = tc.tcp, tc.udp
		if tc.status != "" {
			h.add(statusFile, tc.status)
		}
		p := details.Read(t.Context(), h, debianFacts()).Ports
		var got []string
		for _, s := range p.List {
			got = append(got, s.Proto+" "+s.String()+" "+s.Process)
		}
		if !slices.Equal(got, tc.want) || !tc.check(p) {
			t.Errorf("%s: %q, %+v", tc.name, got, p)
		}
	}
}

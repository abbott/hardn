package config

import (
	"strings"
	"testing"
)

// TestResolveHost: an IPv4 address or a host name, lowered; an IPv6 address
// and anything else each refused with its own error.
func TestResolveHost(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		isAddr   bool
		err      error
	}{
		{in: "192.0.2.50", want: "192.0.2.50", isAddr: true},
		{in: "Logs.Example.NET", want: "logs.example.net"},
		{in: "ntp1", want: "ntp1"},
		{in: "2001:db8::1", err: ErrHostIPv6},
		{in: "::ffff:192.0.2.1", err: ErrHostIPv6},
		{in: "", err: ErrHostName},
		{in: "-R", err: ErrHostName},
		{in: "log_host", err: ErrHostName},
		{in: "logs example", err: ErrHostName},
		{in: "logs.example.net.", err: ErrHostName},
		{in: "192.0.2.50.1", err: ErrHostName},
		{in: "a-.example", err: ErrHostName},
		{in: strings.Repeat("a.", 127) + "a", err: ErrHostName},
		{in: strings.Repeat("a", 64), err: ErrHostName},
	} {
		got, isAddr, err := ResolveHost(tc.in)
		if got != tc.want || isAddr != tc.isAddr || err != tc.err {
			t.Errorf("ResolveHost(%q) = %q, %v, %v; want %q, %v, %v", tc.in, got, isAddr, err, tc.want, tc.isAddr, tc.err)
		}
	}
}

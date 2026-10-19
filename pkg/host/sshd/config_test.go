package sshd

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

func TestParseInclude(t *testing.T) {
	header := "# synthetic header\n\n#Port 22\n"
	tests := []struct {
		name string
		main string
		want Include
	}{
		{"stock", header + IncludeLine + "\n\nPermitRootLogin yes\n", Include{IncludeFirst, 4, IncludeLine}},
		{"trailing whitespace", IncludeLine + " \t\r\n", Include{IncludeFirst, 1, IncludeLine}},
		{"CRLF", "# x\r\n" + IncludeLine + "\r\n", Include{IncludeFirst, 2, IncludeLine}},
		{"none", header + "Port 2230\n", Include{Mode: IncludeNone}},
		{"commented out", "#" + IncludeLine + "\nPort 2230\n", Include{Mode: IncludeNone}},
		{"empty", "", Include{Mode: IncludeNone}},
		{"not first", "Port 22\n" + IncludeLine + "\n", Include{IncludeOther, 2, IncludeLine}},
		{"lower case", "include /etc/ssh/sshd_config.d/*.conf\n", Include{IncludeOther, 1, "include /etc/ssh/sshd_config.d/*.conf"}},
		{"indented", "  " + IncludeLine + "\n", Include{IncludeOther, 1, "  " + IncludeLine}},
		{"tab", "Include\t/etc/ssh/sshd_config.d/*.conf\n", Include{IncludeOther, 1, "Include\t/etc/ssh/sshd_config.d/*.conf"}},
		{"equals", "Include=/etc/ssh/sshd_config.d/*.conf\n", Include{IncludeOther, 1, "Include=/etc/ssh/sshd_config.d/*.conf"}},
		{"quoted keyword", `"Include" /etc/ssh/sshd_config.d/*.conf` + "\n", Include{IncludeOther, 1, `"Include" /etc/ssh/sshd_config.d/*.conf`}},
		{"trailing comment", IncludeLine + " # stock\n", Include{IncludeOther, 1, IncludeLine + " # stock"}},
		{"other path", "Include /etc/ssh/extra.conf\n", Include{IncludeOther, 1, "Include /etc/ssh/extra.conf"}},
		{"spaced equals", "Include = /etc/ssh/sshd_config.d/*.conf\n", Include{IncludeOther, 1, "Include = /etc/ssh/sshd_config.d/*.conf"}},
		// sshd takes strdelim's second token when the first is empty.
		{"leading equals", "=" + IncludeLine + "\n", Include{IncludeOther, 1, "=" + IncludeLine}},
		{"leading empty quotes", `""` + IncludeLine + "\n", Include{IncludeOther, 1, `""` + IncludeLine}},
		{"equals before the stock line", "=Port 22\n" + IncludeLine + "\n", Include{IncludeOther, 2, IncludeLine}},
		// An unmatched quote makes strdelim return NULL: sshd skips the line.
		{"unmatched quote", `"` + IncludeLine + "\n", Include{Mode: IncludeNone}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseInclude([]byte(tt.main)); got != tt.want {
				t.Errorf("ParseInclude = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDetectInclude(t *testing.T) {
	f := hostfake.New(host.Options{})
	if _, err := DetectInclude(context.Background(), f); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing sshd_config: err = %v, want fs.ErrNotExist", err)
	}
	f.AddFile(MainConfig, []byte(IncludeLine+"\n"), 0o644, 0, 0)
	if inc, err := DetectInclude(context.Background(), f); err != nil || inc.Mode != IncludeFirst {
		t.Fatalf("DetectInclude = %+v, %v; want IncludeFirst", inc, err)
	}
}

func TestSplitArgs(t *testing.T) {
	tests := []struct {
		in   string
		want []string
		ok   bool
	}{
		{"User bob", []string{"User", "bob"}, true},
		{"  User\tbob  ", []string{"User", "bob"}, true},
		{`User "alice,bob" Address 'a b'`, []string{"User", "alice,bob", "Address", "a b"}, true},
		{`a\ b c\"d`, []string{"a b", `c"d`}, true},
		{`a\x`, []string{`a\x`}, true},
		{"User bob # a comment", []string{"User", "bob"}, true},
		{"User b#ob", []string{"User", "b#ob"}, true},
		{`User "bob`, nil, false},
	}
	for _, tt := range tests {
		got, ok := splitArgs(tt.in)
		if ok != tt.ok || !slices.Equal(got, tt.want) {
			t.Errorf("splitArgs(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestCursor(t *testing.T) {
	tests := []struct {
		in   string
		want []string // the tokens before strdelim returns NULL
	}{
		{"Include /etc/x", []string{"Include", "/etc/x"}},
		{"Include=/x", []string{"Include", "/x"}},
		{"Include = /x", []string{"Include", "/x"}},
		{"a==b", []string{"a", "", "b"}},
		{"=Include x", []string{"", "Include", "x"}},
		{`"a b" c`, []string{"a b", "c"}},
		{`x"a b"c d`, []string{"xa b", "c", "d"}},
		{`a "open`, []string{"a"}},
		{"", []string{""}},
	}
	for _, tt := range tests {
		var got []string
		c := cursor{s: tt.in}
		for tok, ok := c.next(); ok; tok, ok = c.next() {
			got = append(got, tok)
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("strdelim tokens of %q = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTarget(t *testing.T) {
	base := DefaultConn()
	with := func(f func(*Conn)) Conn { c := base; f(&c); return c }
	user := func(u string) Conn { return with(func(c *Conn) { c.User = u }) }
	addr := func(a string) Conn { return with(func(c *Conn) { c.Addr = a }) }
	tests := []struct {
		match   string
		noDNS   bool
		want    Conn
		reasons string // a substring of the joined reasons; "" for none
	}{
		{"User bob", false, user("bob"), ""},
		{"user=bob", false, user("bob"), ""},
		{"User=bob", false, user("bob"), ""},
		{"User alice,bob", false, user("alice"), ""},
		{`User "bob"`, false, user("bob"), ""},
		{"User !root,b*", false, user("bx"), ""},
		{"User *", false, base, ""},
		{"User ?", false, user("x"), ""},
		{"User *,!hardn-probe", false, user("x"), ""},
		{"User a*,!ax", false, user("ay"), ""},
		{"User a* User *x", false, user("ax"), ""},
		{"User !root", false, base, "User !root: only negated patterns"},
		{"User !root,!admin", false, base, "only negated patterns"},
		{"User alice User bob", false, base, "User alice, User bob: no value"},
		{"User 'bob'", false, user("bob"), "OpenSSH 9.2 and 9.6 read this line differently"},
		{`User a\ b`, false, user("a b"), "OpenSSH 9.2 and 9.6"},
		{"Host *.example.net", false, with(func(c *Conn) { c.Host = "x.example.net" }), ""},
		{"Host *.Example.NET", false, with(func(c *Conn) { c.Host = "x.example.net" }), ""},
		{"Host *.example.net", true, base, "UseDNS is no"},
		{"Host 192.0.2.*", true, with(func(c *Conn) { c.Host = "192.0.2.1" }), ""},
		{"Address 192.0.2.0/24", false, base, ""},
		{"Address 192.0.2.128/25", false, addr("192.0.2.128"), ""},
		{"Address 192.0.2.128/25,!192.0.2.128/26", false, addr("192.0.2.255"), ""},
		{"Address 192.0.2.*", false, base, ""},
		{"Address 192.0.2.?", false, base, ""},
		{"Address 10.*", false, addr("10.1.1.1"), ""},
		{"Address *,!192.0.2.0/24", false, addr("198.51.100.1"), ""},
		{"Address 2001:db8::/32", false, addr("2001:db8::"), ""},
		{"Address !192.0.2.0/24", false, base, "only negated patterns"},
		{"Address web*", false, base, "no address hardn tried matches"},
		{"LocalAddress 192.0.2.10 LocalPort 2230", false, with(func(c *Conn) { c.LAddr, c.LPort = "192.0.2.10", "2230" }), ""},
		{"LocalPort ssh", false, base, "not a port number"},
		{"LocalPort 0", false, base, "not a port number"},
		{"LocalPort 22 LocalPort 2230", false, base, "different ports"},
		{"RDomain red", false, with(func(c *Conn) { c.RDomain = "red" }), ""},
		{"RDomain !red", false, base, "only negated patterns"},
		{"User bob Address 192.0.2.128/25", false, with(func(c *Conn) { c.User, c.Addr = "bob", "192.0.2.128" }), ""},
		{"All", false, base, ""},
		{"Group admins", false, base, "group database"},
		{"Invalid-User", false, base, "Invalid-User"},
		{"Version OpenSSH_10*", false, base, "Version cannot be set"},
		{"User", false, base, "User has no value"},
	}
	for _, tt := range tests {
		t.Run(tt.match, func(t *testing.T) {
			got, reasons := target(tt.match, base, !tt.noDNS)
			if got != tt.want {
				t.Errorf("target = %+v, want %+v", got, tt.want)
			}
			joined := strings.Join(reasons, "; ")
			if tt.reasons == "" && joined != "" || !strings.Contains(joined, tt.reasons) {
				t.Errorf("reasons = %q, want %q", joined, tt.reasons)
			}
		})
	}
}

func TestMatchList(t *testing.T) {
	long := strings.Repeat("a", 1023)
	tests := []struct {
		s, list string
		fold    bool
		want    int
	}{
		{"bob", "bob", false, 1},
		{"bob", "alice,b?b", false, 1},
		{"bob", "!root", false, 0},
		{"root", "!root,*", false, -1},
		{"bob", "*,!b*", false, -1},
		{"Bob", "bob", false, 0},
		{"Web-01.Example.NET", "*.example.net", true, 1},
		{"", "a,,b", false, 1},
		{"a", "a,", false, 1},
		{"a", "a," + long, false, 0},
		{"a", "a," + long[1:], false, 1},
	}
	for _, tt := range tests {
		if got := matchList(tt.s, tt.list, tt.fold); got != tt.want {
			t.Errorf("matchList(%q, %.20q, %v) = %d, want %d", tt.s, tt.list, tt.fold, got, tt.want)
		}
	}
}

func TestMatchAddrList(t *testing.T) {
	tests := []struct {
		addr, list string
		want       int
	}{
		{"192.0.2.1", "192.0.2.0/24", 1},
		{"192.0.2.1", "192.0.2.1", 1},
		{"192.0.2.1", "192.0.2.*", 1},
		{"192.0.2.1", "198.51.100.0/24", 0},
		{"192.0.2.1", "*,!192.0.2.0/25", -1},
		{"192.0.2.1", "!192.0.2.2", 0},
		{"192.0.2.1", "192.0.2.1/24", -2},
		{"192.0.2.1", "192.0.2.0/24,", -2},
		{"web-01", "*", 0},
		{"2001:db8::1", "2001:db8::/32", 1},
		{"2001:db8::1", "192.0.2.0/24", 0},
	}
	for _, tt := range tests {
		if got := matchAddrList(tt.addr, tt.list); got != tt.want {
			t.Errorf("matchAddrList(%q, %q) = %d, want %d", tt.addr, tt.list, got, tt.want)
		}
	}
}

func TestUseDNS(t *testing.T) {
	for in, want := range map[string]bool{"port 22\nusedns yes\n": true, "usedns no\n": false, "UseDNS yes\n": true, "": false} {
		if got := useDNS([]byte(in)); got != want {
			t.Errorf("useDNS(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestStrings pins the names p3-output's findings and codes may use.
func TestStrings(t *testing.T) {
	tests := []struct {
		got, want string
	}{
		{IncludeUnknown.String(), "unknown"},
		{IncludeFirst.String(), "include first"},
		{IncludeNone.String(), "no include"},
		{IncludeOther.String(), "include not first"},
		{EnvKind(0).String(), "unknown"},
		{EnvPrivsepDir.String(), "privilege separation directory"},
		{EnvHostKeys.String(), "host keys"},
		{EnvPrivsepUser.String(), "privilege separation user"},
		{FindingKind(0).String(), "unknown"},
		{FindingClobberable.String(), "clobberable"},
		{FindingIncludeNotFirst.String(), "include-not-first"},
		{FindingMatchUntargeted.String(), "match-untargeted"},
		{FindingWarning.String(), "sshd-warning"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("String() = %q, want %q", tt.got, tt.want)
		}
	}
}

func TestNeedsPrivsepDir(t *testing.T) {
	tests := []struct {
		p    detect.Platform
		want bool
	}{
		{detect.Platform{Distro: detect.DistroDebian}, true},
		{detect.Platform{Distro: detect.DistroUbuntu}, true},
		{detect.Platform{Distro: detect.DistroProxmox}, true},
		{detect.Platform{Distro: detect.DistroOther, Like: detect.DistroDebian}, true},
		{detect.Platform{Distro: detect.DistroOther, Like: detect.DistroUbuntu}, true},
		{detect.Platform{Distro: detect.DistroOther, Like: detect.DistroAlpine}, false},
		{detect.Platform{Distro: detect.DistroOther}, false},
		{detect.Platform{Distro: detect.DistroAlpine}, false},
		{detect.Platform{}, false},
	}
	for _, tt := range tests {
		if got := needsPrivsepDir(tt.p); got != tt.want {
			t.Errorf("needsPrivsepDir(%+v) = %v, want %v", tt.p, got, tt.want)
		}
	}
}

func TestConnSpec(t *testing.T) {
	if got, want := DefaultConn().Spec(), "user=hardn-probe,host=probe.invalid,addr=192.0.2.1"; got != want {
		t.Errorf("DefaultConn().Spec() = %q, want %q", got, want)
	}
	c := Conn{User: "alice", Host: "web-01.example.net", Addr: "192.0.2.10", LPort: "2230"}
	if got, want := c.Spec(), "user=alice,host=web-01.example.net,addr=192.0.2.10,lport=2230"; got != want {
		t.Errorf("Spec() = %q, want %q", got, want)
	}
	for _, bad := range []Conn{{User: "alice"}, {User: "a,b", Host: "h", Addr: "192.0.2.1"}, {User: "a b", Host: "h", Addr: "192.0.2.1"}} {
		if bad.check() == nil {
			t.Errorf("check(%+v) = nil, want an error", bad)
		}
	}
}

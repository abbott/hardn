package sshd

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
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

func TestTarget(t *testing.T) {
	base := DefaultConn()
	with := func(f func(*Conn)) Conn { c := base; f(&c); return c }
	tests := []struct {
		match   string
		want    Conn
		reasons string // a substring of the joined reasons; "" for none
	}{
		{"User bob", with(func(c *Conn) { c.User = "bob" }), ""},
		{"user=bob", with(func(c *Conn) { c.User = "bob" }), ""},
		{"User alice,bob", with(func(c *Conn) { c.User = "alice" }), ""},
		{"User !root,b*", with(func(c *Conn) { c.User = "bx" }), ""},
		{"User !root", base, ""},
		{"User *", base, ""},
		{"Host *.example.net", with(func(c *Conn) { c.Host = "x.example.net" }), ""},
		{"Address 192.0.2.0/24", with(func(c *Conn) { c.Addr = "192.0.2.0" }), ""},
		{"Address 192.0.2.*", with(func(c *Conn) { c.Addr = "192.0.2.1" }), ""},
		{"Address 2001:db8::/32", with(func(c *Conn) { c.Addr = "2001:db8::" }), ""},
		{"LocalAddress 192.0.2.10 LocalPort 2230", with(func(c *Conn) { c.LAddr, c.LPort = "192.0.2.10", "2230" }), ""},
		{"RDomain red", with(func(c *Conn) { c.RDomain = "red" }), ""},
		{"User bob Address 192.0.2.0/24", with(func(c *Conn) { c.User, c.Addr = "bob", "192.0.2.0" }), ""},
		{"All", base, ""},
		{"Address web*", base, "no address matches"},
		{"LocalPort ssh", base, "not a port number"},
		{"Group admins", base, "group database"},
		{"Invalid-User", base, "Invalid-User"},
		{"Version OpenSSH_10*", base, "Version cannot be set"},
		{"User", base, "User has no value"},
	}
	for _, tt := range tests {
		t.Run(tt.match, func(t *testing.T) {
			args, _ := splitArgs(tt.match)
			got, reasons := target(args, base)
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

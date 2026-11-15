package confd_test

import (
	"testing"

	"github.com/abbott/hardn/pkg/host/confd"
)

const (
	key  = "SYSLOGD_OPTS"
	mark = "# Added by hardn (Syslog Settings): "
)

// TestParse: the one assignment in its three forms, and the mark's words.
func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name, data    string
		n             int
		words, marked string // joined with ","
		hasMark       bool
	}{
		{name: "double quotes", data: "# syslog\nSYSLOGD_OPTS=\"-t -S\"\n", n: 2, words: "-t,-S"},
		{name: "single quotes", data: "SYSLOGD_OPTS='-t'\n", n: 1, words: "-t"},
		{name: "bare", data: "SYSLOGD_OPTS=-t\n", n: 1, words: "-t"},
		{name: "empty", data: "SYSLOGD_OPTS=\"\"\n", n: 1},
		{name: "indented, spaces after", data: "  SYSLOGD_OPTS=\"-t\"  \n", n: 1, words: "-t"},
		{name: "commented out beside the real one", data: "#SYSLOGD_OPTS=\"-S\"\nSYSLOGD_OPTS=\"-t\"\n", n: 2, words: "-t"},
		{name: "mark above", data: mark + "-R 192.0.2.50:514 -L\nSYSLOGD_OPTS=\"-t -R 192.0.2.50:514 -L\"\n", n: 2, words: "-t,-R,192.0.2.50:514,-L",
			marked: "-R,192.0.2.50:514,-L", hasMark: true},
		{name: "mark naming nothing", data: "SYSLOGD_OPTS=\"-t\"\n" + mark + "\n", n: 1, words: "-t", hasMark: true},
		{name: "another key's line", data: "NTPD_OPTS=\"$X\"\nSYSLOGD_OPTS=\"-t\"\n", n: 2, words: "-t"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, bad := confd.Parse([]byte(tc.data), key, mark)
			if bad != nil {
				t.Fatal(*bad)
			}
			if l.N != tc.n || join(l.Words) != tc.words || join(l.Marked) != tc.marked || l.HasMark() != tc.hasMark {
				t.Fatalf("Parse = line %d, words %q, marked %q, mark %v; want %d, %q, %q, %v", l.N, l.Words, l.Marked, l.HasMark(), tc.n, tc.words, tc.marked, tc.hasMark)
			}
		})
	}
}

func join(words []string) string {
	out := ""
	for i, w := range words {
		if i > 0 {
			out += ","
		}
		out += w
	}
	return out
}

// TestParseRefuses: a file hardn cannot read exactly is a *Fault, naming the
// line and the key.
func TestParseRefuses(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		line       int
		what       string
	}{
		{"not set", "# SYSLOGD_OPTS=\"-t\"\n", 2, "SYSLOGD_OPTS is not set"},
		{"set twice", "SYSLOGD_OPTS=\"-t\"\nSYSLOGD_OPTS=\"-S\"\n", 2, "SYSLOGD_OPTS set twice"},
		{"expansion", "SYSLOGD_OPTS=\"-t $EXTRA\"\n", 1, "SYSLOGD_OPTS holds a character hardn does not write back"},
		{"semicolon", "SYSLOGD_OPTS='-t;reboot'\n", 1, "SYSLOGD_OPTS holds a character hardn does not write back"},
		{"unclosed quote", "SYSLOGD_OPTS=\"-t\n", 1, "SYSLOGD_OPTS holds a character hardn does not write back"},
		{"unclosed quote, two words", "SYSLOGD_OPTS=\"-t -S\n", 1, "SYSLOGD_OPTS in a form hardn does not read"},
		{"words after the value", "SYSLOGD_OPTS=\"-t\" -S\n", 1, "SYSLOGD_OPTS in a form hardn does not read"},
		{"two marks", mark + "-L\n" + mark + "-L\nSYSLOGD_OPTS=\"-L\"\n", 2, "hardn's mark, twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, bad := confd.Parse([]byte(tc.data), key, mark)
			if bad == nil {
				t.Fatal("Parse read it, want a *Fault")
			}
			if bad.Line != tc.line || bad.What != tc.what {
				t.Fatalf("Parse = line %d, %q; want %d, %q", bad.Line, bad.What, tc.line, tc.what)
			}
		})
	}
}

// TestBytes: the value rewritten in double quotes, the mark put above it or
// taken out, and every other line as it was.
func TestBytes(t *testing.T) {
	for _, tc := range []struct {
		name, data    string
		words, marked []string
		want          string
	}{
		{"value alone", "# syslog\nSYSLOGD_OPTS='-t'\n# end\n", []string{"-t", "-S"}, nil, "# syslog\nSYSLOGD_OPTS=\"-t -S\"\n# end\n"},
		{"no words", "SYSLOGD_OPTS=-t", nil, nil, "SYSLOGD_OPTS=\"\""},
		{"mark added above", "# syslog\nSYSLOGD_OPTS=\"-t\"\n", []string{"-t", "-L"}, []string{"-L"}, "# syslog\n" + mark + "-L\nSYSLOGD_OPTS=\"-t -L\"\n"},
		{"mark taken out", "# syslog\n" + mark + "-L\nSYSLOGD_OPTS=\"-t -L\"\n", []string{"-t"}, nil, "# syslog\nSYSLOGD_OPTS=\"-t\"\n"},
		{"mark below taken out", "SYSLOGD_OPTS=\"-t -L\"\n" + mark + "-L\n# end\n", []string{"-t"}, nil, "SYSLOGD_OPTS=\"-t\"\n# end\n"},
		{"mark moved above", "SYSLOGD_OPTS=\"-t -L\"\n# end\n" + mark + "-L\n", []string{"-t", "-S"}, []string{"-S"}, mark + "-S\nSYSLOGD_OPTS=\"-t -S\"\n# end\n"},
		{"mark replaced", "# syslog\n" + mark + "-L\nSYSLOGD_OPTS=\"-t -L\"\n", []string{"-t", "-S"}, []string{"-S"}, "# syslog\n" + mark + "-S\nSYSLOGD_OPTS=\"-t -S\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, bad := confd.Parse([]byte(tc.data), key, mark)
			if bad != nil {
				t.Fatal(*bad)
			}
			if got := string(l.Bytes(tc.words, tc.marked)); got != tc.want {
				t.Fatalf("Bytes = %q, want %q", got, tc.want)
			}
		})
	}
}

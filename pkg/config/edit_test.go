package config

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// load is data decoded as the loaders decode it.
func load(t *testing.T, data string) *Config {
	t.Helper()
	c := DefaultConfig()
	if err := yaml.Unmarshal([]byte(data), c); err != nil {
		t.Fatalf("load %q: %v", data, err)
	}
	return c
}

// TestEdit edits each form of hardn.yml and pins the exact bytes: the
// setting changed alone, every other byte as it was; or the refusal.
func TestEdit(t *testing.T) {
	port := func(c *Config) { c.SshPort = 2230 }
	user := func(c *Config) { c.Username = "bob" }
	list := func(ns ...string) func(*Config) { return func(c *Config) { c.Nameservers = ns } }
	for _, tt := range []struct {
		name, in string
		set      func(*Config)
		want     string
		err      error  // nil: want is the output
		key, why string // the refusal's setting and words
	}{
		{name: "a scalar, the comments beside and above kept",
			in:   "# hardn's configuration\nusername: alice    # the admin\n\n# SSH\nsshPort: 22 # the port\n",
			set:  port,
			want: "# hardn's configuration\nusername: alice    # the admin\n\n# SSH\nsshPort: 2230 # the port\n"},
		{name: "double quotes kept", in: "username: \"alice\" # q\n", set: user, want: "username: \"bob\" # q\n"},
		{name: "single quotes kept", in: "username: 'alice'\n", set: user, want: "username: 'bob'\n"},
		{name: "plain to a value that needs quotes", in: "dmzSubnet: x\n", set: func(c *Config) { c.DmzSubnet = "" }, want: "dmzSubnet: \"\"\n"},
		{name: "a key quoted, a tab after the colon", in: "\"username\":\talice\n", set: user, want: "\"username\":\tbob\n"},
		{name: "characters before the value", in: "lang: \"ключ\"\nusername: alice # é\n", set: user, want: "lang: \"ключ\"\nusername: bob # é\n"},
		{name: "a quoted value over two lines", in: "username: \"alice\n  # smith\" # c\nsshPort: 22\n", set: user, want: "username: \"bob\" # c\nsshPort: 22\n"},
		{name: "a # inside a plain value", in: "username: alice#1 # c\n", set: user, want: "username: bob # c\n"},
		{name: "a mapping indented as a whole", in: "  username: alice\n", set: port, want: "  username: alice\n  sshPort: 2230\n"},
		{name: "a value on the next line", in: "username:\n  alice\nsshPort: 22\n", set: user, want: "username:\n  bob\nsshPort: 22\n"},
		{name: "a plain value over two lines", in: "username: alice\n  smith  # c\nsshPort: 22\n", set: user, want: "username: bob  # c\nsshPort: 22\n"},
		{name: "a block scalar, its header comment kept", in: "username: |  # odd\n  alice\n  # text\nsshPort: 22\n", set: user, want: "username: bob  # odd\nsshPort: 22\n"},
		{name: "a tag replaced with its value", in: "username: !!str alice\n", set: user, want: "username: bob\n"},
		{name: "nothing after the colon", in: "username:\nsshPort: 22\n", set: user, want: "username: bob\nsshPort: 22\n"},
		{name: "nothing after the colon but a comment", in: "username:   # who\n", set: user, want: "username: bob   # who\n"},
		{name: "~", in: "username: ~ # none\n", set: user, want: "username: bob # none\n"},

		{name: "a block list: an item kept with its comments, one replaced, the commented-out one after kept",
			in:   "nameservers:  # DNS\n  # ours\n  - \"192.0.2.54\"  # first\n  - \"2001:db8::54\"\n  # - 192.0.2.52\nsshPort: 22\n",
			set:  list("192.0.2.54", "192.0.2.53"),
			want: "nameservers:  # DNS\n  # ours\n  - \"192.0.2.54\"  # first\n  - \"192.0.2.53\"\n  # - 192.0.2.52\nsshPort: 22\n"},
		{name: "a block list: an item added before one kept, the order of the new list",
			in:   "nameservers:\n    - 192.0.2.54 # kept\n",
			set:  list("192.0.2.53", "192.0.2.54"),
			want: "nameservers:\n    - 192.0.2.53\n    - 192.0.2.54 # kept\n"},
		{name: "a block list: a removed item's comments go with it",
			in:   "nameservers:\n  # first\n  - 192.0.2.52\n\n  # second\n  - 192.0.2.54  # two\n",
			set:  list("192.0.2.54"),
			want: "nameservers:\n\n  # second\n  - 192.0.2.54  # two\n"},
		{name: "a block list whose dashes are at its key's column",
			in: "nameservers:\n- 192.0.2.54\nsshPort: 22\n", set: list("192.0.2.54", "192.0.2.53"),
			want: "nameservers:\n- 192.0.2.54\n- 192.0.2.53\nsshPort: 22\n"},
		{name: "a block list emptied: [], the comment after it kept",
			in: "sshKeys:  # keys\n  # alice's\n  - ssh-ed25519 AAAA alice\n# end\n", set: func(c *Config) { c.SshKeys = []string{} },
			want: "sshKeys: []  # keys\n# end\n"},
		{name: "a list in brackets over several lines, on one now",
			in: "nameservers: [\n  192.0.2.54,\n  192.0.2.52,\n] # two\nsshPort: 22\n", set: list("192.0.2.53"),
			want: "nameservers: [192.0.2.53] # two\nsshPort: 22\n"},
		{name: "a ] inside a quoted item", in: "sshAllowedUsers: [\"a]b\", bob] # who\n", set: func(c *Config) { c.SshAllowedUsers = []string{"alice"} },
			want: "sshAllowedUsers: [\"alice\"] # who\n"},
		{name: "a list in brackets stays in brackets, in its quotes",
			in: "nameservers: ['192.0.2.54'] # two\n", set: list("1.1.1.1", "1.0.0.1"),
			want: "nameservers: ['1.1.1.1', '1.0.0.1'] # two\n"},
		{name: "[] to a list in the file's style",
			in: "sshKeys: []  # none yet\nsshAllowedUsers:\n    - \"alice\"\n", set: func(c *Config) { c.SshKeys = []string{"ssh-ed25519 AAAA bob"} },
			want: "sshKeys:  # none yet\n    - \"ssh-ed25519 AAAA bob\"\nsshAllowedUsers:\n    - \"alice\"\n"},
		{name: "nothing to a list in brackets, the file's style",
			in: "nameservers:\nsshAllowedUsers: [alice]\n", set: list("192.0.2.53"),
			want: "nameservers: [192.0.2.53]\nsshAllowedUsers: [alice]\n"},
		{name: "~ to a block list", in: "nameservers: ~ # none\nsshPort: 22\n", set: list("192.0.2.53"),
			want: "nameservers: # none\n  - 192.0.2.53\nsshPort: 22\n"},
		{name: "list items that are mappings: one kept, one added",
			in: "ufwAppProfiles:\n  - name: web # ours\n    ports: [80/tcp]\n",
			set: func(c *Config) {
				c.UfwAppProfiles = append(c.UfwAppProfiles, UfwAppProfile{Name: "ssh", Ports: []string{"2230/tcp"}})
			},
			want: "ufwAppProfiles:\n  - name: web # ours\n    ports: [80/tcp]\n  - name: ssh\n    title: \"\"\n    description: \"\"\n    ports:\n      - 2230/tcp\n"},

		{name: "a key added at the end of the mapping, before the comments after it",
			in: "username: alice\n\n# the end\n", set: port, want: "username: alice\nsshPort: 2230\n\n# the end\n"},
		{name: "a key added after a block list", in: "nameservers:\n  - 192.0.2.54\n# end\n", set: port,
			want: "nameservers:\n  - 192.0.2.54\nsshPort: 2230\n# end\n"},
		{name: "a list added in the file's block style", in: "sshAllowedUsers:\n- 'alice'\n", set: list("192.0.2.53"),
			want: "sshAllowedUsers:\n- 'alice'\nnameservers:\n- '192.0.2.53'\n"},
		{name: "a key added after a list in brackets over several lines", in: "nameservers: [\n  192.0.2.54\n]\n# end\n", set: port,
			want: "nameservers: [\n  192.0.2.54\n]\nsshPort: 2230\n# end\n"},
		{name: "a key added after a block scalar", in: "username: |\n  alice\n\n# end\n", set: port,
			want: "username: |\n  alice\nsshPort: 2230\n\n# end\n"},
		{name: "a default is not written", in: "username: alice\n", set: func(c *Config) { c.SshPort = 22; c.EnableBackups = true }, want: "username: alice\n"},
		{name: "confirmTimeout added",
			in: "username: alice\n", set: func(c *Config) { c.ConfirmTimeout = "3m" }, want: "username: alice\nconfirmTimeout: 3m\n"},
		{name: "confirmTimeout emptied", in: "confirmTimeout: 3m # long\n", set: func(c *Config) { c.ConfirmTimeout = "" }, want: "confirmTimeout: \"\" # long\n"},
		{name: "a deprecated bool kept while its value is", in: "permitRootLogin: false # old\n", set: port,
			want: "permitRootLogin: false # old\nsshPort: 2230\n"},
		{name: "a deprecated bool replaced by its string when its value changes", in: "permitRootLogin: false # old\n",
			set: func(c *Config) { _ = c.SetRootLogin(RootLoginYes) }, want: "permitRootLogin: \"yes\" # old\n"},
		{name: "logsconfig left as the file has it", in: "logsconfig:\n    logfilepath: \"\"\nusername: alice\n",
			set: func(c *Config) { c.LogsConfig.LogFilePath = c.LogFile; c.Username = "bob" }, want: "logsconfig:\n    logfilepath: \"\"\nusername: bob\n"},
		{name: "logsconfig as LoadConfig sets it, not added", in: "username: alice\n",
			set: func(c *Config) { c.LogsConfig.LogFilePath = c.LogFile; c.Username = "bob" }, want: "username: bob\n"},
		{name: "an unknown key kept", in: "legacy: 1\nusername: alice\n", set: user, want: "legacy: 1\nusername: bob\n"},

		{name: "a mapping in braces", in: "{username: alice, sshPort: 22}  # all\n",
			set:  func(c *Config) { c.SshPort = 2230; c.Nameservers = []string{"192.0.2.53"} },
			want: "{username: alice, sshPort: 2230, nameservers: [192.0.2.53]}  # all\n"},
		{name: "an empty mapping in braces", in: "{}\n", set: port, want: "{sshPort: 2230}\n"},
		{name: "CRLF line ends", in: "# c\r\nusername: alice\r\nnameservers:\r\n  - 192.0.2.54\r\n",
			set:  func(c *Config) { c.Nameservers = []string{"192.0.2.54", "192.0.2.53"}; c.SshPort = 2230 },
			want: "# c\r\nusername: alice\r\nnameservers:\r\n  - 192.0.2.54\r\n  - 192.0.2.53\r\nsshPort: 2230\r\n"},
		{name: "a byte order mark", in: "\xef\xbb\xbfusername: alice\n", set: user, want: "\xef\xbb\xbfusername: bob\n"},
		{name: "the last key with no line break", in: "username: alice", set: user, want: "username: bob"},
		{name: "the last key with no value and no line break", in: "username:", set: user, want: "username: bob"},
		{name: "a key added after the last with no line break", in: "username: alice", set: port, want: "username: alice\nsshPort: 2230"},
		{name: "comments alone", in: "# hardn\n# nothing yet\n", set: port, want: "# hardn\n# nothing yet\nsshPort: 2230\n"},
		{name: "comments alone, no last line break", in: "# hardn", set: port, want: "# hardn\nsshPort: 2230\n"},
		{name: "an empty file", in: "", set: list("192.0.2.53"), want: "nameservers:\n  - 192.0.2.53\n"},
		{name: "a leading ---", in: "---\nusername: alice\n", set: user, want: "---\nusername: bob\n"},
		{name: "a lone ---", in: "--- # hardn\n", set: port, want: "--- # hardn\nsshPort: 2230\n"},
		{name: "a directive", in: "%YAML 1.1\n---\nusername: alice\n", set: user, want: "%YAML 1.1\n---\nusername: bob\n"},
		{name: "an alias on an unchanged key kept", in: "x: &u alice\nusername: *u\nsshPort: 22\n", set: port, want: "x: &u alice\nusername: *u\nsshPort: 2230\n"},
		{name: "nothing changed", in: "# c\nusername: alice\n", set: func(*Config) {}, want: "# c\nusername: alice\n"},

		{name: "several documents", in: "username: alice\n---\nsshPort: 22\n", set: user, err: ErrNotEditable, why: "holds more than one YAML document"},
		{name: "a document end", in: "username: alice\n...\n", set: port, err: ErrNotEditable, why: "ends its YAML document with ..."},
		{name: "an alias on the key changed", in: "x: &u alice\nusername: *u\n", set: user, err: ErrNotEditable, key: "username", why: "uses a YAML anchor or alias"},
		{name: "an anchor on the key changed", in: "username: &u alice\nx: *u\n", set: user, err: ErrNotEditable, key: "username", why: "uses a YAML anchor or alias"},
		{name: "a merge", in: "<<: {username: alice}\nsshPort: 22\n", set: port, err: ErrNotEditable, why: "merges settings with <<"},
		{name: "a comment inside brackets", in: "nameservers: [192.0.2.54,  # ours\n  192.0.2.52]\n", set: list("192.0.2.53"),
			err: ErrNotEditable, key: "nameservers", why: "has comments inside its brackets"},
		{name: "an explicit key", in: "? username\n: alice\n", set: user, err: ErrNotEditable, key: "username", why: "is written in an unusual YAML form"},
		{name: "a lone carriage return", in: "username: alice\rsshPort: 22\n", set: port, err: ErrNotEditable, why: "has line breaks other than LF and CRLF"},
		{name: "a line separator", in: "username: \"a\u2028b\"\n", set: port, err: ErrNotEditable, why: "has line breaks other than LF and CRLF"},
		{name: "not a mapping", in: "~\n", set: port, err: ErrNotEditable, why: "does not hold its settings as key: value"},
		{name: "a key given twice", in: "username: alice\nusername: bob\n", set: port, err: ErrInvalidFile},
		{name: "a tab as indentation", in: "nameservers:\n\t- 192.0.2.54\n", set: port, err: ErrInvalidFile},
		{name: "not UTF-8", in: "username: \xff\n", set: port, err: ErrInvalidFile},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultConfig()
			if tt.err != ErrInvalidFile {
				c = load(t, tt.in)
			}
			tt.set(c)
			got, err := Edit([]byte(tt.in), c)
			if tt.err != nil {
				var ee *EditError
				if !errors.Is(err, tt.err) || got != nil {
					t.Fatalf("Edit = %q, %v; want %v", got, err, tt.err)
				}
				if tt.err != ErrInvalidFile && (!errors.As(err, &ee) || ee.Key != tt.key || ee.Why != tt.why || len(ee.Keys) == 0) {
					t.Errorf("refusal %+v; want %q %q", ee, tt.key, tt.why)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("Edit:\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// TestEditExample edits the example configuration, comments on nearly
// every line: the three settings changed, nothing else.
func TestEditExample(t *testing.T) {
	c := load(t, ExampleConfigContent)
	c.Nameservers = []string{"9.9.9.9", "1.1.1.1"}
	c.SshPort = 2230
	c.ConfirmTimeout = "3m"
	got, err := Edit([]byte(ExampleConfigContent), c)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(ExampleConfigContent, "nameservers:                      # DNS servers to configure\n  - \"1.1.1.1\"\n",
		"nameservers:                      # DNS servers to configure\n  - \"9.9.9.9\"\n  - \"1.1.1.1\"\n", 1)
	want = strings.Replace(want, "\n  - \"1.0.0.1\"\n", "\n", 1)
	want = strings.Replace(want, "sshPort: 22                       # SSH port", "sshPort: 2230                       # SSH port", 1)
	want = strings.Replace(want, "tz: \"America/New_York\"            # Timezone\n", "tz: \"America/New_York\"            # Timezone\nconfirmTimeout: 3m\n", 1)
	if string(got) != want {
		t.Errorf("Edit of the example:\n%s\nwant\n%s", got, want)
	}
}

// TestEditNested edits a nested mapping at the key that changed, adding
// the one it lacks at its end; no setting of Config is one (logsconfig is
// not a setting: derivedKey), so the trees are given here.
func TestEditNested(t *testing.T) {
	node := func(s string) *yaml.Node {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(s), &doc); err != nil {
			t.Fatal(err)
		}
		return doc.Content[0]
	}
	src := "outer:  # o\n  a: 1  # a\n  b: 2\n\n  # c\nnext: x\n"
	e, err := newEditor([]byte(src), []string{"outer"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.document(node("outer: {a: 1, b: 2}\nnext: x\n"), node("outer: {a: 1, b: 3, c: 4}\nnext: x\n")); err != nil {
		t.Fatal(err)
	}
	got, err := e.apply()
	if want := "outer:  # o\n  a: 1  # a\n  b: 3\n  c: 4\n\n  # c\nnext: x\n"; err != nil || string(got) != want {
		t.Errorf("nested edit: %q, %v; want %q", got, err, want)
	}
}

// TestEditChecks: bytes that do not read back as the configuration to save
// are refused, naming the setting that differs, or the key: a setting, an
// unknown key or logsconfig changed, removed or added, though not to save.
func TestEditChecks(t *testing.T) {
	const original = "username: alice\nlegacy: 1\nlogsconfig: {logfilepath: x}\n"
	to := load(t, original)
	to.Username = "bob"
	want, err := tree(to)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ out, key string }{
		{"username: alice\nlegacy: 1\nlogsconfig: {logfilepath: x}\n", "username"},
		{"username: bob\nlegacy: 1\nlogsconfig: {logfilepath: x}\nsshPort: 2230\n", "sshPort"},
		{"username: bob\nlegacy: 2\nlogsconfig: {logfilepath: x}\n", "legacy"},
		{"username: bob\nlogsconfig: {logfilepath: x}\n", "legacy"},
		{"username: bob\nlegacy: 1\nlogsconfig: {logfilepath: y}\n", derivedKey},
		{"username: bob\nlegacy: 1\nlogsconfig: {logfilepath: x}\nother: 1\n", "other"},
		{"username: [\n", ""},
		{"username: bob\n---\nusername: bob\n", ""},
	} {
		var ee *EditError
		if err := check([]byte(original), []byte(tt.out), want, []string{"username"}); !errors.Is(err, ErrEditUnsafe) || !errors.As(err, &ee) || ee.Key != tt.key {
			t.Errorf("check(%q) = %v; want ErrEditUnsafe for %q", tt.out, err, tt.key)
		}
	}
	if err := check([]byte(original), []byte("username: bob # b\nlegacy: 1\nlogsconfig: {logfilepath: x}\n"), want, []string{"username"}); err != nil {
		t.Errorf("check of the right bytes: %v", err)
	}
	if _, err := Edit([]byte("username: alice\n"), nil); !errors.Is(err, ErrNotLoaded) {
		t.Errorf("Edit of no configuration = %v, want ErrNotLoaded", err)
	}
}

// TestEditKeepsTheRest is a property of the edit, over random changes to
// a file commented throughout: it decodes to the configuration saved, and
// every line of the file outside the settings changed is kept, in order,
// as is each changed setting's own comment.
func TestEditKeepsTheRest(t *testing.T) {
	const file = "# hardn's configuration (web-01.example.net)\n\n" +
		"# Who\nusername: \"alice\"   # the admin\nsshAllowedUsers:   # who may log in\n  - alice # first\n  # bob is new\n  - bob\n\n" +
		"# DNS\nnameservers:\n  - \"192.0.2.54\" # ours\n  - \"2001:db8::54\"\n  # - 192.0.2.52\n" +
		"sshPort: 22   # SSH\n                # a second line\ndmzSubnet: '192.0.2'\nenableLynis: false # audit\n# Time\ntz: \"UTC\"\n# the end\n"
	pool := []string{"192.0.2.54", "2001:db8::54", "192.0.2.53", "192.0.2.52", "1.1.1.1"}
	users := []string{"alice", "bob", "george"}
	r := rand.New(rand.NewPCG(1, 2))
	pick := func(from []string) []string {
		out := slices.Clone(from)
		r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		return out[:1+r.IntN(len(out))]
	}
	orig := strings.Split(file, "\n")
	// spans are each setting's lines: its key's line to its value's last.
	spans := map[string][2]int{"username": {3, 3}, "sshAllowedUsers": {4, 7}, "nameservers": {10, 12}, "sshPort": {14, 14},
		"dmzSubnet": {16, 16}, "enableLynis": {17, 17}, "tz": {19, 19}}
	for i := range 300 {
		c := load(t, file)
		changed := map[string]bool{}
		if r.IntN(2) == 0 {
			c.Username, changed["username"] = users[r.IntN(3)], true
		}
		if r.IntN(2) == 0 {
			c.SshAllowedUsers, changed["sshAllowedUsers"] = pick(users), true
		}
		if r.IntN(2) == 0 {
			c.Nameservers, changed["nameservers"] = pick(pool), true
		}
		if r.IntN(3) == 0 {
			c.SshPort, changed["sshPort"] = 2230, true
		}
		if r.IntN(3) == 0 {
			c.DmzSubnet, changed["dmzSubnet"] = "192.0.2.128", true
		}
		if r.IntN(3) == 0 {
			c.EnableLynis, changed["enableLynis"] = true, true
		}
		if r.IntN(4) == 0 {
			c.ConfirmTimeout = "3m"
		}
		got, err := Edit([]byte(file), c)
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		back := load(t, string(got))
		if fmt.Sprint(back.Username, back.SshAllowedUsers, back.Nameservers, back.SshPort, back.DmzSubnet, back.EnableLynis, back.ConfirmTimeout, back.Tz) !=
			fmt.Sprint(c.Username, c.SshAllowedUsers, c.Nameservers, c.SshPort, c.DmzSubnet, c.EnableLynis, c.ConfirmTimeout, c.Tz) {
			t.Fatalf("run %d: read back %+v, want %+v:\n%s", i, back, c, got)
		}
		var keep []string
		for l, line := range orig {
			in := false
			for key, s := range spans {
				// A block list keeps its key's line; a scalar its comment.
				if changed[key] && l >= s[0] && l <= s[1] && !(s[1] > s[0] && l == s[0]) {
					in = true
					if _, comment, ok := strings.Cut(line, " #"); ok && s[1] == s[0] && !strings.Contains(string(got), " #"+comment+"\n") {
						t.Errorf("run %d: %s's comment %q lost:\n%s", i, key, comment, got)
					}
				}
			}
			if !in {
				keep = append(keep, line)
			}
		}
		lines := strings.Split(string(got), "\n")
		for j := 0; len(keep) > 0 && j < len(lines); j++ {
			if lines[j] == keep[0] {
				keep = keep[1:]
			}
		}
		if len(keep) > 0 {
			t.Fatalf("run %d: %q not kept, in order:\n%s", i, keep[0], got)
		}
	}
}

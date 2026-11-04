package aptconf_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/aptconf"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// The fixtures are synthetic, in the shapes of the VMs' files and dumps
// (02-inspect-c-*.log, 2026-11-03), not copies of them.

// dirDump is apt-config dump Dir's lines that Read checks, as the VMs print
// them.
const dirDump = `Dir "/";
Dir::Etc "etc/apt";
Dir::Etc::main "apt.conf";
Dir::Etc::parts "apt.conf.d";
Dir::Ignore-Files-Silently "";
Dir::Ignore-Files-Silently:: "~$";
Dir::Ignore-Files-Silently:: "\.dpkg-[a-z]+$";
Dir::Ignore-Files-Silently:: "\.ucf-[a-z]+$";
`

var roots = []string{"APT::Periodic", "Unattended-Upgrade::Origins-Pattern", "Hardn"}

// files is every syntax form Read takes, across files whose order matters:
// 20 sets two values, 52 (after 50, before 70) turns one back, apt.conf is
// read last.
var files = map[string]string{
	"/etc/apt/apt.conf.d/01keep": `Hardn
{
  Keep
  {
	# web hosts
	"^web-.*";
	"^db-[0-9]*$"; // the databases
  };
  /* a comment
     over lines */
  Mirror "http://192.0.2.10/debian#main";
};
`,
	"/etc/apt/apt.conf.d/20auto-upgrades": "APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n",
	"/etc/apt/apt.conf.d/50unattended-upgrades": `// Origins-Pattern says which packages are upgraded.
Unattended-Upgrade::Origins-Pattern {
	"origin=Debian,codename=${distro_codename},label=Debian";
//	"origin=Debian,codename=${distro_codename}-updates";
	"origin=Debian,codename=${distro_codename},label=Debian-Security";
};
`,
	"/etc/apt/apt.conf.d/52hardn-auto-upgrades":    "apt::periodic::unattended-upgrade \"0\";\n",
	"/etc/apt/apt.conf.d/70debconf":                "DPkg::Pre-Install-Pkgs {\"/usr/sbin/hardn-none --apt || true\";};\n",
	"/etc/apt/apt.conf.d/99web.conf":               "Hardn::Keep {\"^carol$\";};\n",
	"/etc/apt/apt.conf.d/20auto-upgrades.ucf-dist": "this is not read {",
	"/etc/apt/apt.conf.d/52hardn-auto-upgrades~":   "nor this {",
	"/etc/apt/apt.conf":                            "APT::Periodic::Enable \"1\";\n",
}

// rootDump is apt-config dump's output for roots over files.
const rootDump = `APT::Periodic "";
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "0";
APT::Periodic::Enable "1";
Unattended-Upgrade::Origins-Pattern "";
Unattended-Upgrade::Origins-Pattern:: "origin=Debian,codename=${distro_codename},label=Debian";
Unattended-Upgrade::Origins-Pattern:: "origin=Debian,codename=${distro_codename},label=Debian-Security";
Hardn "";
Hardn::Keep "";
Hardn::Keep:: "^web-.*";
Hardn::Keep:: "^db-[0-9]*$";
Hardn::Keep:: "^carol$";
Hardn::Mirror "http://192.0.2.10/debian#main";
`

func fake(t *testing.T, files map[string]string, dump string) *hostfake.System {
	t.Helper()
	sys := hostfake.New(host.Options{})
	sys.AddDir("/etc/apt/apt.conf.d", 0o755, 0, 0)
	for name, data := range files {
		sys.AddFile(name, []byte(data), 0o644, 0, 0)
	}
	sys.Respond(hostfake.Response{Stdout: dirDump}, "apt-config", "dump", "Dir")
	sys.Respond(hostfake.Response{Stdout: dump}, "apt-config", append([]string{"dump"}, roots...)...)
	return sys
}

func read(t *testing.T, files map[string]string, dump string) (aptconf.Config, error) {
	t.Helper()
	return aptconf.Read(context.Background(), fake(t, files, dump), roots...)
}

func TestRead(t *testing.T) {
	c, err := read(t, files, rootDump)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []string{
		"/etc/apt/apt.conf.d/01keep", "/etc/apt/apt.conf.d/20auto-upgrades", "/etc/apt/apt.conf.d/50unattended-upgrades",
		"/etc/apt/apt.conf.d/52hardn-auto-upgrades", "/etc/apt/apt.conf.d/70debconf", "/etc/apt/apt.conf.d/99web.conf", "/etc/apt/apt.conf",
	}
	if got := c.Files(); !slices.Equal(got, wantFiles) {
		t.Errorf("Files() = %q, want %q", got, wantFiles)
	}
	for _, tc := range []struct{ name, value, file string }{
		{"APT::Periodic::Update-Package-Lists", "1", "/etc/apt/apt.conf.d/20auto-upgrades"},
		{"apt::PERIODIC::Unattended-Upgrade", "0", "/etc/apt/apt.conf.d/52hardn-auto-upgrades"},
		{"APT::Periodic::Enable", "1", "/etc/apt/apt.conf"},
		{"Hardn::Mirror", "http://192.0.2.10/debian#main", "/etc/apt/apt.conf.d/01keep"},
	} {
		v, f, ok := c.Value(tc.name)
		if !ok || v != tc.value || f != tc.file {
			t.Errorf("Value(%s) = %q, %q, %v; want %q, %q", tc.name, v, f, ok, tc.value, tc.file)
		}
	}
	for _, name := range []string{"APT::Periodic", "APT::Periodic::Download-Upgradeable-Packages", "DPkg::Pre-Install-Pkgs", "Hardn::Keep::"} {
		if v, f, ok := c.Value(name); ok {
			t.Errorf("Value(%s) = %q, %q, true; want not set or not under a root", name, v, f)
		}
	}
	keep := c.Items("hardn::keep")
	want := []aptconf.Item{
		{"^web-.*", "/etc/apt/apt.conf.d/01keep"}, {"^db-[0-9]*$", "/etc/apt/apt.conf.d/01keep"}, {"^carol$", "/etc/apt/apt.conf.d/99web.conf"},
	}
	if !slices.Equal(keep, want) {
		t.Errorf("Items(Hardn::Keep) = %q, want %q", keep, want)
	}
	if got := c.Items("Unattended-Upgrade::Origins-Pattern"); len(got) != 2 || strings.Contains(got[1].Value, "updates") {
		t.Errorf("Items(Origins-Pattern) = %q, want the two not commented out", got)
	}
}

// Without leaves one file out and works the values out again: off with no
// record (gallery-updates.md Made here).
func TestWithout(t *testing.T) {
	c, err := read(t, files, rootDump)
	if err != nil {
		t.Fatal(err)
	}
	w := c.Without("/etc/apt/apt.conf.d/52hardn-auto-upgrades")
	if v, f, ok := w.Value("APT::Periodic::Unattended-Upgrade"); !ok || v != "1" || f != "/etc/apt/apt.conf.d/20auto-upgrades" {
		t.Errorf("Without(52…): %q, %q, %v; want 1 from 20auto-upgrades", v, f, ok)
	}
	if v, _, _ := c.Value("APT::Periodic::Unattended-Upgrade"); v != "0" {
		t.Errorf("Without changed the Config it was called on: %q", v)
	}
	if got := w.Items("Hardn::Keep"); len(got) != 3 {
		t.Errorf("Without(52…) Items = %q, want 3", got)
	}
	if got := c.Without("/etc/apt/apt.conf.d/99web.conf").Items("Hardn::Keep"); len(got) != 2 {
		t.Errorf("Without(99web.conf) Items = %q, want 2", got)
	}
}

// Any line of apt-config dump the files do not give is a refusal: a value,
// an order, a spelling, an item apt adds.
func TestDisagreement(t *testing.T) {
	for name, dump := range map[string]string{
		"value":    strings.Replace(rootDump, `Unattended-Upgrade "0"`, `Unattended-Upgrade "1"`, 1),
		"order":    strings.Replace(rootDump, "Hardn::Keep:: \"^web-.*\";\nHardn::Keep:: \"^db-[0-9]*$\";", "Hardn::Keep:: \"^db-[0-9]*$\";\nHardn::Keep:: \"^web-.*\";", 1),
		"spelling": strings.Replace(rootDump, "Hardn::Mirror", "Hardn::mirror", 1),
		"extra":    rootDump + "Hardn::Default \"1\";\n",
		"missing":  strings.Replace(rootDump, "APT::Periodic::Enable \"1\";\n", "", 1),
		"format":   strings.ReplaceAll(rootDump, `"`, `'`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := read(t, files, dump); !errors.Is(err, detect.ErrUnrecognised) {
				t.Errorf("err = %v, want ErrUnrecognised", err)
			}
		})
	}
}

func TestRefusals(t *testing.T) {
	const f = "/etc/apt/apt.conf.d/30george"
	for name, data := range map[string]string{
		"clear":          "#clear APT::Periodic;\n",
		"include":        "#include \"/etc/apt/other.conf\";\n",
		"unquoted":       "APT::Periodic::Enable 1;\n",
		"no semicolon":   "APT::Periodic::Enable \"1\"\n",
		"open quote":     "APT::Periodic::Enable \"1;\n",
		"open comment":   "/* APT::Periodic::Enable \"1\";\n",
		"open block":     "APT { Periodic { Enable \"1\"; };\n",
		"stray close":    "};\n",
		"top-level item": "\"1\";\n",
		"empty tag":      "APT::::Enable \"1\";\n",
		"trailing ::":    "APT::Periodic:: \"1\";\n",
		"value block":    "APT::Periodic \"1\" { Enable \"1\"; };\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := read(t, map[string]string{f: data}, ""); !errors.Is(err, detect.ErrUnrecognised) || !strings.Contains(err.Error(), f) {
				t.Errorf("err = %v, want ErrUnrecognised naming %s", err, f)
			}
		})
	}
	t.Run("directory", func(t *testing.T) {
		sys := fake(t, nil, "")
		sys.AddDir("/etc/apt/apt.conf.d/30web", 0o755, 0, 0)
		if _, err := aptconf.Read(context.Background(), sys, roots...); !errors.Is(err, detect.ErrUnrecognised) {
			t.Errorf("err = %v, want ErrUnrecognised", err)
		}
	})
	t.Run("no root", func(t *testing.T) {
		if _, err := aptconf.Read(context.Background(), fake(t, nil, ""), "APT::"); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("err = %v, want fs.ErrInvalid", err)
		}
	})
	for name, dump := range map[string]string{
		"parts":  strings.Replace(dirDump, `"apt.conf.d"`, `"/srv/apt.d"`, 1),
		"main":   strings.Replace(dirDump, "Dir::Etc::main \"apt.conf\";\n", "", 1),
		"format": strings.ReplaceAll(dirDump, `";`, `"`),
	} {
		t.Run("dir "+name, func(t *testing.T) {
			sys := hostfake.New(host.Options{})
			sys.AddDir("/etc/apt/apt.conf.d", 0o755, 0, 0)
			sys.Respond(hostfake.Response{Stdout: dump}, "apt-config", "dump", "Dir")
			if _, err := aptconf.Read(context.Background(), sys, roots...); !errors.Is(err, detect.ErrUnrecognised) {
				t.Errorf("err = %v, want ErrUnrecognised", err)
			}
		})
	}
}

// TestSkippedNames: a name apt's GetListOfFilesInDir doesn't take is
// skipped, its bytes never read: hidden first, then an extension other
// than "conf", a byte apt refuses, a "." last. A name it takes is read.
func TestSkippedNames(t *testing.T) {
	for _, name := range []string{".30web", "20auto-upgrades.ucf-dist", "foo.txt", "30web.list", "30web 2", "30w\u00e9b", "30web.", "30web~"} {
		t.Run(name, func(t *testing.T) {
			c, err := read(t, map[string]string{"/etc/apt/apt.conf.d/" + name: "this is not read {"}, "")
			if err != nil || len(c.Files()) != 0 {
				t.Errorf("Files = %q, %v; want none read", c.Files(), err)
			}
		})
	}
	for _, name := range []string{"30web.conf", "30web:1", "30.web.conf"} {
		t.Run(name, func(t *testing.T) {
			if _, err := read(t, map[string]string{"/etc/apt/apt.conf.d/" + name: "this is read {"}, ""); !errors.Is(err, detect.ErrUnrecognised) {
				t.Errorf("err = %v, want the parse's ErrUnrecognised", err)
			}
		})
	}
}

// No parts directory and no apt.conf read as nothing; the dump of roots
// apt has nothing under is empty.
func TestEmpty(t *testing.T) {
	sys := hostfake.New(host.Options{})
	sys.Respond(hostfake.Response{Stdout: dirDump}, "apt-config", "dump", "Dir")
	sys.Respond(hostfake.Response{}, "apt-config", append([]string{"dump"}, roots...)...)
	c, err := aptconf.Read(context.Background(), sys, roots...)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Value("APT::Periodic::Enable"); ok || len(c.Files()) != 0 {
		t.Errorf("Value ok = %v, Files = %q; want nothing", ok, c.Files())
	}
}

// Read only reads: no write, no other command.
func TestOnlyReads(t *testing.T) {
	sys := fake(t, files, rootDump)
	if _, err := aptconf.Read(context.Background(), sys, roots...); err != nil {
		t.Fatal(err)
	}
	for _, c := range sys.Calls() {
		if !slices.Contains([]string{"run", "read", "stat", "readdir"}, c.Op) || (c.Op == "run" && c.Argv[0] != "apt-config") {
			t.Errorf("call %s", c)
		}
	}
}

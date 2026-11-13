package output_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/details"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
)

var markdownNow = time.Date(2026, 10, 20, 12, 30, 0, 0, time.UTC)

// handOverRun is the report to hand over on the gallery's debian13 host: the
// Figma host's rows with evidence, a second uid 0 and a reboot pending, so that
// the level has factors and findings, and every section of System Details read.
func handOverRun() output.Run {
	rs := figmaReports()
	rs = replaced(rs, row("users.admin", "User", engine.Configured, "non-root, sudo", users.CodeUID0))
	rs[2].Status.Evidence = []string{"sshd -T: port 2230"}
	rs[3].Status.Evidence = []string{"ufw status verbose: Default: deny (incoming)", "ufw status verbose: 2230/tcp ALLOW IN Anywhere"}
	h := upFor(figmaUptime)
	h.Reboot = header.Reboot{Pending: detect.Yes, Findings: []engine.Finding{{Code: header.CodeRebootRequired, Detail: header.DetailRebootRequired,
		Source: header.RebootRequiredFile}}}
	l := risk.Assess(rs, h)
	whole := wholeReport()
	whole.OS.Uptime = h.Uptime // one host, one uptime
	r := detailsRun(whole)
	r.Reports, r.RiskLevel = rs, &l
	r.Deprecations, r.ConfigPath = []config.Deprecation{deprecation}, fixtureConfig
	return r
}

// TestMarkdownDocuments pins the whole report: the host read, and one with no
// section read, a check that failed and no risk level established, as a user
// other than root.
func TestMarkdownDocuments(t *testing.T) {
	refused := fs.ErrPermission
	unread := func(src string) []details.Unread { return []details.Unread{{Source: src, Err: refused}} }
	none := detailsRun(details.Report{
		OS:     details.OSKernel{Unread: unread("/proc/sys/kernel/osrelease")},
		Host:   details.HostIPs{Unread: unread("ip addr")},
		Users:  details.Users{Unread: unread("/etc/passwd")},
		CPU:    details.CPU{Unread: unread("/proc/cpuinfo")},
		Disk:   details.Disk{Unread: unread("df")},
		Memory: details.Memory{Unread: unread("/proc/meminfo")},
		Logins: details.Logins{Unread: unread("lslogins")},
	})
	none.Hostname, none.Facts = "", &detect.Facts{}
	none.Reports = []engine.Report{{Control: "ssh.port", Title: "SSH Port", Err: errors.New("sshd -T: permission denied")}}
	l := risk.Assess(none.Reports, header.Header{})
	none.RiskLevel = &l
	for _, tc := range []struct {
		name string
		run  output.Run
		opts output.MarkdownOptions
	}{
		{"report", handOverRun(), output.MarkdownOptions{Now: markdownNow}},
		{"report-not-read", none, output.MarkdownOptions{Now: markdownNow, NotRoot: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			if err := output.WriteMarkdown(&b, tc.run, tc.opts); err != nil {
				t.Fatal(err)
			}
			if b.String() != output.Markdown(tc.run, tc.opts) {
				t.Error("WriteMarkdown and Markdown differ")
			}
			golden(t, "markdown/"+tc.name+".md", []byte(b.String()))
		})
	}
}

// TestMarkdownPartsLeftOut: a run with no check and no report has neither
// section, and the document is still whole.
func TestMarkdownPartsLeftOut(t *testing.T) {
	got := output.Markdown(output.Run{Mode: output.ModeCheck, Hostname: "web-01"}, output.MarkdownOptions{Now: markdownNow})
	for _, absent := range []string{"## Risk level", "## Status", "## Disk", "Kernel", "Not root"} {
		if strings.Contains(got, absent) {
			t.Errorf("%q in:\n%s", absent, got)
		}
	}
	for _, want := range []string{"# hardn report: `web-01`\n", "Generated 2026-10-20 12:30 UTC.\n", "## Host\n", "## Findings\n\nNo findings.\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
}

// hostile is a run whose every string a host, a file or a version could hold
// is p.
func hostile(p string) output.Run {
	facts := detect.Facts{
		OS:        detect.OSRelease{ID: p, VersionID: p, VersionCodename: p, PrettyName: p},
		Platform:  detect.Platform{Distro: detect.DistroDebian, Version: p, Support: detect.Supported},
		Proxmox:   detect.Proxmox{Installed: detect.Yes, Version: p},
		Container: detect.Container{Detected: detect.Yes, Manager: p},
	}
	finding := engine.Finding{Code: p, Detail: p, Source: p}
	reports := []engine.Report{
		{Control: "ssh.port", Title: p, Status: engine.Status{State: engine.Configured, Detail: p, Evidence: []string{p, p}, Findings: []engine.Finding{finding}}},
		{Control: p, Title: p, Status: engine.Status{Detail: p}, Err: errors.New(p)},
	}
	level := risk.Level{Band: risk.High, Factors: []risk.Factor{{Control: "ssh.port", Detail: p}, {Detail: p}, {Control: p, Detail: p}},
		Findings: []engine.Finding{finding}}
	report := details.Report{
		OS:   details.OSKernel{Kernel: p, Uptime: header.Uptime{Duration: time.Hour, Known: true}},
		Host: details.HostIPs{Hostname: p, Addresses: []details.Address{{Interface: p, Family: p, Address: p}}, Unread: []details.Unread{{Source: p}}},
		Users: details.Users{Root: detect.Yes, List: []details.User{{Name: p, Grants: []users.Grant{{Tool: p, Grants: detect.Yes}}},
			{Name: p, Grants: sudoYes}}},
		CPU:    details.CPU{Model: p, CPUs: 2, Sockets: 2, Load: [3]string{p, p, p}},
		Disk:   details.Disk{Source: p, Type: p, Used: 1, Total: 2, Known: true, Pool: p, Health: p},
		Memory: details.Memory{Total: 2 << 30, Available: 1 << 30, AvailableKnown: true},
		Logins: details.Logins{State: details.LoginsRead, List: []details.Login{{User: p, State: details.LoginAt, When: p, TTY: p, From: p},
			{User: p, State: details.LoginAt, When: p, TTY: p}, {User: p}}},
	}
	return output.Run{Mode: output.ModeCheck, HardnVersion: p, Hostname: p, Facts: &facts, Reports: reports, RiskLevel: &level,
		Details: &report, Deprecations: []config.Deprecation{{Key: p, Line: 3, Found: p, Use: p}}, ConfigPath: p}
}

// markup is what of a Markdown document a renderer acts on, read as a renderer
// reads it and not as mdText and mdCode write it: each line with its tables'
// escaped pipes, its code spans and its backslash escapes taken out, so that
// what is left of a special character is one that is markup.
func markup(t *testing.T, doc string) []string {
	t.Helper()
	var lines []string
	for line := range strings.SplitSeq(doc, "\n") {
		line = strings.ReplaceAll(line, `\|`, "")
		var b strings.Builder
		for i := 0; i < len(line); {
			switch c := line[i]; {
			case c == '\\' && i+1 < len(line) && strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", line[i+1]) >= 0:
				i += 2
			case c == '`':
				n := 0
				for i+n < len(line) && line[i+n] == '`' {
					n++
				}
				end := -1
				for j := i + n; j < len(line); j++ {
					if line[j] != '`' {
						continue
					}
					m := 0
					for j+m < len(line) && line[j+m] == '`' {
						m++
					}
					if m == n {
						end = j + m
						break
					}
					j += m - 1
				}
				if end < 0 {
					t.Fatalf("a code span never closes in %q", line)
				}
				b.WriteString("CODE")
				i = end
			default:
				b.WriteByte(c)
				i++
			}
		}
		lines = append(lines, b.String())
	}
	return lines
}

// TestMarkdownHostStringsAreText: whatever a string from the host holds, the
// document has the markup it has for "x" and no more: no line added, no HTML,
// link, image, emphasis, heading, list or table cell, nothing a renderer links
// on its own, and no byte a terminal acts on.
func TestMarkdownHostStringsAreText(t *testing.T) {
	opts := output.MarkdownOptions{Now: markdownNow, NotRoot: true}
	plain := markup(t, output.Markdown(hostile("x"), opts))
	for _, p := range []string{
		"<script>alert(1)</script>", "<img src=x onerror=alert(1)>", "<!-- c -->", "</td></tr></table><h1>x</h1>",
		"[click](https://evil.example.net)", "![i](https://evil.example.net/i.png)", "[a]: https://evil.example.net", "[^1]",
		"<https://evil.example.net>", "https://evil.example.net/x", "HTTP://EVIL.EXAMPLE.NET", "www.evil.example.net", "alice@example.net",
		"mailto:alice", "**bold** __b__ *i* _i_ ~~s~~", "`code`", "``a`b``", "`", "a ` b `` c ``` d", "```", "~~~", "a|b", `a\|b`, `a\`, `\\`,
		"| x | y |", "&lt;b&gt; &#60; &amp;", "$x^2$", "# heading", "> quote", "- item", "1. item", "* * *", "---", "===", "    code",
		"line\n# heading\n\n- item", "a\r\n<b>", "tab\there", "\x1b[31mred\x1b[0m", "\x9b31m", "bell\a", "\u202eevil\u202c", "\u200b", "\U000e0041",
		"bad\xffutf8", "x",
	} {
		t.Run(p, func(t *testing.T) {
			doc := output.Markdown(hostile(p), opts)
			for _, r := range doc {
				if r != '\n' && (unicode.IsControl(r) || unicode.Is(unicode.Cf, r)) {
					t.Errorf("%U in the document", r)
				}
			}
			got := markup(t, doc)
			if len(got) != len(plain) {
				t.Fatalf("%d lines, %d for x:\n%s", len(got), len(plain), doc)
			}
			for i, line := range got {
				for _, c := range "\\`*_[]<>&|~$#!" {
					if a, b := strings.Count(line, string(c)), strings.Count(plain[i], string(c)); a != b {
						t.Errorf("line %d has %d %q, %d for x:\n%s", i+1, a, c, b, strings.Split(doc, "\n")[i])
					}
				}
				low := strings.ToLower(line)
				for _, l := range []string{"://", "www.", "@", "mailto:"} {
					if strings.Contains(low, l) {
						t.Errorf("line %d holds %q outside a code span:\n%s", i+1, l, strings.Split(doc, "\n")[i])
					}
				}
				if a, b := strings.TrimLeft(line, " ") != line, strings.TrimLeft(plain[i], " ") != plain[i]; a != b {
					t.Errorf("line %d is indented:\n%s", i+1, strings.Split(doc, "\n")[i])
				}
			}
		})
	}
}

// TestMarkdownCodeSpans: a code span's backtick strings are longer than any run
// inside it, a pipe is escaped only in a table's cell, and what a terminal
// would act on is written out.
func TestMarkdownCodeSpans(t *testing.T) {
	title := func(host string) string {
		doc := output.Markdown(output.Run{Hostname: host}, output.MarkdownOptions{Now: markdownNow})
		return strings.SplitN(doc, "\n", 2)[0]
	}
	for host, want := range map[string]string{
		"web-01":       "# hardn report: `web-01`",
		"a`b":          "# hardn report: ``a`b``",
		"`a``":         "# hardn report: ``` `a`` ```",
		"a|b":          "# hardn report: `a|b`",
		"a\nb":         "# hardn report: `a b`",
		"\u202eweb":    "# hardn report: `\\u202eweb`",
		"a\x1b[0m":     "# hardn report: `a\\u001b[0m`",
		"\U000e0041 x": "# hardn report: `\\U000e0041 x`",
		" \t ":         "# hardn report",
	} {
		if got := title(host); got != want {
			t.Errorf("%q: %s, want %s", host, got, want)
		}
	}
	r := output.Run{Mode: output.ModeCheck, Reports: []engine.Report{{Control: "ssh.port", Title: "SSH Port",
		Status: engine.Status{State: engine.Configured, Detail: "a|b *c* https://example.net/a|b"}}}}
	if got, want := output.Markdown(r, output.MarkdownOptions{}), "| SSH Port | Configured | `a\\|b *c* https://example.net/a\\|b` |\n"; !strings.Contains(got, want) {
		t.Errorf("no %q in:\n%s", want, got)
	}
	r.Reports[0].Status.Detail = "a|b *c*"
	if got, want := output.Markdown(r, output.MarkdownOptions{}), "| SSH Port | Configured | a\\|b \\*c\\* |\n"; !strings.Contains(got, want) {
		t.Errorf("no %q in:\n%s", want, got)
	}
}

func TestFormatUsed(t *testing.T) {
	for _, c := range []struct {
		used, total uint64
		want        string
	}{
		{4518241280, 41442029568, "4.2 of 38.6 GiB (11%)"},
		{87 << 20, 256 << 20, "87 of 256 MiB (34%)"},
		{0, 0, "0 of 0 MiB (0%)"},
	} {
		if got := output.FormatUsed(c.used, c.total); got != c.want {
			t.Errorf("%d of %d: %q, want %q", c.used, c.total, got, c.want)
		}
	}
}

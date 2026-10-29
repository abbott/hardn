package steps

import (
	"fmt"
	"path"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// The planners carry the primitives' findings on their changes
// (engine.Change.Findings), which p3-output renders. Each code is the
// primitive's own stable kind code (pinned by its TestFindingCodes), upper
// case with underscores, under the primitive's prefix: PVE_ROOT_KEY_ONLY,
// SSHD_OVERRIDDEN, UFW_SSH_NOT_ALLOWED_AT_START.

// CodeMigrateDropped is a legacy line the migration leaves out of
// 00-hardn.conf, with why (migrate.Plan.Dropped).
const CodeMigrateDropped = "SSH_MIGRATE_LINE_DROPPED"

// findingCode is kind, a primitive's code ("root-key-only"), as an
// engine.Finding's: "<PREFIX>_ROOT_KEY_ONLY", the prefix said once.
func findingCode(prefix, kind string) string {
	k := strings.ToUpper(strings.ReplaceAll(kind, "-", "_"))
	if strings.HasPrefix(k, prefix+"_") {
		return k
	}
	return prefix + "_" + k
}

// at is file:line, file alone, or "".
func at(file string, line int) string {
	if file != "" && line > 0 {
		return fmt.Sprintf("%s:%d", file, line)
	}
	return file
}

func pveFindings(fs []pve.Finding) []engine.Finding {
	var out []engine.Finding
	for _, f := range fs {
		out = append(out, engine.Finding{Code: findingCode("PVE", f.Kind.String()), Detail: f.Detail, Source: at(f.File, f.Line)})
	}
	return out
}

func sshdFindings(fs []sshd.Finding) []engine.Finding {
	var out []engine.Finding
	for _, f := range fs {
		out = append(out, engine.Finding{Code: findingCode("SSHD", f.Kind.String()), Detail: f.Detail, Source: at(f.File, f.Line)})
	}
	return out
}

func ufwFindings(fs []ufw.Finding) []engine.Finding {
	var out []engine.Finding
	for _, f := range fs {
		out = append(out, engine.Finding{Code: findingCode("UFW", f.Kind.String()), Detail: strings.TrimPrefix(f.String(), f.Kind.String()+": "), Source: "ufw"})
	}
	return out
}

// droppedFindings are the legacy lines a migration leaves out, with why.
func droppedFindings(ds []migrate.Carried) []engine.Finding {
	var out []engine.Finding
	for _, d := range ds {
		out = append(out, engine.Finding{Code: CodeMigrateDropped, Detail: fmt.Sprintf("%q: %s", d.Text, d.Why), Source: at(d.File, d.Line)})
	}
	return out
}

// The conversions above, exported for the status controls
// (next/p4-check-ssh, docs/engine/checks.md §1.7), so that a check codes a
// primitive's finding as a plan does, and the next area adds its
// primitive's here rather than a copy of its own.

// FindingCode is a primitive's finding kind ("root-not-key-only") as a
// code under its prefix ("PVE_ROOT_NOT_KEY_ONLY"), the prefix said once.
func FindingCode(prefix, kind string) string { return findingCode(prefix, kind) }

// SourceAt is a Finding's Source for a line of a file: file:line, file
// alone, or "".
func SourceAt(file string, line int) string { return at(file, line) }

// SSHDFinding is one of sshd's findings as an engine.Finding (SSHD_...).
func SSHDFinding(f sshd.Finding) engine.Finding { return sshdFindings([]sshd.Finding{f})[0] }

// PVEFinding is one of pve's findings as an engine.Finding (PVE_...).
func PVEFinding(f pve.Finding) engine.Finding { return pveFindings([]pve.Finding{f})[0] }

// UFWFinding is one of ufw's findings as an engine.Finding (UFW_...), for
// the firewall's status (next/p4-check-firewall).
func UFWFinding(f ufw.Finding) engine.Finding { return ufwFindings([]ufw.Finding{f})[0] }

// DNSFinding is one of dns's findings as an engine.Finding (DNS_...), in a
// few words, for a plan and a check alike (next/s2-control; the words of
// gallery-dns.md §5, decision 5, proposed): s is the state it was found in,
// which says whether udhcpc's rewrite is established; where whether udhcpc
// is the DHCP client is not, the finding says what it would do if it is
// (next/s2-udhcpc). A udhcpc hook is
// named by its path (next/s2-flow-review, proposed), as a DHCP client is
// by its name. dns's own detail, a long sentence, is in the primitive's
// documentation (dns.md §2.9).
func DNSFinding(s dns.State, f dns.Finding) engine.Finding {
	out := engine.Finding{Code: findingCode("DNS", f.Kind.String()), Detail: f.Detail, Source: f.Path}
	switch f.Kind {
	case dns.FindingPerLink:
		out.Source = "resolvectl dns"
		if l := f.Link; l != nil {
			name, noun := l.Name, "nameserver"
			if l.Index == 0 {
				name = "delegate " + name
			}
			if len(l.Servers) > 1 {
				noun += "s"
			}
			out.Detail = fmt.Sprintf("%s still uses its own %s %s", name, noun, strings.Join(l.Servers, ", "))
		}
	case dns.FindingDHCPClient:
		out.Detail = path.Base(f.Path) + " may rewrite " + dns.ResolvConf + " on a lease"
	case dns.FindingUDHCPC:
		switch {
		case f.Path != dns.UDHCPCConf: // a hook, named by its path, so that two are two lines
			out.Detail = f.Path + " may rewrite " + dns.ResolvConf
		case s.UDHCPC.Rewrites == detect.Yes:
			out.Detail = "udhcpc rewrites " + dns.ResolvConf + " on each lease"
		default:
			out.Detail = "udhcpc may rewrite " + dns.ResolvConf + " on each lease"
		}
	case dns.FindingUDHCPCUnknown: // whether udhcpc is the client is not established (next/s2-udhcpc, proposed)
		out.Detail = "udhcpc may rewrite " + dns.ResolvConf + " if it is the DHCP client"
		if s.UDHCPC.Rewrites == detect.Yes {
			out.Detail = "udhcpc rewrites " + dns.ResolvConf + " if it is the DHCP client"
		}
	case dns.FindingProxmox:
		out.Detail, out.Source = "Saving Proxmox VE's DNS settings rewrites "+dns.ResolvConf, ""
	}
	return out
}

// SudoersFinding is one of sudoers' findings as an engine.Finding
// (SUDOERS_...), in a few words that name the file, as the review shows a
// finding's detail alone, for a plan and a check alike (next/s9-control;
// gallery-environment.md §7 item 21): a file read after hardn's rule as
// "<file> may override hardn's rule" (decided, slice 9's decision 7), a
// line of the main file after its includedir as "Line <n> of <file> may
// override hardn's rule" (proposed). origin/main's file keeps sudoers'
// detail, which is in the user's words already (proposed), as they depend
// on the policy, which a finding does not carry. sudoers' own details are
// in its documentation (sudoers.md §2 item 5).
func SudoersFinding(f sudoers.Finding) engine.Finding {
	out := engine.Finding{Code: findingCode("SUDOERS", f.Kind.String()), Detail: f.Detail, Source: at(f.File, f.Line)}
	switch f.Kind {
	case sudoers.FindingLater:
		out.Detail = f.File + " may override hardn's rule"
	case sudoers.FindingAfterInclude:
		out.Detail = fmt.Sprintf("Line %d of %s may override hardn's rule", f.Line, f.File)
	}
	return out
}

func sudoersFindings(fs []sudoers.Finding) []engine.Finding {
	var out []engine.Finding
	for _, f := range fs {
		out = append(out, SudoersFinding(f))
	}
	return out
}

func dnsFindings(s dns.State, fs []dns.Finding) []engine.Finding {
	var out []engine.Finding
	for _, f := range fs {
		out = append(out, DNSFinding(s, f))
	}
	return out
}

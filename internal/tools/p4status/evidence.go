package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
)

// evidenceTitle starts the evidence, after the main menu.
const evidenceTitle = "== p4status evidence: read only; check each line against the host (docs/safety/vmtest.md §8)"

// evidence is the second part of the output: what each line of the main
// menu was read from, for a person to check against the host by hand.
// readErr is read's error, with which r holds detect's facts alone; calls
// are the guard's. Every string read from the host is shown with its
// control characters escaped.
func evidence(r reading, readErr error, calls []call) string {
	var b evidenceWriter
	b.printf("%s\n", evidenceTitle)
	b.facts(r.facts)
	if readErr != nil {
		b.printf("\nnot read: %s %s\n", output.CodeOf(readErr), clean(readErr.Error()))
	} else {
		b.rows(r.reports)
		b.header(r.header)
		b.risk(r)
	}
	b.commands(calls)
	return b.String()
}

// evidenceWriter builds the evidence, a labelled line at a time.
type evidenceWriter struct{ strings.Builder }

func (b *evidenceWriter) printf(format string, a ...any) { fmt.Fprintf(b, format, a...) }

// item writes one labelled line under a heading, its control
// characters escaped (clean).
func (b *evidenceWriter) item(label, format string, a ...any) {
	b.printf("  %-10s %s\n", label, clean(fmt.Sprintf(format, a...)))
}

// fail writes err's line, with its registry code: nothing for nil.
func (b *evidenceWriter) fail(label string, err error) {
	if err != nil {
		b.item(label, "%s %s", output.CodeOf(err), err)
	}
}

// findings writes each finding: its code, detail and source.
func (b *evidenceWriter) findings(fs []engine.Finding) {
	for _, f := range fs {
		b.item("finding", "%s %s [%s]", f.Code, f.Detail, f.Source)
	}
}

// facts writes detect's facts, which every row and the header read, as
// key=value, a value with a space in it quoted.
func (b *evidenceWriter) facts(f detect.Facts) {
	b.printf("\nfacts (detect)\n")
	b.item("os-release", "id=%s version=%s codename=%s like=%s file=%s",
		f.OS.ID, f.OS.VersionID, f.OS.VersionCodename, strings.Join(f.OS.IDLike, ","), f.OS.Path)
	b.fail("error", f.OS.Err)
	b.item("platform", "distro=%s version=%s support=%s note=%q", f.Platform.Distro, f.Platform.Version, f.Platform.Support, f.Platform.Note)
	b.fail("error", f.Platform.Err)
	b.item("init", "kind=%s running=%s source=%s", f.Init.Kind, f.Init.Running, f.Init.Source)
	b.fail("error", f.Init.Err)
	s := f.SSH
	b.item("ssh", "mode=%s source=%q ssh.socket=%s/%s ssh.service=%s/%s generator=%s generator-path=%s openrc-started=%s runlevels=%s",
		s.Mode, s.Source, s.Socket.Enabled, s.Socket.Active, s.Service.Enabled, s.Service.Active,
		s.Generator, s.GeneratorPath, s.OpenRC.Started, strings.Join(s.OpenRC.Runlevels, ","))
	b.fail("error", s.Err)
	p := f.Proxmox
	b.item("proxmox", "installed=%s version=%s source=%q pmxcfs=%s fs-type=%s", p.Installed, p.Version, p.Source, p.ClusterFS, p.ClusterFSType)
	b.fail("error", p.Err)
	c := f.Container
	b.item("container", "detected=%s signal=%s manager=%s", c.Detected, c.Signal, c.Manager)
	b.fail("error", c.Err)
	e := f.Elevation
	b.item("elevation", "sudo=%s sudo-path=%s flavour=%s version=%q doas=%s doas-path=%s",
		e.Sudo.Present, e.Sudo.Path, e.SudoFlavor, e.SudoVersion, e.Doas.Present, e.Doas.Path)
	b.fail("error", e.Err)
}

// rows writes each status row's report: its control, state, detail,
// evidence, findings and error.
func (b *evidenceWriter) rows(reports []engine.Report) {
	for _, r := range reports {
		b.printf("\nrow %s (%s)\n", r.Control, r.Title)
		b.item("state", "%s", r.Status.State)
		b.item("detail", "%s", orNone(r.Status.Detail))
		for _, ev := range r.Status.Evidence {
			b.item("evidence", "%s", ev)
		}
		b.findings(r.Status.Findings)
		b.fail("error", r.Err)
	}
}

// header writes the header probe's facts, each with its error.
func (b *evidenceWriter) header(h header.Header) {
	b.printf("\nheader\n")
	b.item("os", "%s", orNone(strings.TrimSpace(h.OS.Name+" "+h.OS.Version+" "+h.OS.Codename)))
	b.fail("error", h.OS.Err)
	b.item("hostname", "%s", orNone(h.Hostname.Name))
	b.fail("error", h.Hostname.Err)
	for _, fam := range []struct {
		name string
		f    header.Family
	}{{"ipv4", h.Address.IPv4}, {"ipv6", h.Address.IPv6}} {
		addr := "no address"
		if fam.f.Addr.IsValid() {
			addr = fam.f.Addr.String()
		}
		b.item(fam.name, "default route %s, %s, %s", fam.f.Route, orNone(fam.f.Iface), addr)
		b.fail("error", fam.f.Err)
	}
	if addr, known := h.Address.Primary(); known {
		b.item("primary", "%s", orNone(addr.String()))
	} else {
		b.item("primary", "not established")
	}
	if h.Uptime.Known {
		b.item("uptime", "%s (%d s)", output.FormatUptime(h.Uptime.Duration), int64(h.Uptime.Duration.Seconds()))
	} else {
		b.item("uptime", "not established")
	}
	b.fail("error", h.Uptime.Err)
	rb := h.Reboot
	b.item("reboot", "pending %s; running %s; host's kernel %t", rb.Pending, orNone(rb.Running), rb.HostKernel)
	for _, ev := range rb.Evidence {
		b.item("evidence", "%s", ev)
	}
	b.findings(rb.Findings)
	b.fail("error", rb.Err)
}

// risk writes the risk level: its band and score, and every factor with
// its cost.
func (b *evidenceWriter) risk(r reading) {
	l := r.level
	b.printf("\nrisk\n")
	b.item("band", "%s, score %.1f, forced %t", l.Band, l.Score, l.Forced)
	for _, f := range l.Factors {
		b.item("factor", "%s (%s, row %s, cost %d)", f.Detail, f.Kind, orNone(f.Control), f.Penalty)
	}
	b.findings(l.Findings)
}

// commands writes every command run, with its exit status, and every
// call refused, in order.
func (b *evidenceWriter) commands(calls []call) {
	refused := 0
	for _, c := range calls {
		if c.refused() {
			refused++
		}
	}
	b.printf("\ncommands (%d run, %d refused, in order)\n", len(calls)-refused, refused)
	for _, c := range calls {
		what := c.path
		if c.argv != nil {
			what = host.FormatArgv(c.argv)
		}
		var xe *host.ExitError
		switch {
		case c.refused():
			b.item("refused", "%s %s: %s", c.method, what, c.why)
		case c.err != nil && !errors.As(c.err, &xe):
			b.item("failed", "%s: %v", what, c.err)
		default:
			b.item(fmt.Sprintf("exit %d", c.exit), "%s", what)
		}
	}
}

// orNone is s, or "(none)" for an empty s.
func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// clean writes each control character of s, read from the host, as \xNN,
// so that none reaches the terminal or the log as itself.
func clean(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r >= 0x80 && r < 0xa0 {
			fmt.Fprintf(&b, `\x%02x`, r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

package logfwd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// rsyslog's files, and hardn's own among them.
const (
	// RsyslogConf and RsyslogDir are what Read reads: the main file, then
	// the directory's *.conf in name order, as Debian's, Ubuntu's and
	// CentOS Stream's rsyslog.conf include them (gallery-logforward.md
	// §3.2). A file another include names is not read.
	RsyslogConf = "/etc/rsyslog.conf"
	RsyslogDir  = "/etc/rsyslog.d"
	// DropIn is hardn's own file, and Header its first line.
	DropIn = RsyslogDir + "/60-hardn-forward.conf"
	Header = "# Written by hardn (Log Forwarding).\n"
	// GnuTLSPackage holds the TLS driver hardn's file names ("gtls").
	GnuTLSPackage = "rsyslog-gnutls"
)

// RsyslogFile is hardn's file for s: one omfwd action for every message
// (rsyslog's omfwd, §3.2), with a queue for TCP so that a server that is
// away does not block the logger, and for TLS the driver, the server's name
// and the CA file. s is checked first (Server.Check): its values are
// written between double quotes.
func RsyslogFile(s Server) ([]byte, error) {
	if err := s.Check(); err != nil {
		return nil, err
	}
	open := func(proto string) string {
		return Header + "*.* action(type=\"omfwd\" protocol=\"" + proto + "\"\n    target=\"" + s.Address + "\" port=\"" + s.Port + "\""
	}
	const queue = "\n    queue.type=\"linkedList\")\n"
	switch s.Proto {
	case UDP:
		return []byte(open("udp") + ")\n"), nil
	case TLS:
		return []byte(open("tcp") + "\n    StreamDriver=\"gtls\" StreamDriverMode=\"1\"\n    StreamDriverAuthMode=\"x509/name\"\n" +
			"    StreamDriverPermittedPeers=\"" + s.Address + "\"\n    StreamDriver.CAFile=\"" + s.CAFile + "\"" + queue), nil
	}
	return []byte(open("tcp") + queue), nil
}

// readRsyslog reads rsyslog's forwarding from RsyslogConf and RsyslogDir's
// *.conf, in name order. A file or the directory not there is skipped; one
// that cannot be read, or whose forwarding cannot be made out, is the error.
func readRsyslog(ctx context.Context, sys host.System) ([]Forward, error) {
	names := []string{RsyslogConf}
	ents, err := sys.ReadDir(ctx, RsyslogDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("logfwd: %w", err)
	}
	var drop []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".conf") {
			drop = append(drop, RsyslogDir+"/"+e.Name())
		}
	}
	slices.Sort(drop)
	var out []Forward
	tls := false // the legacy $ActionSendStreamDriverMode, as the files set it
	for _, name := range append(names, drop...) {
		data, ok, err := readFile(ctx, sys, name)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		fw, err := parseRsyslog(name, data, &tls)
		if err != nil {
			return nil, err
		}
		own := name == DropIn && bytes.HasPrefix(data, []byte(Header))
		for i := range fw {
			fw[i].ByHardn = own
		}
		out = append(out, fw...)
	}
	return out, nil
}

var (
	actionOpen  = regexp.MustCompile(`(?i)\baction\s*\(`)
	actionParam = regexp.MustCompile(`([A-Za-z0-9_.]+)\s*=\s*"((?:[^"\\]|\\.)*)"`)
)

// parseRsyslog reads data's forwarding: each action(type="omfwd" ...), on
// one line or several, its parameter names in any case; and each older
// line whose action is "@host" (UDP) or "@@host" (TCP), with its options in
// parentheses, a port after a colon, an address in square brackets and a
// template after a semicolon (rsyslog's actions page, "Remote Machine").
// tls carries the older $ActionSendStreamDriverMode from file to file: "1"
// makes the "@@" lines after it TLS. An omfwd action without a protocol or
// a port is shown as UDP, port 514: the defaults rsyslogd takes are not
// verified from its documentation. An action that does not close, or an
// omfwd action or "@" line with no host, is ErrMalformed.
func parseRsyslog(file string, data []byte, tls *bool) ([]Forward, error) {
	text := stripComments(string(data))
	bad := func(line int, what string) error {
		return fmt.Errorf("logfwd: %s:%d: %s: %w", file, line, what, ErrMalformed)
	}
	var out []Forward
	for _, m := range actionOpen.FindAllStringIndex(text, -1) {
		line := 1 + strings.Count(text[:m[0]], "\n")
		body, ok := parenBody(text[m[1]:])
		if !ok {
			return nil, bad(line, "an action that does not close")
		}
		p := map[string]string{}
		for _, kv := range actionParam.FindAllStringSubmatch(body, -1) {
			p[strings.ToLower(kv[1])] = kv[2]
		}
		if !strings.EqualFold(p["type"], "omfwd") {
			continue
		}
		s := Server{Address: p["target"], Port: p["port"], CAFile: p["streamdriver.cafile"]}
		if s.Address == "" {
			return nil, bad(line, "an omfwd action with no target")
		}
		if s.Port == "" {
			s.Port = "514"
		}
		switch {
		case p["streamdrivermode"] == "1":
			s.Proto = TLS
		case strings.EqualFold(p["protocol"], "tcp"):
			s.Proto = TCP
		}
		out = append(out, Forward{Server: s, File: file, Line: line})
	}
	for i, raw := range strings.Split(text, "\n") {
		f := strings.Fields(raw)
		if len(f) == 0 {
			continue
		}
		if strings.EqualFold(f[0], "$ActionSendStreamDriverMode") {
			*tls = len(f) > 1 && f[1] == "1"
			continue
		}
		last := f[len(f)-1]
		if len(f) < 2 || !strings.HasPrefix(last, "@") || strings.HasPrefix(f[0], "$") {
			continue
		}
		s, ok := legacyServer(last, *tls)
		if !ok {
			return nil, bad(i+1, "a forwarding line with no host")
		}
		out = append(out, Forward{Server: s, File: file, Line: i + 1})
	}
	slices.SortStableFunc(out, func(a, b Forward) int { return a.Line - b.Line })
	return out, nil
}

// legacyServer reads an older action, "@host", "@@(o,z9)host:port" or
// "@[2001:db8::1]:515;template".
func legacyServer(action string, tls bool) (Server, bool) {
	s := Server{Port: "514"}
	rest := strings.TrimPrefix(action, "@")
	if r, tcp := strings.CutPrefix(rest, "@"); tcp {
		rest, s.Proto = r, TCP
		if tls {
			s.Proto = TLS
		}
	}
	if strings.HasPrefix(rest, "(") {
		_, after, ok := strings.Cut(rest, ")")
		if !ok {
			return Server{}, false
		}
		rest = after
	}
	rest, _, _ = strings.Cut(rest, ";")
	if inner, after, ok := strings.Cut(strings.TrimPrefix(rest, "["), "]"); ok && strings.HasPrefix(rest, "[") {
		s.Address = inner
		if p, has := strings.CutPrefix(after, ":"); has && p != "" {
			s.Port = p
		}
	} else if i := strings.LastIndexByte(rest, ':'); i >= 0 {
		s.Address = rest[:i]
		if rest[i+1:] != "" {
			s.Port = rest[i+1:]
		}
	} else {
		s.Address = rest
	}
	return s, s.Address != ""
}

// stripComments blanks each "#" comment to its line's end, outside double
// quotes, keeping the line breaks, so that a line's number stays its own.
func stripComments(text string) string {
	var b strings.Builder
	quoted, comment := false, false
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c == '\n':
			comment, quoted = false, false // a quote never spans lines here: a stray one must not hide the rest
		case comment:
			continue
		case quoted && c == '\\' && i+1 < len(text) && text[i+1] != '\n':
			b.WriteByte(c)
			i++
			c = text[i]
		case c == '"':
			quoted = !quoted
		case c == '#' && !quoted:
			comment = true
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// parenBody is s up to the ")" that closes an action's "(", quotes
// honoured; ok is false where none does.
func parenBody(s string) (string, bool) {
	quoted := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case quoted && c == '\\':
			i++
		case c == '"':
			quoted = !quoted
		case c == ')' && !quoted:
			return s[:i], true
		}
	}
	return "", false
}

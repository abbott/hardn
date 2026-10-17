// Package sshd checks a candidate sshd configuration in context, before it
// is installed (refactor plan P2, next/p2-sshd-validate; brief 2.2).
//
// hardn's SSH settings live in /etc/ssh/sshd_config.d/00-hardn.conf when the
// first directive of /etc/ssh/sshd_config is the stock Include of that
// directory, and in a block at the top of sshd_config when there is no
// Include (brief §2). Either way a candidate is checked as sshd will read it:
// Validate copies sshd_config into a run directory with its Include pointed
// at a copy of sshd_config.d that holds the candidate and the other drop-ins,
// then runs sshd -t, sshd -T and one sshd -T -C per Match block against the
// copy. Nothing under /etc is written. Everything goes through host.System.
//
// docs/safety/sshd-validate.md describes the guarantees, the platform facts
// they rely on and what is written but not yet run on a Linux host.
package sshd

import (
	"context"
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// Paths sshd reads, and the drop-in hardn owns (brief §2).
const (
	MainConfig  = "/etc/ssh/sshd_config"
	DropInDir   = "/etc/ssh/sshd_config.d"
	HardnDropIn = DropInDir + "/00-hardn.conf"
	// IncludeLine is the stock first directive of sshd_config on Debian
	// 12/13, Ubuntu 22.04 to 26.04, Proxmox VE 9 and Alpine 3.19 and later.
	IncludeLine = "Include " + DropInDir + "/*.conf"
)

// directive is one line of an sshd configuration file that is neither blank
// nor a comment.
type directive struct {
	line    int    // 1-based
	text    string // the line without trailing whitespace
	keyword string // lower case: sshd compares keywords case-insensitively
	args    string // the rest of the line after the keyword, as strdelim leaves it
}

// directives splits data as sshd reads a configuration file. The loader
// drops each line's leading spaces, tabs and carriage returns
// (load_server_config); trailing whitespace and form feeds go next; the
// keyword is strdelim's first token, or its second when the first is empty
// (a leading '=' or ""); a line with no keyword, or whose keyword starts
// with '#', is skipped (servconf.c, process_server_config_line_depth).
// The caller refuses data with a NUL byte (hasNUL): sshd's loader joins the
// line to the next one there, which this does not model.
func directives(data []byte) []directive {
	var ds []directive
	for i, raw := range strings.Split(string(data), "\n") {
		text := strings.TrimRight(raw, " \t\r\n\f")
		c := cursor{s: strings.TrimLeft(text, " \t\r")}
		kw, ok := c.next()
		if ok && kw == "" {
			kw, ok = c.next()
		}
		if !ok || kw == "" || kw[0] == '#' {
			continue
		}
		ds = append(ds, directive{line: i + 1, text: text, keyword: strings.ToLower(kw), args: c.s})
	}
	return ds
}

// hasNUL returns the offset of the first NUL byte in data, or -1.
func hasNUL(data []byte) int {
	return strings.IndexByte(string(data), 0)
}

// cursor is OpenSSH's strdelim (misc.c, strdelim_internal with
// split_equals) over one line: s is what is left, and null is sshd's NULL
// pointer, the end of the line.
type cursor struct {
	s    string
	null bool
}

// next returns the next token: up to a space, tab, CR, LF or '=', which is
// dropped with the whitespace after it (and one '=' after whitespace), or,
// when a double quote comes first, the text before it joined to the text up
// to the closing quote. ok is false where strdelim returns NULL: at the end
// of the line, or when the closing quote is missing.
func (c *cursor) next() (tok string, ok bool) {
	if c.null {
		return "", false
	}
	const ws = " \t\r\n"
	i := strings.IndexAny(c.s, ws+`"=`)
	if i < 0 {
		tok, c.s, c.null = c.s, "", true
		return tok, true
	}
	if c.s[i] == '"' {
		rest := c.s[i+1:]
		j := strings.IndexByte(rest, '"')
		if j < 0 {
			c.s, c.null = "", true
			return "", false
		}
		tok, c.s = c.s[:i]+rest[:j], strings.TrimLeft(rest[j+1:], ws)
		return tok, true
	}
	tok, rest := c.s[:i], strings.TrimLeft(c.s[i+1:], ws)
	if c.s[i] != '=' && strings.HasPrefix(rest, "=") {
		rest = strings.TrimLeft(rest[1:], ws)
	}
	c.s = rest
	return tok, true
}

// splitArgs splits a directive's arguments as sshd's argv_split does: words
// separated by spaces or tabs, single or double quotes, a backslash before a
// quote, a backslash or (unquoted) a space, and a '#' starting a word ends
// the line. ok is false for an unbalanced quote.
func splitArgs(s string) (args []string, ok bool) {
	for i := 0; i < len(s); {
		if s[i] == ' ' || s[i] == '\t' {
			i++
			continue
		}
		if s[i] == '#' {
			break
		}
		var b strings.Builder
		var quote byte
	word:
		for ; i < len(s); i++ {
			c := s[i]
			switch {
			case c == '\\' && i+1 < len(s) && (strings.IndexByte(`'"\`, s[i+1]) >= 0 || quote == 0 && s[i+1] == ' '):
				i++
				b.WriteByte(s[i])
			case quote == 0 && (c == ' ' || c == '\t'):
				break word
			case quote == 0 && (c == '"' || c == '\''):
				quote = c
			case quote != 0 && c == quote:
				quote = 0
			default:
				b.WriteByte(c)
			}
		}
		if quote != 0 {
			return nil, false
		}
		args = append(args, b.String())
	}
	return args, true
}

// IncludeMode is how sshd_config reads the drop-in directory.
type IncludeMode uint8

const (
	// IncludeUnknown: not established (the zero value).
	IncludeUnknown IncludeMode = iota
	// IncludeFirst: the first directive is exactly IncludeLine, so the
	// drop-ins are read before any setting of sshd_config, in C byte order
	// among themselves. hardn writes HardnDropIn.
	IncludeFirst
	// IncludeNone: no Include directive at all, as on an Alpine host
	// upgraded from 3.18 or earlier. hardn's block goes at the top of
	// sshd_config, where cloud-init and Proxmox VE can rewrite it.
	IncludeNone
	// IncludeOther: there is an Include, but the first directive is not
	// exactly IncludeLine (another spelling, path or position). hardn has no
	// strategy for it; the caller reports it.
	IncludeOther
)

func (m IncludeMode) String() string {
	switch m {
	case IncludeFirst:
		return "include first"
	case IncludeNone:
		return "no include"
	case IncludeOther:
		return "include not first"
	}
	return "unknown"
}

// Include is what sshd_config says about the drop-in directory.
type Include struct {
	Mode IncludeMode
	Line int    // IncludeFirst, IncludeOther: the line of the first Include
	Text string // that line
}

// ParseInclude classifies the content of sshd_config. "Exactly" means byte
// for byte, after sshd's own removal of trailing whitespace. Variants sshd
// reads the same (lower case, a tab, quotes, a trailing comment) are
// IncludeOther: they are not the stock line, and cloud-init, which looks for
// a line starting "Include /etc/ssh/sshd_config.d/*.conf" to decide between
// writing a drop-in and rewriting sshd_config (cloudinit/ssh_util.py,
// _includes_dconf), may not agree with sshd about them.
func ParseInclude(main []byte) Include {
	ds := directives(main)
	for i, d := range ds {
		if d.keyword != "include" {
			continue
		}
		if i == 0 && d.text == IncludeLine {
			return Include{Mode: IncludeFirst, Line: d.line, Text: d.text}
		}
		return Include{Mode: IncludeOther, Line: d.line, Text: d.text}
	}
	return Include{Mode: IncludeNone}
}

// DetectInclude reads MainConfig and classifies it with ParseInclude.
func DetectInclude(ctx context.Context, sys host.System) (Include, error) {
	data, err := readRegular(ctx, sys, MainConfig)
	if err != nil {
		return Include{}, err
	}
	return ParseInclude(data), nil
}

// Conn is a connection for sshd -T -C: the client's user, host name and
// address, and optionally the local address, port and routing domain.
type Conn struct {
	User, Host, Addr      string // required
	LAddr, LPort, RDomain string // optional
}

// DefaultConn is the connection a Match probe starts from: a user name of
// hardn's own, a host in the reserved .invalid domain (RFC 6761) and an
// address from TEST-NET-1 (RFC 5737), so a criterion the block does not set
// is unlikely to match another block by accident.
func DefaultConn() Conn {
	return Conn{User: "hardn-probe", Host: "probe.invalid", Addr: "192.0.2.1"}
}

// Spec is c as sshd -C takes it: comma-separated key=value pairs.
func (c Conn) Spec() string {
	s := "user=" + c.User + ",host=" + c.Host + ",addr=" + c.Addr
	for _, kv := range [][2]string{{"laddr", c.LAddr}, {"lport", c.LPort}, {"rdomain", c.RDomain}} {
		if kv[1] != "" {
			s += "," + kv[0] + "=" + kv[1]
		}
	}
	return s
}

// check refuses a value sshd's comma-separated spec cannot carry.
func (c Conn) check() error {
	for _, v := range []string{c.User, c.Host, c.Addr, c.LAddr, c.LPort, c.RDomain} {
		if strings.ContainsAny(v, ", \t\r\n") {
			return fmt.Errorf("sshd: test connection %q: comma or whitespace in a value", c.Spec())
		}
	}
	if c.User == "" || c.Host == "" || c.Addr == "" {
		return fmt.Errorf("sshd: test connection %q: user, host and addr are required", c.Spec())
	}
	return nil
}

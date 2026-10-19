package sshd

import (
	"fmt"
	"strings"
)

// accumulating are the keywords whose every value, from every line of
// every file, sshd keeps: servconf.c adds Port, ListenAddress, HostKey and
// HostCertificate to lists, and AllowUsers, DenyUsers, AllowGroups,
// DenyGroups and AcceptEnv "append to list; doesn't respect
// first-match-wins". For every other keyword the first value wins.
var accumulating = map[string]bool{
	"port": true, "listenaddress": true, "hostkey": true, "hostcertificate": true,
	"allowusers": true, "denyusers": true, "allowgroups": true, "denygroups": true, "acceptenv": true,
}

// aliases are the keywords servconf.c's keywords table maps to another
// keyword's opcode, in OpenSSH 9.2 and 10.3 alike.
var aliases = map[string]string{
	"hostdsakey":                      "hostkey",
	"dsaauthentication":               "pubkeyauthentication",
	"challengeresponseauthentication": "kbdinteractiveauthentication",
	"skeyauthentication":              "kbdinteractiveauthentication",
	"keepalive":                       "tcpkeepalive",
	"pubkeyacceptedkeytypes":          "pubkeyacceptedalgorithms",
	"hostbasedacceptedkeytypes":       "hostbasedacceptedalgorithms",
}

// canonical is a directive's keyword as sshd acts on it: an alias is its
// keyword, and Subsystem is one keyword per subsystem name, since sshd
// keeps the first definition of each name (servconf.c, sSubsystem).
func canonical(keyword string, args []string) string {
	if a, ok := aliases[keyword]; ok {
		keyword = a
	}
	if keyword == "subsystem" && len(args) > 0 {
		keyword += " " + args[0]
	}
	return keyword
}

// setLine is one directive of the configuration, in the order sshd reads
// it.
type setLine struct {
	Origin
	keyword string   // canonical
	args    []string // splitArgs of the arguments
	match   *Block   // the Match block the line is under; nil: global
	hardn   bool
}

func (l setLine) value() string { return normalize(l.keyword, strings.Join(l.args, " ")) }

// sameValue reports whether l and o give the keyword the same value: their
// normalised arguments, compared as sets for an accumulating keyword.
func (l setLine) sameValue(o setLine) bool {
	if !accumulating[l.keyword] {
		return l.value() == o.value()
	}
	norm := func(args []string) []string {
		vs := make([]string, len(args))
		for i, a := range args {
			vs[i] = normalize(l.keyword, a)
		}
		return vs
	}
	return sameSet(norm(l.args), norm(o.args))
}

// flatten lists the directives of main and the drop-ins in the order sshd
// reads them: main's lines, with every drop-in's at each IncludeLine (no
// other Include is followed). A
// drop-in starts under the Match block its Include is under, and its own
// Match blocks end with it (servconf.c: "Don't let included files clobber
// the containing file's Match state"); main's run to the end of main.
func flatten(main []byte, drops []File, own Owner) []setLine {
	var ls []setLine
	var read func(path string, data []byte, under *Block)
	read = func(path string, data []byte, under *Block) {
		for _, d := range directives(data) {
			switch d.keyword {
			case "match":
				under = &Block{path, d.line, d.text}
			case "include":
				if path == MainConfig && d.text == IncludeLine {
					for _, f := range drops {
						read(f.Path, f.Data, under)
					}
				}
			default:
				args, _ := splitArgs(d.args) // unbalanced quotes fail sshd -t first
				ls = append(ls, setLine{Origin{path, d.line, d.text}, canonical(d.keyword, args), args, under, own.owns(path, d.line)})
			}
		}
	}
	read(MainConfig, main, nil)
	return ls
}

// conflicts reports, for each keyword hardn's lines set outside a Match
// block, every other line that sets it too:
//   - outside a Match block, for an accumulating keyword: it adds values
//     (FindingAccumulates);
//   - outside a Match block, read before hardn's: sshd uses it, not
//     hardn's (FindingOverridden); only the first, which wins;
//   - outside a Match block, read after hardn's, with another value: it
//     has no effect while hardn's line is there (FindingShadowed);
//   - in a Match block: for the connections the block matches, sshd uses
//     it in place of hardn's (FindingMatchOverride; servconf.c
//     copy_set_server_options).
//
// A line of hardn's own under a Match block is a FindingMatchOverride too.
func conflicts(ls []setLine) (cs []Conflict, found []Finding) {
	first, mine := map[string]int{}, map[string]int{}
	for i, l := range ls {
		if l.match != nil {
			if l.hardn {
				found = append(found, Finding{FindingMatchOverride, l.File, l.Line, fmt.Sprintf(
					"hardn's %q is under %q (%s line %d), so it applies only to the connections that block matches", l.Text, l.match.Text, l.match.File, l.match.Line)})
			}
			continue
		}
		if _, ok := first[l.keyword]; !ok {
			first[l.keyword] = i
		}
		if _, ok := mine[l.keyword]; !ok && l.hardn {
			mine[l.keyword] = i
		}
	}
	for i, l := range ls {
		h, ok := mine[l.keyword]
		if !ok || l.hardn {
			continue
		}
		c := Conflict{Keyword: l.keyword, Hardn: ls[h].Origin, Other: l.Origin, SameValue: l.sameValue(ls[h])}
		switch {
		case l.match != nil:
			c.Kind, c.Match = FindingMatchOverride, *l.match
		case accumulating[l.keyword]:
			c.Kind = FindingAccumulates
		case first[l.keyword] == i:
			c.Kind = FindingOverridden
		case first[l.keyword] == h && !c.SameValue:
			c.Kind = FindingShadowed
		default: // hardn's line loses to an earlier one, reported already, or the value is hardn's
			continue
		}
		cs = append(cs, c)
		found = append(found, Finding{c.Kind, l.File, l.Line, c.detail()})
	}
	return cs, found
}

func (c Conflict) detail() string {
	hardn := fmt.Sprintf("hardn's %q (%s line %d)", c.Hardn.Text, c.Hardn.File, c.Hardn.Line)
	switch c.Kind {
	case FindingOverridden:
		return fmt.Sprintf("%q is read before %s, so sshd uses it", c.Other.Text, hardn)
	case FindingShadowed:
		return fmt.Sprintf("%q is read after %s, so it has no effect while hardn's line is there", c.Other.Text, hardn)
	case FindingAccumulates:
		return fmt.Sprintf("%q adds to %s: sshd keeps every value of every file", c.Other.Text, hardn)
	}
	return fmt.Sprintf("%q is under %q (%s line %d): for the connections it matches, sshd uses it in place of %s",
		c.Other.Text, c.Match.Text, c.Match.File, c.Match.Line, hardn)
}

// predicted is what the files say sshd -T prints for k: the first value
// outside a Match block, or for an accumulating keyword every value of
// every such line. ok is false when the files do not say: no line, a line
// under a Match block (Match All applies to sshd -T too), or a port that is
// not a number (sshd also takes a service name).
func predicted(ls []setLine, k string) (vs []string, ok bool) {
	for _, l := range ls {
		switch {
		case l.keyword != k:
		case l.match != nil:
			return nil, false
		case accumulating[k]:
			for _, a := range l.args {
				if k == "port" && portOf(a) == 0 {
					return nil, false
				}
				vs = append(vs, normalize(k, a))
			}
		case len(vs) == 0 && len(l.args) > 0:
			vs = []string{normalize(k, l.args[0])}
		}
	}
	return vs, len(vs) > 0
}

// crossCheck compares sshd -T's values with the files' for the keywords e
// has a field for. Where they disagree, the conflict report does not
// explain what sshd does (a file changed between the reads, or a rule this
// package does not model), so the value is Unknown; for Port, the
// listeners sshd -T printed from the same reading are Unknown too.
func crossCheck(ls []setLine, e *Effective) (found []Finding) {
	disagree := func(k string, files, sshd []string, more string) {
		found = append(found, Finding{Kind: FindingDisagree, Detail: fmt.Sprintf("the files give %s %q, sshd -T %q%s", k, files, sshd, more)})
	}
	for _, k := range settingKeys {
		if s := e.setting(k); s.Known {
			if want, ok := predicted(ls, k); ok && want[0] != s.Value {
				disagree(k, want, []string{s.Value}, "")
				*s = Setting{}
			}
		}
	}
	for _, k := range setKeys {
		if s := e.set(k); s.Known {
			if want, ok := predicted(ls, k); ok && !sameSet(want, s.Values) {
				more := ""
				if k == "port" {
					e.Listeners.Known, more = false, ", so the listeners are Unknown too"
				}
				disagree(k, uniq(want), s.Values, more)
				*s = Set{}
			}
		}
	}
	return found
}

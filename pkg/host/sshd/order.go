package sshd

import "strings"

// Line is one directive of an sshd configuration, in the order sshd reads
// it (ReadOrder).
type Line struct {
	Origin // the file, the 1-based line and its text without trailing whitespace
	// Keyword is the canonical lower-case name: an alias reads as its
	// keyword (HostbasedAcceptedKeyTypes is hostbasedacceptedalgorithms),
	// and a Subsystem as "subsystem <name>", as in Conflict.Keyword.
	Keyword string
	Args    []string // the arguments as sshd splits them
	Match   *Block   // the Match block the line is under; nil outside one
}

// ReadOrder lists the directives of main (sshd_config) and drops in the
// order sshd reads them, by the rules Evaluate's conflict report uses: the
// drop-ins sshd's IncludeLine reads (DropInDir, *.conf, no leading "."),
// in C byte order, at each IncludeLine of main; no other Include is
// followed; a drop-in starts under the Match block its Include is under and
// its own Match blocks end with it. Match and Include lines themselves are
// not listed. It reads nothing and runs nothing (p2-sshd-migrate uses it to
// tell which legacy line sshd reads first).
func ReadOrder(main []byte, drops []File) []Line {
	var ls []Line
	for _, l := range flatten(main, sshdOrder(drops), Owner{}) {
		ls = append(ls, Line{Origin: l.Origin, Keyword: l.keyword, Args: l.args, Match: l.match})
	}
	return ls
}

// Accumulates reports whether sshd keeps every value of every line of a
// keyword (Port, ListenAddress, HostKey, HostCertificate, AllowUsers,
// DenyUsers, AllowGroups, DenyGroups, AcceptEnv), given in any case or as
// an alias. For every other keyword the first value read wins.
func Accumulates(keyword string) bool {
	return accumulating[canonical(strings.ToLower(keyword), nil)]
}

// CanonicalKeyword is keyword as Line.Keyword gives it: lower case, an
// alias as its keyword, and a Subsystem as "subsystem <name>" (args[0]).
func CanonicalKeyword(keyword string, args []string) string {
	return canonical(strings.ToLower(keyword), args)
}

package secondary

import (
	"errors"
	"strings"
)

// errNoUserLeft is the refusal when removing root from AllowUsers would
// leave the directive with no user: sshd rejects an empty AllowUsers, and
// keeping root while root login is refused would let no one in.
var errNoUserLeft = errors.New("AllowUsers allows only root, so no user could log in once root is refused; add a user to AllowUsers first")

// setRootLogin edits sshd_config text in place, as v0.3.2's pkg/ssh
// DisableRootSSHAccess did, from a real parse of the file:
//
//   - every PermitRootLogin directive of the global section (the lines
//     before the first Match) gets the value permitRootLogin; if there is
//     none, "PermitRootLogin <value>" is added just before the first Match
//     line, or at the end when there is no Match block;
//   - when removeRoot is set, the user "root" is removed from every global
//     AllowUsers directive; a directive left with no user is dropped, and if
//     no global AllowUsers user is left at all the edit is refused
//     (errNoUserLeft) and nothing changes.
//
// Every other line is kept byte for byte: comments, commented-out
// directives, Include lines (which are not followed), Port and
// ListenAddress, and Match blocks with whatever they set. Keywords match
// case-insensitively and may be followed by "=", as sshd_config(5) allows.
// The value replaces the first argument; anything after it on the line is
// kept.
func setRootLogin(content, permitRootLogin string, removeRoot bool) (string, error) {
	lines := strings.SplitAfter(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	firstMatch := -1
	found := false
	allowUsers, usersLeft := 0, 0
	drop := map[int]bool{}

	for i, line := range lines {
		text := strings.TrimRight(line, "\r\n")
		eol := line[len(text):]
		keyword, argAt := sshdKeyword(text)
		switch {
		case strings.EqualFold(keyword, "Match"):
			if firstMatch < 0 {
				firstMatch = i
			}
		case firstMatch >= 0:
			// Inside a Match block: kept as it is.
		case strings.EqualFold(keyword, "PermitRootLogin"):
			found = true
			lines[i] = replaceFirstArg(text, argAt, permitRootLogin) + eol
		case removeRoot && strings.EqualFold(keyword, "AllowUsers"):
			allowUsers++
			users, comment := splitArgs(text[argAt:])
			var kept []string
			for _, u := range users {
				if u != "root" && u != `"root"` {
					kept = append(kept, u)
				}
			}
			usersLeft += len(kept)
			if len(kept) == len(users) {
				continue
			}
			if len(kept) == 0 {
				drop[i] = true
				continue
			}
			rebuilt := text[:argAt] + strings.Join(kept, " ")
			if comment != "" {
				rebuilt += " " + comment
			}
			lines[i] = rebuilt + eol
		}
	}
	if removeRoot && allowUsers > 0 && usersLeft == 0 {
		return content, errNoUserLeft
	}

	var b strings.Builder
	directive := "PermitRootLogin " + permitRootLogin + "\n"
	for i, line := range lines {
		if !found && i == firstMatch {
			b.WriteString(directive)
		}
		if !drop[i] {
			b.WriteString(line)
		}
	}
	if !found && firstMatch < 0 {
		if s := b.String(); s != "" && !strings.HasSuffix(s, "\n") {
			b.WriteString("\n")
		}
		b.WriteString(directive)
	}
	return b.String(), nil
}

// sshdKeyword returns a config line's keyword and the index where its
// arguments start, after the whitespace or "=" that separates them; the
// keyword is "" for a blank line or a comment.
func sshdKeyword(text string) (keyword string, argAt int) {
	i := skipBlank(text, 0)
	if i == len(text) || text[i] == '#' {
		return "", len(text)
	}
	j := i
	for j < len(text) && text[j] != ' ' && text[j] != '\t' && text[j] != '=' {
		j++
	}
	k := skipBlank(text, j)
	if k < len(text) && text[k] == '=' {
		k = skipBlank(text, k+1)
	}
	return text[i:j], k
}

func skipBlank(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

// replaceFirstArg puts value in place of the first argument, which starts
// at argAt (a double-quoted argument ends at its closing quote), keeping
// the rest of the line.
func replaceFirstArg(text string, argAt int, value string) string {
	if argAt >= len(text) {
		head := strings.TrimRight(text, " \t")
		if strings.HasSuffix(head, "=") {
			return head + value
		}
		return head + " " + value
	}
	end := argAt
	if text[end] == '"' {
		if q := strings.IndexByte(text[end+1:], '"'); q >= 0 {
			end += q + 2
		} else {
			end = len(text)
		}
	} else {
		for end < len(text) && text[end] != ' ' && text[end] != '\t' {
			end++
		}
	}
	return text[:argAt] + value + text[end:]
}

// splitArgs splits arguments on blanks, up to a word starting with "#",
// which with the rest of the line is returned as a trailing comment.
func splitArgs(args string) (words []string, comment string) {
	fields := strings.Fields(args)
	for i, f := range fields {
		if strings.HasPrefix(f, "#") {
			return fields[:i], strings.Join(fields[i:], " ")
		}
	}
	return fields, ""
}

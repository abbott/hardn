package output

import (
	"fmt"
	"time"

	"github.com/abbott/hardn/pkg/host/accounts"
)

// The words of a last login (FormatLogin): System Details' Last Login rows'
// and the User Management screen's, beside each user's badge.
const (
	// NoLoginRecorded is an account whose record holds no login.
	NoLoginRecorded = "None recorded"
	loginFromHost   = "%s from %s"
	loginOnTTY      = "%s on %s"
)

// FormatLogin says l, a last login, at now: "Nov 16 22:31 from 10.2.8.2", or
// "Nov 16 22:31 on pts/0" with no remote host, the year in place of the clock
// where it was not now's year, as ls does; NoLoginRecorded for none
// (accounts.LoginNone); "" where nothing is known (accounts.LoginUnknown).
func FormatLogin(l accounts.LastLogin, now time.Time) string {
	switch l.State {
	case accounts.LoginUnknown:
		return ""
	case accounts.LoginNone:
		return NoLoginRecorded
	}
	when := l.At.Format("Jan 2 15:04")
	if l.At.Year() != now.Year() {
		when = l.At.Format("Jan 2 2006")
	}
	if l.From == "" {
		return fmt.Sprintf(loginOnTTY, when, l.TTY)
	}
	return fmt.Sprintf(loginFromHost, when, l.From)
}

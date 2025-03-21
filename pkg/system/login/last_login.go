// pkg/system/login/last_login.go
package login

import (
	"os/exec"
	"strings"
	"time"
)

// LastLoginProvider defines the interface for retrieving last login information
type LastLoginProvider interface {
	GetLastLogin(username string) (time.Time, error)
}

// OSLastLoginProvider implements LastLoginProvider for *nix systems
type OSLastLoginProvider struct {
	// UseLastlog determines which command to use
	// true: use lastlog (from /var/log/lastlog)
	// false: use last (from /var/log/wtmp)
	UseLastlog bool
}

// NewOSLastLoginProvider creates a new OSLastLoginProvider with default settings
func NewOSLastLoginProvider() *OSLastLoginProvider {
	return &OSLastLoginProvider{
		UseLastlog: true, // Use lastlog by default
	}
}

// GetLastLogin retrieves the last login time for the specified user
func (p *OSLastLoginProvider) GetLastLogin(username string) (time.Time, error) {
	var cmd *exec.Cmd

	if p.UseLastlog {
		cmd = exec.Command("lastlog", "-u", username)
	} else {
		cmd = exec.Command("last", "-1", username)
	}

	output, err := cmd.Output()
	if err != nil {
		return time.Time{}, err
	}

	// Parse the output to get the timestamp
	// This will depend on which command is used and may need to be adjusted
	if p.UseLastlog {
		return parseLastlogOutput(output)
	}
	return parseLastOutput(output)
}

// parseLastlogOutput extracts the timestamp from lastlog output
func parseLastlogOutput(output []byte) (time.Time, error) {
	// Sample output:
	// Username         Port     From             Latest
	// bruce                                      **Never logged in**
	// or
	// bruce            pts/0    192.168.1.5      Mon Mar 18 15:30:45 -0700 2025

	outputStr := string(output)
	lines := strings.Split(outputStr, "\n")

	if len(lines) < 2 {
		return time.Time{}, nil // No login data
	}

	// Skip header line, check the user line
	userLine := lines[1]

	if strings.Contains(userLine, "Never logged in") {
		return time.Time{}, nil
	}

	// Extract date part - format depends on system locale
	// This is a simplistic approach and may need to be adjusted
	fields := strings.Fields(userLine)
	if len(fields) < 5 {
		return time.Time{}, nil
	}

	// Assuming format like "Mon Mar 18 15:30:45 -0700 2025"
	// Starting from 4th field (index 3)
	timeStr := strings.Join(fields[3:], " ")

	// Parse with a flexible time format
	// Note: This is a simplified example and may need adjustment based on actual output format
	t, err := time.Parse("Jan 2 15:04:05 2006", timeStr)
	if err != nil {
		// Try alternative format
		t, err = time.Parse("Mon Jan 2 15:04:05 -0700 2006", timeStr)
		if err != nil {
			return time.Time{}, err
		}
	}

	return t, nil
}

// parseLastOutput extracts the timestamp from last command output
func parseLastOutput(output []byte) (time.Time, error) {
	// Sample output:
	// bruce    pts/0        192.168.1.5      Mon Mar 18 15:30 - 16:45  (01:15)

	outputStr := string(output)
	if outputStr == "" {
		return time.Time{}, nil // No login data
	}

	lines := strings.Split(outputStr, "\n")
	if len(lines) == 0 || lines[0] == "" {
		return time.Time{}, nil
	}

	fields := strings.Fields(lines[0])
	if len(fields) < 5 {
		return time.Time{}, nil
	}

	// Extract date parts - typically fields starting from 3 or 4 depending on format
	// This is a simplistic approach and may need to be adjusted
	dateStartIdx := 3
	for i, field := range fields {
		if strings.HasPrefix(field, "Mon") || strings.HasPrefix(field, "Tue") ||
			strings.HasPrefix(field, "Wed") || strings.HasPrefix(field, "Thu") ||
			strings.HasPrefix(field, "Fri") || strings.HasPrefix(field, "Sat") ||
			strings.HasPrefix(field, "Sun") {
			dateStartIdx = i
			break
		}
	}

	// Join date parts
	dateStr := strings.Join(fields[dateStartIdx:dateStartIdx+4], " ")

	// Parse with a flexible time format
	// Note: This may need adjustment based on actual output format
	t, err := time.Parse("Mon Jan 2 15:04", dateStr)
	if err != nil {
		return time.Time{}, err
	}

	// Set current year as last command doesn't include it
	currentYear := time.Now().Year()
	return time.Date(currentYear, t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, t.Location()), nil
}

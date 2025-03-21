// pkg/adapter/secondary/lastlog_command_adapter.go
package secondary

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	domainports "github.com/abbott/hardn/pkg/domain/ports/secondary"
)

// LastlogCommandAdapter implements UserLoginPort using the 'lastlog' command
type LastlogCommandAdapter struct {
}

// NewLastlogCommandAdapter creates a new LastlogCommandAdapter
func NewLastlogCommandAdapter() domainports.UserLoginPort {
	return &LastlogCommandAdapter{}
}

// GetLastLoginTime implements UserLoginPort.GetLastLoginTime
func (a *LastlogCommandAdapter) GetLastLoginTime(username string) (time.Time, error) {
	cmd := exec.Command("lastlog", "-u", username)
	output, err := cmd.Output()
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to execute lastlog command: %w", err)
	}

	return a.parseLastlogOutput(output)
}

// GetLastLoginInfo implements UserLoginPort.GetLastLoginInfo
func (a *LastlogCommandAdapter) GetLastLoginInfo(username string) (time.Time, string, error) {
	cmd := exec.Command("lastlog", "-u", username)
	output, err := cmd.Output()
	if err != nil {
		return time.Time{}, "", fmt.Errorf("failed to execute lastlog command: %w", err)
	}

	loginTime, err := a.parseLastlogOutput(output)
	if err != nil {
		return time.Time{}, "", err
	}

	// Extract IP address if available
	ipAddress := ""
	lines := strings.Split(string(output), "\n")
	if len(lines) >= 2 && !strings.Contains(lines[1], "Never logged in") {
		fields := strings.Fields(lines[1])
		// The IP or hostname is typically in the third column
		if len(fields) >= 3 {
			// Check if it looks like an IP address
			ipPattern := regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`)
			if ipPattern.MatchString(fields[2]) {
				ipAddress = fields[2]
			}
		}
	}

	return loginTime, ipAddress, nil
}

// parseLastlogOutput extracts the timestamp from lastlog output
func (a *LastlogCommandAdapter) parseLastlogOutput(output []byte) (time.Time, error) {
	outputStr := string(output)
	lines := strings.Split(outputStr, "\n")

	// lastlog output has a header line and then user data
	if len(lines) < 2 {
		return time.Time{}, fmt.Errorf("unexpected format in lastlog output")
	}

	// Check if user has never logged in
	if strings.Contains(lines[1], "Never logged in") {
		return time.Time{}, nil
	}

	// Extract timestamp portion
	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return time.Time{}, fmt.Errorf("not enough fields in lastlog output")
	}

	// Determine where the date/time part starts
	var startIdx int
	// If there's an IP or hostname in the output, adjust accordingly
	if strings.Contains(fields[2], ".") || strings.Contains(fields[2], ":") {
		// There's an IP or hostname in the output
		startIdx = 3
	} else {
		// No IP/hostname, so date starts earlier
		startIdx = 2
	}

	// Make sure we have enough fields
	if len(fields) < startIdx+4 {
		return time.Time{}, fmt.Errorf("not enough fields for timestamp in lastlog output")
	}

	// Combine the date and time fields
	// Format is typically: Weekday Month Day Time Year
	timeStr := strings.Join(fields[startIdx:startIdx+5], " ")

	// Try to parse the timestamp
	t, err := time.Parse("Mon Jan 2 15:04:05 2006", timeStr)
	if err != nil {
		// Try without seconds
		t, err = time.Parse("Mon Jan 2 15:04 2006", timeStr)
		if err != nil {
			// Try without year
			shorterTimeStr := strings.Join(fields[startIdx:startIdx+4], " ")
			t, err = time.Parse("Mon Jan 2 15:04:05", shorterTimeStr)
			if err != nil {
				return time.Time{}, fmt.Errorf("failed to parse time: %w", err)
			}

			// Add current year if not present
			if t.Year() == 0 {
				currentYear := time.Now().Year()
				current := time.Now()
				t = time.Date(currentYear, t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, current.Location())
			}
		}
	}

	return t, nil
}

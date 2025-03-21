// pkg/adapter/secondary/last_command_adapter.go
package secondary

import (
	"fmt"
	"strings"
	"time"

	domainports "github.com/abbott/hardn/pkg/domain/ports/secondary"
	"github.com/abbott/hardn/pkg/interfaces"
)

// LastCommandAdapter implements UserLoginPort using the 'last' command
type LastCommandAdapter struct {
	commander interfaces.Commander
}

// NewLastCommandAdapter creates a new LastCommandAdapter
func NewLastCommandAdapter(commander interfaces.Commander) domainports.UserLoginPort {
	return &LastCommandAdapter{
		commander: commander,
	}
}

// GetLastLoginTime implements UserLoginPort.GetLastLoginTime
func (a *LastCommandAdapter) GetLastLoginTime(username string) (time.Time, error) {
	lastLoginOutput, err := a.commander.Execute("last", "-1", username)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to execute last command: %w", err)
	}

	return a.parseLastOutput(lastLoginOutput)
}

// GetLastLoginInfo implements UserLoginPort.GetLastLoginInfo
func (a *LastCommandAdapter) GetLastLoginInfo(username string) (time.Time, string, error) {
	lastLoginOutput, err := a.commander.Execute("last", "-1", username)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("failed to execute last command: %w", err)
	}

	loginTime, err := a.parseLastOutput(lastLoginOutput)
	if err != nil {
		return time.Time{}, "", err
	}

	// Extract IP address if available
	ipAddress := ""
	lines := strings.Split(string(lastLoginOutput), "\n")
	if len(lines) > 0 && !strings.Contains(lines[0], "wtmp begins") {
		fields := strings.Fields(lines[0])
		// The IP address is typically in the third field if present
		if len(fields) >= 3 {
			// Check if it looks like an IP address (simple check)
			if strings.Contains(fields[2], ".") {
				ipAddress = fields[2]
			}
		}
	}

	return loginTime, ipAddress, nil
}

// parseLastOutput extracts the timestamp from last command output
func (a *LastCommandAdapter) parseLastOutput(output []byte) (time.Time, error) {
	outputStr := string(output)
	lines := strings.Split(strings.TrimSpace(outputStr), "\n")

	// Check if there's any actual login data
	if len(lines) == 0 || strings.Contains(lines[0], "wtmp begins") {
		// No login history found
		return time.Time{}, nil
	}

	// Parse the first line (most recent login)
	fields := strings.Fields(lines[0])
	if len(fields) < 5 {
		return time.Time{}, fmt.Errorf("unexpected format in last command output")
	}

	// Extract timestamp fields
	// Typical format: username pts/0 192.168.1.1 Wed Mar 6 19:30:00 2025
	timestampStart := 3 // Skip username, terminal, and IP
	// If there's no IP address, adjust start position
	if !strings.Contains(fields[2], ".") {
		timestampStart = 2
	}

	// Make sure we have enough fields for a timestamp
	if len(fields) < timestampStart+4 {
		return time.Time{}, fmt.Errorf("not enough fields for timestamp")
	}

	// Construct time string - format varies but typically contains:
	// Day of week, Month, Day, Time, Year (if available)
	timeStr := strings.Join(fields[timestampStart:timestampStart+4], " ")

	// Try to parse with various formats
	t, err := time.Parse("Mon Jan 2 15:04:05", timeStr)
	if err != nil {
		// Try without seconds
		t, err = time.Parse("Mon Jan 2 15:04", timeStr)
		if err != nil {
			// Try with year
			for i := 0; i < 2; i++ {
				if timestampStart+4+i < len(fields) {
					extendedTimeStr := timeStr + " " + fields[timestampStart+4+i]
					t, err = time.Parse("Mon Jan 2 15:04:05 2006", extendedTimeStr)
					if err == nil {
						return t, nil
					}
				}
			}
			return time.Time{}, fmt.Errorf("failed to parse time: %w", err)
		}
	}

	// If year is not included in the output, use current year
	if t.Year() == 0 {
		currentYear := time.Now().Year()
		current := time.Now()
		t = time.Date(currentYear, t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, current.Location())
	}

	return t, nil
}

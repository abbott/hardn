// pkg/system/user_info.go
package system

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/domain/model"
)

// collectUserInfo gathers non-system user information
func (m *SystemDetails) collectUserInfo(hostInfoManager *application.HostInfoManager) error {
	// Get non-system users from Host Info Service
	users, err := hostInfoManager.GetNonSystemUsers()
	if err != nil {
		// Fallback to older implementation
		users, err = getNonSystemUsers()
		if err != nil {
			return fmt.Errorf("failed to get non-system users: %w", err)
		}
	}
	m.Users = users
	return nil
}

// getNonSystemUsers retrieves non-system users from the system
func getNonSystemUsers() ([]model.User, error) {
	var users []model.User

	// Read /etc/passwd to get all users
	file, err := os.Open("/etc/passwd")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Common threshold for non-system users across Linux distributions
	minUID := 1000 // Both Alpine and Debian/Ubuntu use 1000 as the starting UID for regular users

	// Parse /etc/passwd file
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Split(line, ":")
		if len(fields) >= 7 {
			username := fields[0]
			uid, err := strconv.Atoi(fields[2])

			// Skip if we can't parse the UID or if it's a system user
			if err != nil || uid < minUID {
				continue
			}

			// Skip common service accounts even if they have high UIDs
			if username == "nobody" || username == "nfsnobody" {
				continue
			}

			// Check if user has sudo access (either in sudo group or in sudoers file)
			hasSudo := checkSudoAccess(username)

			users = append(users, model.User{
				Username: username,
				HasSudo:  hasSudo,
			})
		}
	}

	return users, scanner.Err()
}

// checkSudoAccess checks if a user has sudo access
func checkSudoAccess(username string) bool {
	// Check if user is in sudo/wheel/admin group
	for _, group := range []string{"sudo", "wheel", "admin"} {
		cmd := exec.Command("groups", username)
		output, err := cmd.Output()
		if err == nil && strings.Contains(string(output), group) {
			return true
		}
	}

	// Check sudoers file
	cmd := exec.Command("sudo", "-l", "-U", username)
	output, err := cmd.Output()
	if err == nil && !strings.Contains(string(output), "not allowed to run sudo") {
		return true
	}

	return false
}

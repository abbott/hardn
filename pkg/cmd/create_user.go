package cmd

import "strings"

// authorizedKeysSSHUOnly: DISABLED 2026-10-17. Flip to true to restore.
//
// With it true, hardn -c passes the sshKeyPath setting (default ".ssh_%u")
// to SSHManager.ConfigureSSH as the only AuthorizedKeysFile, so hardn.conf
// says "AuthorizedKeysFile .ssh_%u": sshd then reads keys from the file
// ~/.ssh_<user> (a relative path is taken from the home directory and %u is
// the user name, sshd_config(5)), while -c writes them to
// ~/.ssh/authorized_keys and the same file turns password authentication
// off. Every key user, root included, is locked out of new connections
// (V5), and the keys a v0.3.2 install kept in ~/.ssh_<user>/authorized_keys
// stop working too (V24).
//
// With it false, -c writes v0.3.2's pair (git show v0.3.2:pkg/ssh/ssh.go,
// the AuthorizedKeysFile lines of WriteSSHConfig): .ssh/authorized_keys and
// <sshKeyPath>/authorized_keys, on one line, so keys on standard hosts and
// on v0.3.2-hardened hosts both work. v0.3.2 separated the words with four
// spaces; hardn.conf uses one, like its other lines.
const authorizedKeysSSHUOnly = false

// CreateUserAuthorizedKeysFiles returns the AuthorizedKeysFile paths
// hardn -c passes to SSHManager.ConfigureSSH for the sshKeyPath setting,
// which v0.3.2 and the user screens read as a directory under the home
// directory holding authorized_keys. A value ending in authorized_keys
// names the file itself, as the user screens read it
// (OSUserRepository.GetExtendedUserInfo) and as origin/main wrote it, so
// it is listed as it is. An empty sshKeyPath adds nothing to
// .ssh/authorized_keys, and neither does one naming .ssh itself.
//
// Any other value is taken as a directory, as v0.3.2 took it, including a
// per-user file such as /etc/ssh/authorized_keys/%u, which then gets
// /authorized_keys appended; which forms sshKeyPath accepts is plan §3.17's.
func CreateUserAuthorizedKeysFiles(sshKeyPath string) []string {
	if authorizedKeysSSHUOnly {
		return []string{sshKeyPath}
	}
	paths := []string{".ssh/authorized_keys"}
	if dir := strings.TrimRight(sshKeyPath, "/"); dir != "" {
		p := dir + "/authorized_keys"
		if strings.HasSuffix(dir, "authorized_keys") {
			p = dir
		}
		if p != paths[0] {
			paths = append(paths, p)
		}
	}
	return paths
}

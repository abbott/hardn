package cmd

// DisableRootDone is what hardn -d reports once SSHManager.DisableRootSSH
// has succeeded. proxmox is the ServiceFactory's IsProxmoxHost. On a
// Proxmox VE host DisableRootSSH sets root login to key-only
// (PermitRootLogin prohibit-password) rather than turning it off, because
// cluster nodes log in to each other as root with keys (brief §2.5), so it
// says that instead of "disabled". proxmox is false, and the message is
// origin/main's, while pkg/adapter/secondary's proxmoxTreatedAsDebian is
// set.
func DisableRootDone(proxmox bool) string {
	if proxmox {
		return "Root SSH login set to key-only (PermitRootLogin prohibit-password), not disabled: Proxmox VE clustering needs root key login"
	}
	return "Root SSH access disabled"
}

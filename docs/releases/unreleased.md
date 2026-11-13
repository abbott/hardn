# Unreleased

## New

- **SSH Login shows sshd's own brute-force limit.** The row "Brute-Force Limit" reads On, Off (with the file that turns it off) or "Not available" on an sshd before OpenSSH 9.8. It changes nothing.
- **Firewall can limit repeated SSH connections (item 5, Limit SSH).** On an active ufw that allows SSH from anywhere, it turns that rule into ufw's limit: an address that opens 6 or more connections in 30 seconds is refused, yours included. `ufw allow <port>/tcp` undoes it. Not on Rocky, Alma or RHEL, and not part of Run All.

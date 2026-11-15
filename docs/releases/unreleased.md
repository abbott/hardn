# hardn (next release)

> **Draft: every line is proposed until the maintainer approves the words.** At the release candidate this file becomes `docs/releases/v<x.y.z>.md`, the release's whole body, and this note is cut.

## Changed

- **The main menu has one SSH row.** It was two, SSH Login and SSH Port. With root login off, passwords off and the port moved it reads "+ key-only, no root, port 2230".
- **A host still on port 22 reads "Partial default port (22)"** once root login and passwords are off. It used to show a Configured SSH Login row and a Not Configured SSH Port row. The risk level is unchanged.
- **Where root login or passwords are not set, the row says that**, as before ("Not Configured password auth on"), with "~ Default port (22)" under it where the port is 22.
- **Main menu 2 is "SSH"**, and so is its screen's title. It was "SSH Login". The screen shows the same one row; its items are unchanged.
- **`hardn check`, the JSON and the report are unchanged**: `ssh.login`, `ssh.auth` and `ssh.port` each keep their row, state and codes.
- **Words that name the screen follow it**: "Select option: 2) SSH", Run All's "SSH's: 2230", "upgrade openssh in SSH (5)".

- **The firewall and DNS screens are titled "Firewall Settings" and "DNS Settings".** They were "UFW Firewall Configuration", "Firewall Configuration" and "DNS Configuration".

## Fixed

- **The DNS screen shows the nameservers and search domains under NetworkManager, resolvconf, cloud-init or another program.** They read "Unknown". hardn still only reports there.
- **The SSH screen marks the row behind a Partial status.** Root SSH Login is marked where it is why SSH is Partial, SSH Port where 22 is still open.
- **A "Refused" SSH listener is no longer marked as a fault where the same port is let in on the other IP version**, such as IPv6 refused beside IPv4 allowed.

- **Change SSH port works where ufw lets SSH in from some addresses alone.** It was refused, and adding the rules for the new port by hand did not help. hardn now gives the new port the same ufw rules, before sshd moves. It still refuses a rule with a source port, and a session whose address no rule covers.
- **Change SSH port works where ufw has a rule for every port, such as `ufw allow from 10.0.0.0/8`, beside SSH open to anyone.** It was refused (`UFW_WOULD_BLOCK`). hardn now opens the new port as the old one is.

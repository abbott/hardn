# hardn (next release)

> **Draft: every line is proposed until the maintainer approves the words.** At the release candidate this file becomes `docs/releases/v<x.y.z>.md`, the release's whole body, and this note is cut.

## Changed

- **The main menu's rows are shorter.** A row that is configured shows `+` and its detail, such as "+ deny policy", where it said "Configured deny policy". A row that needs work keeps its word: Partial, Not Configured, Not Applicable or Unknown. Every screen's own row reads the same. `hardn check`, the JSON and the Markdown report still say "Configured".
- **A line under a row starts with `~`**, such as "~ No password to become root", where it started with "-". So does "~ Reboot pending" under the uptime.
- **SSH Login and SSH Auth are one row, SSH Login.** It shows the worse of the two and what is off, such as "Not Configured password auth on"; with both on, "+ root disallowed, key-only auth". The SSH Login screen has the same row. Nothing else changed: `ssh.login` and `ssh.auth` are still two controls in `hardn check`, the JSON and the report, with the same findings and codes.
- **Kernel has a row on every host, and is main menu 4.** It reads "6 to set", "+ 6 items applied" once hardn's file sets them, "+ baseline default" where the host already met the baseline, and Not Applicable in a container. Run All still applies Kernel only with `kernel: true` in hardn.yml.
- **The risk level counts Kernel: 5 points, taken from Auto Updates (20 to 15).** A host hardened with v0.10.0 scores 95 until Kernel is applied. With no line under a row that is still Minimal. With automatic updates off a host scores 5 more, once Kernel is applied.
- **DNS is under Configuration (main menu 8, then 2).** It was main menu 4. Syslog Settings moved from 8, then 2 to 8, then 4; NTP Settings stays at 3. Kernel Settings left Configuration for main menu 4.
- **DNS, NTP and Syslog have a main-menu row only with a problem**, under the other rows: the host's value and the problem under it, such as a syslog server and "Unreachable", or nameservers and "Not hardn.yml's". While they are fine they have no row. No time service, and no log forwarding, are a problem only where hardn.yml names servers for it: a host with no time service and no `ntpServers` no longer shows an NTP row. They never changed the risk level and still don't.
- **"not reached" is now "unreachable"** on the Syslog row and screen: "server unreachable", "1 of 3 unreachable".
- **The main menu's items have new words.** Item 1 is "User" (the screen is still User Management). The keys 0 to 10 are unchanged.
- **The Markdown report lists Kernel, and DNS last.** DNS was between Firewall and Auto Updates.

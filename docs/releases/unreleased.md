# hardn (next release)

> **Draft: every line is proposed until the maintainer approves the words.** At the release candidate this file becomes `docs/releases/v<x.y.z>.md`, the release's whole body, and this note is cut.

## Added

- **hardn check compares the NTP servers**; a baseline saved before is still read, and --accept adds them.

## Fixed

- **Sub-menus open and close on a cleared screen**, as the main menu's items do.
- **NTP Settings and Syslog Settings have Refresh**, as System Details has: it reads the host again and redraws the screen.
- **install.sh names doas on a doas host.** Its last line says "doas hardn", and run as a user it says "doas sh". With neither sudo nor doas it says "Run hardn --help, or hardn as root".
- **A syslog server's protocol can be changed.** hardn.yml's editor has Change protocol under `syslogServers`; a default port follows the new protocol. busybox's UDP-only line points to it.
- **The first run asks the syslog server's protocol:** "Syslog protocol (udp, tcp, tls) [tcp]:". tls asks a CA file.
- **Turning automatic updates on saves `enableAutoUpdates: true`**, as turning them off saves false.
- **The main menu's User item names doas on a doas host**: "Create, manage (doas, SSH keys)". It said sudo on every host.

## Changed

- **`enableUnattendedUpgrades` is now `enableAutoUpdates`**; the old name is still read, with a warning.
- **The release check defaults to yes.** "Check for new hardn releases, once a day? [Y/n]:": Enter is yes, wherever it is asked. n or no is no; another answer is taken as no, and hardn says so.
- **Manage User's sudo or doas row reads "Enabled, passwordless"** for a rule that asks no password. It was "Enabled, No password to ask for".

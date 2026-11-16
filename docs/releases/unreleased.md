# hardn (next release)

> **Draft: every line is proposed until the maintainer approves the words.** At the release candidate this file becomes `docs/releases/v<x.y.z>.md`, the release's whole body, and this note is cut.

## Fixed

- **NTP Settings and Syslog Settings have Refresh**, as System Details has: it reads the host again and redraws the screen.
- **install.sh names doas on a doas host.** Its last line says "doas hardn", and run as a user it says "doas sh". With neither sudo nor doas it says "Run hardn --help, or hardn as root".
- **The main menu's User item names doas on a doas host**: "Create, manage (doas, SSH keys)". It said sudo on every host.

## Changed

- **The release check defaults to yes.** "Check for new hardn releases, once a day? [Y/n]:": Enter is yes, wherever it is asked. n or no is no; another answer is taken as no, and hardn says so.
- **Manage User's sudo or doas row reads "Enabled, passwordless"** for a rule that asks no password. It was "Enabled, No password to ask for".

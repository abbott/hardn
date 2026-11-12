# hardn (next release)

> **Draft: every line is proposed until the maintainer approves the words.** At the release candidate this file becomes `docs/releases/v<x.y.z>.md`, the release's whole body, and this note is cut.

## Changed

- **SSH Login says what to do when `ssh.socket` and `ssh.service` are both enabled.** hardn can't tell which will start sshd after a restart, so it changes nothing. It used to print the error as it was; now it names the two units and the one to disable: "Run systemctl disable ssh.socket (ssh.service is the one running), then try again." The code is still `RELOAD_MODE_UNKNOWN`.
- **Under doas, Remove admin rights now removes a rule that keeps `HARDN_CONFIG`** along with the variable. It used to be refused until Environment stopped keeping it.

## Removed

- **The Environment screen, `-e` and `setup-sudo-env` are gone.** They kept `HARDN_CONFIG` under sudo or doas. Use `sudo hardn -f <path>`, `sudo HARDN_CONFIG=<path> hardn` or `doas env HARDN_CONFIG=<path> hardn` instead, or put the file at `/etc/hardn/hardn.yml`.
- **A line hardn already wrote keeps working.** To take it away under sudo, delete `/etc/sudoers.d/hardn-env-<user>`.
- **The main menu has nine items:** System Details is 8 and Logs is 9.
- **Run All no longer shows an Environment row.**

# hardn (next release)

> **Draft: every line is proposed until the maintainer approves the words.** At the release candidate this file becomes `docs/releases/v<x.y.z>.md`, the release's whole body, and this note is cut.

## Changed

- **SSH Login says what to do when `ssh.socket` and `ssh.service` are both enabled.** hardn can't tell which will start sshd after a restart, so it changes nothing. It used to print the error as it was; now it names the two units and the one to disable: "Run systemctl disable ssh.socket (ssh.service is the one running), then try again." The code is still `RELOAD_MODE_UNKNOWN`.

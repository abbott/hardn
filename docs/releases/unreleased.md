# hardn (next release)

> **Draft: every line is proposed until the maintainer approves the words; those below were approved on 2026-11-11.** At the release candidate this file becomes `docs/releases/v<x.y.z>.md`, the release's whole body, and this note is cut.

## Changed

- **SSH rows say when sshd has no host keys,** as on an Alpine container where sshd was installed but never started: "sshd off, no host keys" in place of "can't read". SSH Login's changes, `-d` and Run All say what to do: make the keys, or, where sshd is off, start it or remove it (`SSHD_ENV_NO_HOSTKEYS`).
- **Enable firewall lists the other ports it will block,** such as a web app's, and how to keep one open (`FIREWALL_PORTS_BLOCKED`). It adds no rule for them. For ufw, and for firewalld on Rocky Linux, AlmaLinux and RHEL.
- **The Firewall screen says when hardn runs in a container:** its host may filter traffic too, where hardn can't see it.
- **Create User installs shadow on Alpine** where useradd is missing, after asking. `-c`, `-u` and Run All still refuse, and name doas too when it is missing.

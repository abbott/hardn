# CVE checks

> [!IMPORTANT]
> `hardn` doesn’t write its own patches. For each CVE below, it checks whether the host has the vendor’s fix, alerts when the fix is missing, refuses any change the vulnerability would make unsafe, and installs the fix where it can.

## CVE-2026-123158, Alpine (aka "*sshole")
OpenSSH before 10.6p2 ([Alpine's advisory](https://alpinelinux.org/posts/2026-11-05-openssh-password-authentication-bypass/)), on musl without PAM, lets a password of 257 to 1024 bytes into an account whose password field is `*`, as a key-only account's is. hardn compares the installed `openssh-server` with each Alpine branch's first fixed release:
  - **The main menu's SSH Auth row** reports an unfixed sshd (`SSH_SSHD_UNFIXED`, or `SSH_SSHD_UNFIXED_LATENT` with password login off), or an exposed host (`SSH_PASSWORD_BYPASS`, risk level Critical) where such an account can be reached by password. The main menu shows an alert for either.
  - **Creating a user is refused** (`USERS_PASSWORD_BYPASS`) on an unfixed sshd with password login on, since a new user's field is `*`.
  - **Turning password login on is refused** (`SSH_PASSWORDS_UNFIXED`) on an unfixed sshd, for the same reason.
  - **To fix it,** select `2) SSH Login`, then `5) Upgrade openssh`, or select `6) Run All`: hardn upgrades every openssh package, then reloads sshd. Or turn password login off: select `2) SSH Login`, then `2) Key-only login`.

Learn more: [alpine.md](safety/alpine.md) (the fixed releases by branch) and [check-ssh.md](engine/check-ssh.md).


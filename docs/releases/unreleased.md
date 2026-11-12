# hardn (next release)

> **Draft: every line is proposed until the maintainer approves the words.** At the release candidate this file becomes `docs/releases/v<x.y.z>.md`, the release's whole body, and this note is cut.

## Changed

- **Admin rights without a password are opt-in.** A user created by Create User, `-c` or Run All used to get sudo's `NOPASSWD` rule (doas's `nopass`). Now the rule asks their password. To keep the old rule, set `adminNoPassword: true` in hardn.yml, or answer no to Create User's "Require password for admin rights?".
- **Create User offers to set the new admin's password:** on yes it runs `passwd <name>` on your terminal once the user is created. The password is typed to `passwd`, never to hardn.
- **`-c` and Run All set no password.** The new admin can't use sudo until root runs `passwd <name>`, and the plan says so (`USERS_PASSWORD_NOT_SET`). So `-c -d` on a host with no other admin now refuses `-d`, unless `adminNoPassword` is true.
- **Grant admin rights asks the user's password too,** even when they have none yet. Manage User's "Don't ask password" still removes the question for one user.
- **`sudoNoPassword` is retired** for `adminNoPassword`. `sudoNoPassword: true` is ignored, as every hardn.yml an older hardn saved holds it; `false` warns that it isn't used.
- **Users hardn already created keep their no-password rule.** To tighten one, run `passwd <name>`, then choose Manage User's "Ask for password".

## Removed

- **The Environment screen, `-e` and `setup-sudo-env` are gone.** They kept `HARDN_CONFIG` under sudo or doas. Use `sudo hardn -f <path>`, `sudo HARDN_CONFIG=<path> hardn` or `doas env HARDN_CONFIG=<path> hardn` instead, or put the file at `/etc/hardn/hardn.yml`.
- **A line hardn already wrote keeps working.** To take it away under sudo, delete `/etc/sudoers.d/hardn-env-<user>`.
- **The main menu has nine items:** System Details is 8 and Logs is 9.
- **Run All no longer shows an Environment row.**
- **Under doas, Remove admin rights now removes a rule that keeps `HARDN_CONFIG`** along with the variable. It used to be refused until Environment stopped keeping it.


# Roadmap

Features hardn doesn't have yet. None has a release or a date.

## Platforms

- **Arch Linux.** Arch has no version: its os-release has `BUILD_ID=rolling` and no `VERSION_ID`, so it would get one support row with no version, tested against the current packages. SSH fits as it is: Arch's stock `sshd_config` includes `sshd_config.d`, and `sshdgenkeys.service` makes missing host keys. sudo isn't in `base`, and ufw and firewalld are both packaged, with iptables on nftables. Two controls don't fit Arch's model:
  - **Installing a package is a full upgrade.** Arch doesn't support partial upgrades, so Install UFW would run `pacman -Syu ufw` and upgrade the whole host first. The review would have to say so.
  - **Auto Updates.** Arch has no security-only updates, and its wiki asks you to read the Arch news before upgrading, since some updates need manual steps. Auto Updates would likely stay read only there.

  AppArmor needs a kernel parameter to turn on. Arch's security tracker is marked out of date. Official packages are x86_64 only; aarch64 is Arch Linux ARM, a separate project.
- **Fedora Server.** Close to Rocky, Alma and RHEL: firewalld, SELinux enforcing, `sshd_config.d` with Red Hat's drop-ins, host keys made by `sshd-keygen`. What differs:
  - **Versions.** os-release has `ID=fedora` and `VARIANT_ID=server`, with no `ID_LIKE`. A release comes every six months and is supported about 13 months (45 and 44 now; 43 ends 2026-12-01), so the support rows would change twice a year.
  - **dnf5,** the default since Fedora 41. Its install output ends with "Transaction Summary:", where hardn's dnf parser stops at dnf 4's "Transaction Summary". Its automatic updates (`dnf5-plugin-automatic`) keep their defaults under `/usr/share/dnf5` and have no `dnf-automatic-install.timer`; hardn's EL Auto Updates needs both `/etc/dnf/automatic.conf` and that timer.
  - **Firewall.** The default zone is `FedoraServer`, which lets in ssh, dhcpv6-client and cockpit, and Cockpit listens on 9090 from the start. The Firewall screen would say so.
  - **DNS.** systemd-resolved manages it, as on Ubuntu, where EL has NetworkManager.

## Configuration

- **Edit hardn.yml from the menu.** `8) Configuration` shows which hardn.yml hardn read and each key's value, but changes nothing. Firewall, DNS, Auto Updates and Backup already save their own keys: only those lines change, comments stay, and a file changed since it was read is refused. Configuration would edit the keys no other screen saves through the same save: the edited lines shown first, then asked.

## Firewall

- **Granular ufw rules.** The Firewall screen opens other ports only through Application Profiles, or a whole interface through Trusted Interfaces, and Enable's review keeps a port open with a rule for everyone (`ufw allow 8000/tcp`). A Rules item would add and remove rules limited by source, port and protocol, such as `ufw allow proto tcp from <caddy-address> to any port 8000` for an app only a reverse proxy should reach. Each would be shown first and asked, like any change, and Enable's review could offer one for each port it lists. hardn already reads such rules (`Restricted`) and leaves them be. This revisits the decision that other ports open from Application Profiles ([gallery-firewall.md](engine/gallery-firewall.md) §5). On Rocky, Alma and RHEL it would take firewalld's rich rules, which hardn doesn't read yet.

## Backup

- **Restore a backup run.** Backup (7) lists past runs and their files, and only reads. A rollback already puts back the files of the change it undoes. Restoring a run you choose would go through the same steps as any change: shown first, asked, and confirm-or-revert where it restores SSH or firewall files.

## Audit log

- **Record confirmations and rollbacks.** `/var/log/hardn/audit.jsonl` doesn't record `hardn confirm`'s confirmation itself, `hardn rollback <id>` (the revert timer's included), `--abandon` or `--boot`.

## Checks

Both are off by default. hardn makes no network request today, and the README promises it never phones home: each would be a request only where you turn it on in hardn.yml.

- **Check for new hardn releases.** hardn would read GitHub's latest release (`api.github.com/repos/abbott/hardn/releases/latest`, as `install.sh` does) at most once a day, and say so in one line on the main menu, never in its way. Updating would stay `install.sh`'s, which checks a release against `SHA256SUMS`. An earlier checker is kept, disabled, in `pkg/version/checker.go`: it checked at every start, and cached in `/tmp`, where another user could plant a fake update. Proposal P-15 sets the fix: a cache in a directory root owns, checked before it's read.
- **Check for new applicable CVEs.** hardn knows one CVE today: Alpine's sshd password bypass (CVE-2026-123158). It reads openssh-server's installed version against each Alpine branch's first fixed release, warns on the main menu, raises the risk level to Critical, and offers Upgrade openssh. This check would read every installed package (apk, dpkg, rpm) against vulnerability data and list the CVEs an available update fixes, with the update that fixes them. OSV.dev's format covers Debian, Ubuntu, Alpine, Rocky Linux, AlmaLinux and Red Hat, keyed by source package, so installed packages would be mapped to their source first. Proxmox VE's own packages and Arch aren't covered there.

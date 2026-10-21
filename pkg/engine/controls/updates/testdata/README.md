# Auto Updates check fixtures (synthetic)

Each directory holds one platform's `apt-config dump APT::Periodic
Unattended-Upgrade::Origins-Pattern Unattended-Upgrade::Allowed-Origins`
with unattended-upgrades installed and on, in apt-config's default format
(`%F "%v";`, one item a line, each root printed before its items, a list's
items named after it with `::`; apt 3.0.3 `cmdline/apt-config.cc`,
`apt-pkg/contrib/configuration.cc`). The values are written from the
packages' own files and are not captured from a host:

- `debian13`: Debian 13 after `apt install unattended-upgrades` with
  debconf's default answer: the postinst's `20auto-upgrades`
  (`Update-Package-Lists "1"`, `Unattended-Upgrade "1"`) and the
  `Origins-Pattern` of `50unattended-upgrades` (Debian's own origins);
- `ubuntu2604`: Ubuntu 26.04 as installed: `10periodic`'s three items,
  then `20auto-upgrades`, and `50unattended-upgrades`' `Allowed-Origins`;
- `pve9`: Proxmox VE 9 with unattended-upgrades installed as on Debian,
  so no origin names Proxmox (a stock Proxmox VE 9 has no
  unattended-upgrades, which the tests model with an empty dump).

The exact text of each release's files is **[unverified]**; what the tests
rely on is the format and the items. The tests answer `dpkg-query`,
`systemctl is-active` and `is-enabled` for apt's two timers, `apk info -e`
and `rc-service crond status` as each case says, with Alpine's files in
`hostfake`. Synthetic values only.

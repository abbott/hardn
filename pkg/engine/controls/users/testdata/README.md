# User check fixtures (synthetic)

Each directory holds one platform's stock account and elevation files,
written from the vendor's layout as docs/safety/sudoers.md §3 and
docs/safety/detect.md describe it, not captured from a host:

- `debian13`: `/etc/passwd` and `/etc/group` with the system accounts a
  minimal install has and no user account; `/etc/sudoers` with
  `%sudo ALL=(ALL:ALL) ALL` before `@includedir /etc/sudoers.d`, and a
  comment-only `README` there;
- `ubuntu2604`: the same accounts; `/etc/sudoers` with `%admin` too
  (sudo-rs reads it, no `/etc/sudoers-rs`);
- `alpine324`: Alpine's accounts (root's shell `/bin/sh`), `wheel`
  listing root; `/etc/doas.conf` with its rule commented out and
  `/etc/doas.d/20-wheel.conf` (`permit persist :wheel`), as
  `setup-alpine` leaves them;
- `pve9`: Debian 13's accounts, root only, and no sudo.

The tests load them into `hostfake` with the accounts and rules each case
adds. Synthetic values only: alice, bob, george.

# Proxmox VE 9 fixtures (synthetic)

Shaped like a two-node Proxmox VE 9 cluster (nodes `web-01` and `web-02`,
`192.0.2.0/24`), not captured from a host. `root/` is loaded into the fake
host as it is, plus the links Proxmox VE keeps (`/root/.ssh/authorized_keys`
to `/etc/pve/priv/authorized_keys`, pmxcfs's `/etc/pve/local` to
`nodes/web-01`), which the tests add.

- `etc/ssh/sshd_config`: Debian 13's stock layout (the Include first), with
  the `PermitRootLogin yes` Proxmox VE's installer and `pvecm` write.
- `etc/ssh/sshd_config.d/00-hardn.conf`: what `pve.PlanSSH` makes of
  `AllowUsers alice` and `PasswordAuthentication no`.
- `sshd-T.out`: `sshd -T` for those files in OpenSSH 10.0p1's format (Debian
  13 and Proxmox VE 9; it prints `without-password`), after
  `servconf.c`'s `dump_config`; `pubkeyacceptedalgorithms` is 10.0p1's
  default list (`myproposal.h`, `KEX_DEFAULT_PK_ALG`).
- `etc/pve/priv/authorized_keys`: the cluster-wide keys file, with each
  node's root key (`ssh-rsa`, comment `root@<node>`); the keys are made-up
  bytes, not keys.
- `etc/pve/firewall/cluster.fw`, `etc/pve/nodes/web-01/host.fw`: the
  firewall enabled, `policy_in` DROP, a `management` IP set.
- `etc/pve/nodes/web-02/host.fw`: the other node's host firewall, off. It
  makes `nodes/web-02` a directory git keeps (`pve.ReadKeys` lists the
  nodes from `/etc/pve/nodes`), and a read of the wrong node's `host.fw`
  fails the firewall tests.

# SSH check fixtures (synthetic)

Each directory holds one platform's stock `/etc/ssh/sshd_config`, written
from the vendor's layout, not captured from a host:

- `debian13`: Debian 13 (OpenSSH 10.0p1, `ssh.service`);
- `ubuntu2604`: Ubuntu 26.04 (OpenSSH 10.2p1, `ssh.socket` from the
  generator);
- `alpine324`: Alpine 3.24 (OpenSSH 10.3_p1, OpenRC's `sshd`, built without
  PAM);
- `pve9`: Proxmox VE 9 (Debian 13's file with Proxmox VE's
  `PermitRootLogin yes`).

The tests load the file into `hostfake`, with the drop-ins each case adds,
and answer `sshd -T` from a model of sshd's reading of those files
(`sshdT` in `ssh_test.go`: the first value of each keyword outside a
`Match` block, every value of an accumulating one, OpenSSH's defaults for
the rest; before 10.1 `prohibit-password` prints as `without-password`;
Alpine's `/usr/sbin/sshd`, built without PAM, prints no `usepam` line, and
where a case installs `openssh-server-pam`, its `sshd.pam` answers `-G` and
`-T` as a build with PAM),
`systemctl show -p Listen ssh.socket` and `ss -Htln` from the model's
listeners unless a case says otherwise. Synthetic values only: alice,
web-01.example.net, 192.0.2.0/24, port 2230.

# Platform notes

What hardn does differently on each distribution. The support table is in the [README](../README.md#supported-distributions).

## Alpine

- On Alpine, `ufw` and `sudo` come from the community repository. On Alpine 3.22 and 3.23, the vendor supports only the main repository.
- On Alpine, Auto Updates restarts no service. An upgraded service keeps running its old version until it restarts, unless its package restarts it.

## Rocky, Alma and RHEL

On Rocky, Alma and RHEL:

- A new SSH port is labelled for sshd in SELinux and let through firewalld before sshd moves.
- The Firewall screen installs, enables and disables firewalld. It refuses a dnf install that brings a new kernel.
- Run All and `-w` leave the firewall to that screen. ufw is never installed or enabled.
- Auto Updates turns on dnf-automatic's daily security updates; restarts stay as automatic.conf sets them (never, as shipped). Off undoes only what hardn did.
- DNS is only read.
- RHEL's repositories come with its subscription: on a host with none enabled, such as one that isn't registered, hardn refuses to install firewalld or dnf-automatic.

## Proxmox

On a Proxmox node:

- hardn never turns root's SSH login off, because the nodes log in to each other as root by key: `-d` and Run All make it key-only instead. Where it is already off, they leave it off; SSH Login's Root key-only item turns it back to key-only.
- SSH stays on port 22.
- Root's keys are edited through the cluster's shared file, and a node's own key is never removed.
- Root's password is never locked.
- The firewall is only read (pve-firewall). `ufw` is never touched, and `-w` is refused.
- Auto Updates only shows the state.

The default cluster join (`pvecm add`, or the web UI) logs in to the other node's API with root's password, and needs no SSH password login. Two steps log in over SSH as root instead. hardn does neither, so do them by hand:

- **`pvecm add <node> --use_ssh`** joins over SSH to the existing node. Where hardn made root key-only on that node (`-d`, Run All or SSH Login), root logs in there by key only. Add the joining node's root key (`/root/.ssh/id_rsa.pub`) to root's keys there first.
- **`pvecm qdevice setup <address>`** logs in to the QDevice host as root over SSH, using root's password where the node's key isn't there yet. If hardn turned root login off on that host (`-d`, or Run All), allow it again there by hand for the setup.


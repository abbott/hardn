# hardn (unreleased)

## New

- **SSH Login lists every SSH key sshd reads, by account (item 6, SSH keys).** It marks a weak key (DSA, or RSA under 2048 bits), a key more than one account holds, and a key this sshd refuses. A key file it can't read, and lines that are no key, are shown as such. It changes nothing.

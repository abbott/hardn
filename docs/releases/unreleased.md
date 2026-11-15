# hardn (next release)

> **Draft: every line is proposed until the maintainer approves the words.** At the release candidate this file becomes `docs/releases/v<x.y.z>.md`, the release's whole body, and this note is cut.

## Changed

- **The main menu has one SSH row.** It was two, SSH Login and SSH Port. With root login off, passwords off and the port moved it reads "+ key-only, no root, port 2230".
- **A host still on port 22 reads "Partial default port (22)"** once root login and passwords are off. It used to show a Configured SSH Login row and a Not Configured SSH Port row. The risk level is unchanged.
- **Where root login or passwords are not set, the row says that**, as before ("Not Configured password auth on"), with "~ Default port (22)" under it where the port is 22.
- **Main menu 2 is "SSH"**, and so is its screen's title. It was "SSH Login". The screen shows the same one row; its items are unchanged.
- **`hardn check`, the JSON and the report are unchanged**: `ssh.login`, `ssh.auth` and `ssh.port` each keep their row, state and codes.
- **Words that name the screen follow it**: "Select option: 2) SSH", Run All's "SSH's: 2230", "upgrade openssh in SSH (5)".

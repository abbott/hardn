# hardn (next release)

> **Draft: every line is proposed until the maintainer approves the words.** At the release candidate this file becomes `docs/releases/v<x.y.z>.md`, the release's whole body, and this note is cut.

## Added

- **hardn asks whether to check for new releases.** Before the main menu, on a terminal, as root: "Check for new hardn releases, once a day? [y/N]:". Enter is no, and hardn still makes no network request unless you say yes or set `checkReleases: true`.
- **A hardn.yml without `checkReleases` gets the key.** hardn asks once and writes your answer to the file, yes or no; the rest of the file and its comments stay.
- **With no hardn.yml, a yes is kept** in `/var/lib/hardn/release-ask.json` and turns the check on until a hardn.yml exists. After a no, hardn asks again after 30 days or 15 opens of the main menu, whichever comes first.

## Changed

- **Creating hardn.yml asks a fifth question**, the release check, and the file written holds `checkReleases` either way.

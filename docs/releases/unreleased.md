# hardn (unreleased)

## New

- **Firewall shows each listening port against the rules**: open, let in from some sources, blocked, or local only, with the program. A port the rules let in with nothing listening is flagged. It changes nothing.
- **System Details lists the listening ports**, and `hardn system-details` prints them. Run it as root to see every program. `--output json` doesn't include them.

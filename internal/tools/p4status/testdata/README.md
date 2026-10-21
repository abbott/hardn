# p4status's test hosts

`hosts/<name>.json` are five of the gallery's status hosts (`internal/tools/uigallery`, `statusHosts`), as its model makes them ([gallery-status.md](../../../../docs/engine/gallery-status.md) §2.1, §2.3): `debian13`, `ubuntu2604`, `alpine324`, `pve9` and `hardened`. Synthetic data only: `web-01`, `192.0.2.20`, `alice`.

Each file holds what one reading of the host by the real code (detect, `Engine.Check` over the status rows, the header probe) used of it, and nothing else:

- `commands`: every command the reading ran, in order, with the model's answer (`stdout`, `stderr`, `exit`; `err` is `notfound` for a command not installed);
- `entries`: every file system entry the reading touched, the entries of each directory it listed, and the directories and symlink targets on their way, with their modes and owners;
- `setuid`: the files whose `Stat` shows the setuid bit (hostfake keeps none; the gallery's `statusSys` adds it on the elevation tools);
- `users`, `groups`: the accounts of `/etc/passwd` and `/etc/group` that `LookupUser` and `LookupGroup` find.

`TestMainMenuAsGallery` holds them to the gallery: p4status's main menu over each file must be, line for line, the main menu in the plain-80 cell of `internal/tools/uigallery/testdata/golden/status/<name>.golden`. When the gallery's model of these hosts or a check changes, that test (or `TestRunOverHosts`, for a command the file does not answer) fails: write the files again from the gallery with `export/export_test.go`, a test of the gallery's package `main` that only `-overlay` builds, so that neither tool imports the other. From the repository's root:

```sh
d=$(mktemp -d)
printf '{"Replace":{"%s/internal/tools/uigallery/zz_export_test.go":"%s/internal/tools/p4status/testdata/export/export_test.go"}}' "$PWD" "$PWD" > "$d/overlay.json"
EXPORT_HOSTS=debian13,ubuntu2604,alpine324,pve9,hardened GOTOOLCHAIN=local GOWORK=off \
  mise exec -- go test -count=1 -overlay "$d/overlay.json" -run TestExportStatusHosts ./internal/tools/uigallery \
  -args -export-dir "$PWD/internal/tools/p4status/testdata/hosts"
```

Review the diff, as for a golden file.

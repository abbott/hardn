# Editing hardn.yml in place: the save every slice uses (`config.Edit`)

> **Unit:** refactor plan slice 2 (DNS, the pilot of the full flow), `next/s2-config-edit`, on `next/s2-engine-gallery`. **The maintainer's decision of 2026-10-22, amending plan §3.19 for every slice:** "Change the hardn.yml save to edit only the keys that changed, keeping the rest of the file and its comments, for every slice; drop the comments-lost warning." §3.19 (approved 2026-10-17: save to the loaded path, one atomic save function, warn that comments are lost) keeps its first two points; the third is gone. Builds on [control-dns.md](control-dns.md) §2.9 (the save as a change kind, `steps.PlanSave`, `next/s2-control`) and slice 2's decision 1 ([gallery-dns.md](gallery-dns.md) §4), which the maintainer's amendment is recorded under. V-numbers: none new; side-effects row 29 (`./hardn.yml` saved whole) is the legacy path, untouched.
> **Status:** built and tested on macOS, in process, on the recording fake (`hostfake`) and on bytes. Nothing has run on Linux: the save's write on a host is "written, not run (gate 5)" (§9). Nothing is wired into `cmd/hardn` or the live main menu; the legacy `SaveConfig` and its `./hardn.yml` path are untouched. **Every word new here is proposed** (§5).

## 1. Why

Until this unit `config.Save` wrote `config.Marshal` of the whole configuration: a five-line `hardn.yml` became about fifty keys, every default pinned (`sudoNoPassword: true`, `permitRootLogin: "no"`, `enableBackups: true`, ...), its comments dropped, and the plan warned "Saving hardn.yml loses its comments" (`CONFIG_COMMENTS_LOST`; the gallery's `dns-cloudflare-debian13` showed it). A default written into the file is no longer a default: a later release that changes it would not reach that host. The maintainer chose the edit.

## 2. Guarantees

### The edit (`config.Edit(original []byte, to *Config) ([]byte, error)`)

1. **The settings that changed, alone.** `from` is `original` decoded as the loaders decode it (the defaults, then the file: `DefaultConfig` and `yaml.Unmarshal`, as `config.Read` and `LoadConfig` do). Each setting of `to` is compared with `from`'s by its canonical encoding (`permitRootLogin` as its string, as `canonical` gives it). A setting whose value differs is changed in `original`'s bytes, at its value; **every other byte stays as it was**: key order, blank lines, indentation, quoting, unknown keys, the comments of the file's head, above and beside each key, inside a list and after it. `TestEdit` pins each case's exact output bytes; `TestEditExample` edits `hardn.yml.example`, commented on nearly every line, and checks the output is the input but for the nameservers' items (one added, one removed, one kept with its line), `sshPort`'s value, and `confirmTimeout` added after the last setting.
2. **A default is never written.** A setting the file does not have differs only when `to`'s value is not its default (what the file, as loaded, gives it); then it is added at the end of its mapping, after the mapping's last value and before the comments that follow it, at its keys' column ("a default is not written", "a key added at the end of the mapping, before the comments after it").
3. **Each value in its own form.** A scalar is replaced where it is written, in its own quotes when it is a string (`"alice"` → `"bob"`, `'alice'` → `'bob'`), otherwise as yaml.v3 writes it (plain, or quoted where it must be: `""`), always on one line (a string with a line break, double-quoted); a tag before it goes with it. A list in brackets stays in brackets, on one line, its items quoted as its first was. **A block list is edited item by item**: the items the new list keeps keep their lines, their line comments and the comment lines above them; the new items take the list's dash column, the spaces after it and its first item's quotes; a removed or replaced item's lines go, its comments with it (the longest run of items kept, in order: `script`). Comment lines after a list's last item (a commented-out entry) stay where they are. A list emptied becomes `[]` on its key's line, the item lines going and the comments after it staying. A list the file has as nothing, `~` or `[]` takes the file's list style: the geometry and quoting of its first block list, or brackets when its only lists are in brackets, or two spaces, a dash and one space (`hardn.yml.example`'s). A mapping is edited at the key that changed, and a key it lacks added at its end (`TestEditNested`). A list's new items that are mappings (`ufwAppProfiles`) are written with every field, as the struct encodes them.
4. **The text's own forms kept.** CRLF line ends (new lines take the file's first line break), a UTF-8 byte order mark, a leading `---` and a `%YAML` directive, a key quoted, a tab after a colon, characters before a value (columns are counted in characters, as yaml.v3 counts them), a value on the line after its key, a plain or quoted value over several lines, a block scalar (its header's comment kept), a last line with no line break (added keys keep it so), a mapping in braces (a key added after its last value, or inside `{}`), a mapping indented as a whole, a file of comments alone, an empty file or a lone `---` (the settings appended at the end, a line break added before them when the file has none). Each is a case of `TestEdit`.
5. **Never corrupt the file: refused, writing nothing** (`EditError`, §5), each case tested:
   - before editing, a file in a form the edit does not take apart (`ErrNotEditable`): more than one YAML document; a document end (`...`); a root that is not a mapping (`~`); a merge (`<<`) into a mapping it edits; a setting to change with an anchor or an alias anywhere in it (its key or value: an edit through an alias would change the other key too; an alias or anchor on an unchanged key is kept); a comment inside a changed list in brackets (collapsed onto one line it would be lost); an explicit key (`? key`) or a key off its mapping's column; line breaks other than LF and CRLF (a lone CR, NEL, LS or PS, which yaml.v3 counts as line breaks and the editor would not); not UTF-8 (a UTF-16 file, which yaml.v3 reads; other bytes that are not UTF-8 yaml.v3 refuses first, `ErrInvalidFile`);
   - **after editing, the edited bytes read back** (`check`, `ErrEditUnsafe`): one document; decoded as the loaders decode them, exactly `to`, every setting compared; and every key of the original but the settings changed, unknown keys and `logsconfig` included, decoded (into `any`) as before, with no key added but those (`TestEditChecks`: a setting not changed, one changed though not to save, an unknown key changed or removed, `logsconfig` changed, a key added, bytes that do not parse, two documents). One edit is refused so (`TestEdit`): a key added after a block scalar that ends the file with no line break, which would give that scalar a line break it did not have;
   - bytes that do not decode at all (a key given twice, a tab as indentation): `ErrInvalidFile` (`CONFIG_FILE_INVALID`), as `config.Read` refuses them.
6. **A property** (`TestEditKeepsTheRest`, 300 seeded runs over a file commented throughout, random changes to the user name, the port, a quoted string, a bool, two block lists and an added key): the output decodes to the configuration saved, every line outside the settings changed is kept in order, and each changed scalar keeps its comment. **A fuzz test** (`FuzzEdit`: eight changes over any bytes that decode; its seeds run with the tests): a 60-second run of it on this machine, and a 90-second run of an earlier copy, about 1.9 million inputs in all, found no panic and no saved bytes that read back otherwise; their corpus showed the one refusal of the read-back noted above, and UTF-16 files.
7. **`logsconfig` is not a setting** (`derivedKey`): `LoadConfig` sets `LogsConfig.LogFilePath` from `logFile` whatever the file says, and nothing reads it from the file. The edit neither compares nor writes it: a file's `logsconfig` stays as it is (legacy saves wrote one) and none is added for the value `LoadConfig` derived ("logsconfig as LoadConfig sets it, not added"). The read-back requires it to decode as before.
8. **A deprecated value is rewritten only when its value changes** (edit only what changed): `permitRootLogin: false` stays until the value changes, then becomes `"yes"` or `"no"` (§3.17's canonical string), its comment kept.

### The save (`config.Save`) and its plan (`steps.PlanSave`)

9. **`config.Save` is built on `Edit`**: to the loaded path (`ErrNotLoaded` without one), the file read not through a symlink (`host.ErrSymlink`), refused when its bytes are not the ones the configuration was read from or last saved (`ErrChanged`, `CONFIG_CHANGED`: the edit compares with the file as read, so it would undo an edit made since), `Edit`'s bytes written atomically (`host.WriteFileAtomicKeep`, mode and owner kept), and nothing written when nothing changed (`TestSave`, `TestRead`).
10. **`steps.PlanSave` plans `Edit`'s bytes**: none when the file already gives the configuration's settings (it returns `[]engine.Change`, empty then); otherwise one change, "Save the configuration", **its diff the edited lines alone**, and **no finding** (`CONFIG_COMMENTS_LOST` removed). An edit `Edit` refuses is a refusal before anything changes, with a report (§5). Its other refusals (`CONFIG_NOT_LOADED`, `SYMLINK_REFUSED`, `CONFIG_CHANGED`), its Apply's checks (the backup as the plan read it; the configuration still giving the bytes shown, else `PLAN_CHANGED`) and its failure ("Saving <path> failed", the changes before it rolled back) are `next/s2-control`'s, unchanged (`TestPlanSave`).
11. **The DNS control's save** (`dns.nameservers/save`) now shows the list's lines alone: Debian 13's preset on the scenarios' file (`# hardn's configuration (web-01.example.net)`, `username: alice`, `nameservers:` with two items) has the diff `@@ -1,5 +1,5 @@`, the two items replaced, the comment kept; the file is saved byte for byte so (`TestApply`, `TestPlayEngineDNS`).

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| yaml.v3 (`go.yaml.in/yaml/v3`, already in `go.mod`) gives each node its `Line` and `Column`, 1-based, the column in characters, after a byte order mark; no end position | **observed** by probes of this unit (`go test -overlay` in `pkg/config`): `"ключ": x`, a tab after the colon, a BOM | the editor finds each value from its start and scans to its end (`pos`, `value`) |
| A null written as nothing is at the column after its key's colon, even with a comment or a next line after it; a block list's node is at its first dash, its items after their dash; a tagged or anchored value at its tag or anchor; a block scalar at its indicator; an explicit key's key and value at their own columns | **observed** (same probes) | `value`, `items`, the refusal of explicit keys |
| yaml.v3 refuses a key given twice and a tab as indentation, and decodes only the first of several documents | **observed** (probes; `TestEdit`) | `ErrInvalidFile`; the documents counted, several refused |
| yaml.v3 counts a lone CR, NEL, LS and PS as line breaks | **observed** for CR and LS (`TestEdit` decodes those files before refusing them) | refused (§2.5) |
| yaml.v3's encoder writes a string on one line unless it holds a line break (a 264-character string unfolded), quotes it as asked (`'it''s'`), and a list or mapping in brackets on one line (30 items) | **observed** (probes) | `inline` |
| The canonical encoding of equal values is the same bytes | yaml.v3's encoder is deterministic over the same types and styles | `same`: what changed |

## 4. What happens to each form

| Form | Handled or refused | `TestEdit` case |
|---|---|---|
| comments above, beside, after; blank lines | kept | "a scalar, the comments beside and above kept" and the property |
| block list | edited item by item; kept items keep their comments | "a block list: …" (four cases) |
| list in brackets | stays in brackets, one line; a comment inside: refused | "a list in brackets …", "a comment inside brackets" |
| mapping in braces, block mapping | edited at the key; a key added at its end | "a mapping in braces", "an empty mapping in braces"; `TestEditNested` |
| quoted and plain scalars | in their own quotes; plain where yaml.v3 can | "double quotes kept", "single quotes kept", "plain to a value that needs quotes" |
| a key given twice | `CONFIG_FILE_INVALID` | "a key given twice" |
| anchors and aliases | on the setting changed: refused; elsewhere: kept | "an alias on the key changed", "an anchor on the key changed", "an alias on an unchanged key kept" |
| CRLF; a BOM | kept | "CRLF line ends", "a byte order mark" |
| comments only; empty | settings appended | "comments alone", "an empty file" |
| a leading `---`, a directive | kept (yamllint's document-start style) | "a leading ---", "a lone ---", "a directive" |
| several documents; `...` | refused | "several documents", "a document end" |
| a tab where YAML forbids it | `CONFIG_FILE_INVALID`; a tab after a colon kept | "a tab as indentation", "a key quoted, a tab after the colon" |
| the last key with no line break | kept so | "the last key with no line break" (three cases) |

## 5. Words and codes

All **proposed**. Each refusal is a `steps.Refusal`; `pkg/output` shows the Why with the code, then "To go on: " and the Fix. The Why names the file (and the setting, when one is at issue), the Fix the settings the save changes.

| Code | When | Why | Fix |
|---|---|---|---|
| `CONFIG_NOT_EDITABLE` | several documents | `<path>` holds more than one YAML document | set `<settings>` in `<path>` by hand, then try again |
| `CONFIG_NOT_EDITABLE` | a document end | `<path>` ends its YAML document with ... | the same |
| `CONFIG_NOT_EDITABLE` | the root not a mapping | `<path>` does not hold its settings as key: value | the same |
| `CONFIG_NOT_EDITABLE` | a merge | `<path>` merges settings with << | the same |
| `CONFIG_NOT_EDITABLE` | an anchor or alias | `<setting> in <path>` uses a YAML anchor or alias | the same |
| `CONFIG_NOT_EDITABLE` | a comment in brackets | `<setting> in <path>` has comments inside its brackets | the same |
| `CONFIG_NOT_EDITABLE` | an explicit key, a key off its column, a value not found | `<setting> in <path>` is written in an unusual YAML form | the same |
| `CONFIG_NOT_EDITABLE` | line breaks | `<path>` has line breaks other than LF and CRLF | the same |
| `CONFIG_NOT_EDITABLE` | not UTF-8 | `<path>` is not UTF-8 text | the same |
| `CONFIG_EDIT_UNSAFE` | the read-back fails | `<path>` cannot be edited safely (`<setting> in <path>` when one setting differs) | the same |

For DNS: "/etc/hardn/hardn.yml holds more than one YAML document (CONFIG_NOT_EDITABLE)", "To go on: set nameservers in /etc/hardn/hardn.yml by hand, then try again." Once set by hand, "Configure DNS" applies the list with no save.

**Codes** (`pkg/output`'s registry): `CONFIG_NOT_EDITABLE` (`config.ErrNotEditable`) and `CONFIG_EDIT_UNSAFE` (`config.ErrEditUnsafe`) added; `config.ErrChanged`, `config.Save`'s, joins `CONFIG_CHANGED`, `steps.ErrConfigChanged`'s code (no code renamed). **`CONFIG_COMMENTS_LOST` removed**, with `steps.CodeCommentsLost`: it was added by `next/s2-control` and never merged or released, so no released code goes (`docs/reference/output.md` §2.2 says so); the rule that a code never goes once released stands. The save's other words are unchanged: "Save the configuration", "Save the configuration to <path>", "Saving <path> failed", "saved <path>".

## 6. Decisions of this unit

1. **`Edit(original, to)` decodes `from` itself** (the task proposed `Edit(original, from, to)`): `from` is by definition what the bytes decode to, so no caller can pass one that disagrees.
2. **`logsconfig` is left out** (§2.7): the task named it as a nested key to edit at its leaf, but it is derived, not read; comparing it would write the value `LoadConfig` derived into every file loaded through it. Nested mappings are edited at the leaf all the same (`TestEditNested`, on trees given directly); no setting of `Config` is a mapping.
3. **A replaced list item is a removed one and a new one**: its comments go, since they spoke of the old value; the comment lines above an item belong to it; those after the last item stay. *Alternative:* keep a replaced item's line comment on the new value.
4. **The file's list style for a list it has as nothing or `[]`** (§2.3): legacy saves write `sshKeys: []` beside block lists, so `[]`'s own brackets say nothing of the file's style. A non-empty list in brackets stays in brackets.
5. **A single leading `---` and a directive are kept, not refused** (the task listed "a document separator or several documents (refuse)"): yamllint's document-start rule asks for `---`, the positions and the read-back work the same, and refusing would leave such a file unsaveable; several documents and `...` are refused. Say if a lone `---` should be refused too.
6. **A comment inside a changed list in brackets is refused, not dropped**: the list would be collapsed onto one line.
7. **The read-back covers every key of the file**, unknown keys and `logsconfig` included, not only `Config`'s.
8. **A deprecated value is not rewritten by an unrelated save** (§2.8): the bool stays until its value changes, and §3.17's warning for it, owed by the slices, stays owed ([sshd-migrate.md](../safety/sshd-migrate.md) §22).
9. **`PlanSave` returns `[]engine.Change`**, none when nothing changed, rather than a change with an empty diff that would say "Save the configuration" of a save that changes nothing. Its one caller, the DNS control, appends it.
10. **`config.Save` reads the file and checks `LoadedFrom` itself**, where it wrote without reading: it needs the bytes to edit, and an edit over another one would undo it.
11. **Removed:** `config.Marshal` (what the old save wrote; nothing else used it), `config.HasComments` and `steps.CodeCommentsLost` (the warning), all added by `next/s2-control`, unreleased. `MarshalYAML` and the legacy `SaveConfig` are unchanged.

## 7. API

```go
package config // pkg/config: edit.go and editor.go new
var ErrNotEditable, ErrEditUnsafe, ErrChanged error
type EditError struct { Key, Why string; Keys []string; Err error } // Unwrap: Err
func Edit(original []byte, to *Config) ([]byte, error)
// Save: reads the file (no symlink), ErrChanged for other bytes, writes Edit's bytes, nothing when nothing changed
// removed: func Marshal(c *Config) ([]byte, error); func HasComments(data []byte) bool

package steps // pkg/engine/steps
func PlanSave(ctx context.Context, sys host.System, id string, cfg *config.Config, after ...string) ([]engine.Change, error) // was (engine.Change, error)
// removed: const CodeCommentsLost

package output // codes: CONFIG_NOT_EDITABLE, CONFIG_EDIT_UNSAFE added; config.ErrChanged under CONFIG_CHANGED; CONFIG_COMMENTS_LOST removed
```

`pkg/config` reaches the host only through the `host.System` it is given (`Read`, `Save`); `Edit` reaches nothing. No depguard rule changed (no new package). `TestPlayTouchesNothing` allows `Edit` beside `Read` and `Save`.

## 8. Goldens changed, and why

- `pkg/output/testdata/codes.golden`: `CONFIG_NOT_EDITABLE` and `CONFIG_EDIT_UNSAFE` added after `CONFIG_FILE_INVALID`; `CONFIG_COMMENTS_LOST` removed. `sentinels.golden`: `config.ErrChanged`, `ErrEditUnsafe`, `ErrNotEditable` added.
- `pkg/engine/controls/dns/testdata/golden/human/plan-debian13-cloudflare.golden` and `json/plan-debian13-cloudflare.json`: the save's diff the list's lines alone (`@@ -1,5 +1,5 @@`, the comment kept), no comments warning. `human/applied-debian13-cloudflare.golden`, `json/applied-debian13-cloudflare.json`, `human/failed-save.golden`, `json/failed-save.json`: the same diff in the JSON, the warning gone (after the failed save it warned of a save that never happened).
- `internal/tools/uigallery/testdata/golden/play/engine/dns-cloudflare-debian13.golden`, `dns-dry-run-debian13.golden`, `dns-failed-save-debian13.golden`: the save's diff five lines where it was fifty-one, the warning gone. `play/dns.golden`: the mock's review line "Saving hardn.yml loses its comments" gone.
- `pkg/ui/screen/testdata/golden/workflow-step/`: `dns-add-nameserver/normal/4-confirm`, `dns-remove-nameserver/normal/2-confirm`, `dns-use-cloudflare-dns/{normal/1-confirm,cancelled/1-confirm,dry-run/1-planned}`, `dns-use-google-dns/normal/1-confirm`, `dns-use-quad9-dns/normal/1-confirm`: the review's warning line gone; the Save change keeps its title and path.

No other golden changed: no legacy or characterization golden, and no golden of another control.

## 9. Written, not run (gate 5)

`config.Save`'s write on a host is `next/s2-control`'s (`host.WriteFileAtomicKeep`), unchanged; what it writes is now `Edit`'s bytes, tested on bytes only. **On the VM** (control-dns.md §4): a preset on Debian 13 with `/etc/hardn/hardn.yml` commented throughout: the file saved at its loaded path, mode and owner kept, the list's lines alone changed, every comment kept; `hardn.yml.example` copied in as `hardn.yml` and a preset applied; a file with two documents refused, nothing changed.

## 10. Not in this unit; inputs to later units

- **Unit (c), the screen:** the save's diff is a few lines now, so the question of folding it (control-dns.md §11) is gone; the review shows no comments line (the mock's, gone).
- **Every slice that saves hardn.yml:** `steps.PlanSave` after the change, none planned when nothing differs; a struct list's new items are written with every field; a refusal of the edit names the settings to set by hand.
- **The deprecated `permitRootLogin` bool's warning** (§3.17) is still owed; the save no longer rewrites the bool unless its value changes.
- **The legacy `SaveConfig`** (`./hardn.yml`, the whole file, 15 call sites) is untouched; replacing it is the slices'.

## 11. Size

About 1,100 lines of non-test Go added in `pkg/config` (`edit.go` 272, `editor.go` 827; 159 of them whole-line comments), 98 added elsewhere (`steps/save.go`, `config.Save`, `pkg/output`, the DNS control and mock) and 112 removed (`Marshal`, `HasComments`, the warning): 1,197 added and 112 removed in all (`git diff --numstat next/s2-engine-gallery`). Tests: 514 lines added and 79 removed (`edit_test.go` 401: 71 table cases, the example, the nested edit, the read-back, the property and the fuzz test). Testdata: 56 lines added and 697 removed (the save's fifty-line diffs). Over the guideline of about 500 changed lines because yaml.v3 gives each value's start and not its end: the editor scans every form the task lists to its end (plain, quoted and block scalars, lists and mappings in block and flow style, nothing, CRLF, a byte order mark, documents), edits block lists item by item to keep their comments, writes added values in the file's own style, and reads the result back; the split would have been scalars and block lists first, then the other forms.

# Asking to turn the release check on: design

Status: **adopted 2026-11-15, built the same day** (§7; what is not verified: §8). The maintainer adopted §4's recommended answers ("adopt all"), with two changes: decision 7 is replaced (7 below is as adopted), and decision 8's words lose "GitHub". §4 is kept as the record.

## 1. What is there

- **The release check is off unless hardn.yml says `checkReleases: true`.** Off, hardn makes no network request. On, the main menu asks GitHub for the latest release's tag at most once a day and shows "v0.10.2 available" (`cmd/hardn/release.go`, `docs/engine/screen-release-check.md`).
- **Nothing ever asks.** A person learns of the key only from `docs/configuration.md` or the Configuration screen.
- **With no hardn.yml, hardn asks to create one** at every start on a terminal: "No configuration file found. Would you like to create one? [Y/n]:" (`pkg/config/config.go`, `ShouldCreateDefaultConfig`).
- **A yes asks four more questions** (`CreateDefaultConfig`): a default username, the SSH port, "Enable automatic security updates? [y/N]:", an SSH public key. Then hardn.yml is written. (Since `next/config-edit`, two more before the release check: an NTP server and a syslog server, `docs/engine/design-config-edit.md`.)
- **A no writes nothing.** hardn runs on the defaults, and asks again at the next start.

## 2. What is asked for

The maintainer, 2026-11-15:

1. Where the person creates hardn.yml, the release check is one more of those questions.
2. Where they decline, hardn asks about the release check next, and no more often than once per 30 days or 15 opens of the menu.

## 3. Mocks

Creating hardn.yml (the new question is the last):

```
No configuration file found. Would you like to create one? [Y/n]: y
Enter default username (optional, press Enter to skip):
Enter SSH port [22]:
Enable automatic security updates? [y/N]:
Add SSH public key (optional, press Enter to skip):
Check for new hardn releases, once a day? [y/N]: y
Created configuration file at /etc/hardn/hardn.yml
```

A hardn.yml without the key (decision 7), before the main menu:

```
Check for new hardn releases, once a day? [y/N]: y
Saved checkReleases: true to /etc/hardn/hardn.yml
```

Declining hardn.yml:

```
No configuration file found. Would you like to create one? [Y/n]: n
Check for new hardn releases, once a day? [y/N]:
```

## 4. Decisions for the maintainer

1. **Where a yes is kept with no hardn.yml.** `checkReleases` lives in hardn.yml, which the person has just declined. Gates 2 and 3.
   - (a) *Recommended:* in hardn's own state, `/var/lib/hardn/release-ask.json`, root's alone, beside the check's cache. It counts only while no hardn.yml is found; a hardn.yml, once there, decides alone, and the first-run questions carry the answer into it (decision 5).
   - (b) Write a hardn.yml holding that one key. The file they declined appears anyway, and "create one?" is never asked again.
2. **"Once per 30 days or 15 opens."** After a no, hardn asks again:
   - (a) *Recommended:* when 30 days have passed or the main menu has opened 15 times since, whichever comes first.
   - (b) Only when both have happened: at least 30 days and at least 15 opens.
3. **After a yes.** (a) *Recommended:* never asked again; turning it off is `checkReleases: false` in a hardn.yml, or deleting the state file, and the docs say so. (b) A menu item to turn it off.
4. **The default answer.** (a) *Recommended:* no, `[y/N]`: Enter makes no network request, as today. (b) Yes, `[Y/n]`.
5. **In the hardn.yml questions, where.** (a) *Recommended:* last, after the SSH key, with the same words. Where the state already holds a yes (decision 1 (a)), the question's default is yes. (b) After the automatic updates question.
6. **Who is asked.** (a) *Recommended:* only at the main menu, on a terminal, as root: never for a flag or a subcommand, and never where hardn can't keep the answer. (b) Every start on a terminal.
7. **A host that has a hardn.yml with `checkReleases` not set**, such as one installed before this. *Adopted, the maintainer's:* asked too, at least once, and the answer is written to hardn.yml, yes or no, so every hardn.yml comes to hold the key. In the maintainer's words: "we should at least add the key, and at most follow the once per 30 or 15 opens policy", and "hardn is a security tool, so we have a responsibility to ensure end-users have the latest version". Read here as: a key that is set, either way, is never asked about again; a question left unanswered (the end of the input, or a hardn.yml that can't be edited in place) is asked again on decision 2's 30 days or 15 opens. The save edits the key in place and keeps the rest of the file and its comments (`config.Edit`).
8. **The words.** *Adopted:* "Check for new hardn releases, once a day? [y/N]:".

## 5. Not in this unit

- "Create one?" is still asked at every start after a no. The same 30 days or 15 opens could apply to it; not asked for.
- The first-run questions are plain prompts, not a screen. This adds one in the same form.

## 6. Checks when built

- A no, then 14 opens: not asked. The 15th: asked. A clock 30 days on: asked.
- A yes with no hardn.yml: the menu checks and shows the notice; `hardn --version` says it as root.
- A hardn.yml created later with `checkReleases: false`: no request, whatever the state file says.
- No terminal, a flag, a subcommand, a user other than root: no question, nothing written.
- `hardn check` writes nothing.
- A hardn.yml without the key: asked once; yes writes `checkReleases: true`, no writes `checkReleases: false`, comments kept; not asked again either way.

## 7. Built

`next/release-ask`, 2026-11-15. The code: `cmd/hardn/releaseask.go` (when to ask, and what to do with the answer), `pkg/version/ask.go` (the state file and the 30 days or 15 opens), `pkg/config` (`ReleaseQuestion`, `AskCheckReleases`, the fifth hardn.yml question, `EditKeys` and `SaveKeys`).

Choices made where the page was silent:

- **`-n` asks nothing**: no question, no open counted, nothing written, as the first-run question is not asked under `-n`. A yes already in the state still turns the check on there, since a dry run changes nothing about the check.
- **hardn.yml's `dryRun: true` does not stop the question**, and the answer is written to that hardn.yml as in a live session. It is a lasting setting: skipped, that host would never be asked, against decision 7. The save is hardn's own key, not a change to the host's hardening; the Configuration screen's saves there stay plan only.
- **Where hardn's state can't be written, nothing is asked**, with or without a hardn.yml (decision 6, "never where hardn can't keep the answer"): a state directory that fails its check, or a read-only `/var`. Otherwise the question came at every open. `Due` writes the state before the question, and is not due where that fails. So a hardn.yml without the key on such a host is not asked until the directory is put right.
- **Once due, the state says so** (`"answer": "none"`, `"opens": 15`) before the question is asked, so Ctrl-C still asks at the next start. After an answer saved to hardn.yml that file stays as it is, and is never read again.
- **A flag run that creates hardn.yml asks the question** with the other four (`hardn -g` on a terminal, no hardn.yml, a yes to "create one?"). Decision 6's "never for a flag" is read as the question asked alone: here the person is answering hardn.yml's questions, and leaving this one out would write `checkReleases: false` unasked, after which a set key is never asked about.
- **Saving to hardn.yml goes straight through `config.SaveKeys`**, not through a plan: no backup, no journal entry, no confirm. It is the same in-place edit and the same checks as every save (the file not a link, not changed since it was read, read back before it is written), on one key.
- **`config.EditKeys` is new.** `config.Edit` never writes a default, so it could not write `checkReleases: false` to a file without the key. `EditKeys` is `Edit` that also writes a named key the file gives no value. `Edit` is unchanged.
- **A hardn.yml hardn creates always holds the key**: `checkReleases` lost its `omitempty`, so the file written without questions (no terminal on stdin) holds `checkReleases: false` too.
- **No answer is kept as its own answer** (`"answer": "none"`), with no hardn.yml too: the end of the input counts as asked, so the next start does not ask again. It turns nothing on.
- **A key left null** (`checkReleases:` with no value) counts as not set, and the answer is written at that line.
- **A yes in the state, then a hardn.yml without the key**: asked at once, with yes as the default (`[Y/n]`), since the file now decides.
- **A no in the state, then a hardn.yml without the key**: asked by the same count, not at once.
- **A clock that went backwards**: the 30 days count from now, and the opens count as ever, so the question comes within 15 opens whatever the clock does.
- **A state file that fails a check** (another owner, not valid JSON, an unknown field) counts as never asked, and the answer replaces it.
- **Ctrl-C at the question** ends hardn and records no answer: it is asked at the next start.
- **An answer other than y, yes or Enter is no**, as at "Enable automatic security updates?".
- **With a yes and no hardn.yml, the Configuration screen shows `checkReleases` as true** for the session: the yes sets the session's value.
- **The lines said:** "Saved checkReleases: true to /etc/hardn/hardn.yml"; "No answer: /etc/hardn/hardn.yml not changed"; "/etc/hardn/hardn.yml not changed: it merges settings with <<. Set checkReleases there by hand".

Tested (`cmd/hardn/releaseask_test.go`, `pkg/version/ask_test.go`, `pkg/config/releaseask_test.go`): every check of §6, and 14 opens, the 15th, 30 days, the clock set back, each answer with and without a hardn.yml, a file that can't be edited, a state that can't be written, no terminal, `-n`, hardn.yml's `dryRun: true`, a flag, `hardn check`, `--version`, a user other than root, and the five hardn.yml questions.

## 8. Not verified

- Nothing ran on Linux, and no hardn binary ran: the tests are in process on macOS, with a fake host, clock and prompt.
- A flag run that creates hardn.yml asking the fifth question: read from the code (`loadConfig`), no test runs it.
- The question on a real terminal, inside the output frame, after "create one?" is declined.
- The save on a real `/etc/hardn/hardn.yml` (its mode and owner kept).
- README.md's "No telemetry" line still says "unless you turn on `checkReleases`"; a yes with no hardn.yml also turns the check on. Not changed here.

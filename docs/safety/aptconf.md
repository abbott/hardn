# apt's settings: which file sets each value (`pkg/host/aptconf`)

> **Unit:** refactor plan slice 7, `next/s7-control`, unit (b) ([gallery-updates.md](../engine/gallery-updates.md) §7 item 6, Made here's off with no record).
> **Status:** built and tested on macOS against `hostfake` only. **Not run: nothing has been read on a VM through this package.** It only reads.

## 1. Why
apt-config has no per-file readout, and neither `-o Dir::Etc::Parts=/nonexistent` nor `APT_CONFIG=<file>` keeps `apt.conf.d` out of its read (`01-inspect-b-deb13-vm.log`, `01-inspect-b-ubuntu26-vm.log`, 2026-11-03). So hardn reads the files itself and checks its answer against apt's.

## 2. API (only through `host.System`: depguard `aptconf-through-host`)
```go
const PartsDir = "/etc/apt/apt.conf.d"; const MainFile = "/etc/apt/apt.conf"
type Item struct{ Value, File string }
func Read(ctx context.Context, sys host.System, roots ...string) (Config, error)
func (c Config) Value(name string) (value, file string, ok bool) // case-insensitive
func (c Config) Items(name string) []Item                        // list items, in order
func (c Config) Files() []string                                 // read, in apt's order
func (c Config) Without(file string) Config                      // the values with file left out
```
Errors: `detect.ErrUnrecognised` for anything below not read, or any disagreement; `fs.ErrInvalid` for no root or a bad root; a read or run failure as it is.

## 3. Guarantees
- **Never a guess.** After reading, `apt-config dump <roots>` is run, and its output must equal, byte for byte, the dump hardn renders from the files: every name, its spelling (the first writer's), value and order, list items included. Any difference is `ErrUnrecognised` ("hardn can't tell which file sets what"). A root apt gives defaults under (most of `APT`, `Dir`) therefore refuses.
- `Value` and `Items` answer only names under the roots checked; any other name is not set. A name set only as the parent of another (`APT::Periodic`) is not set.
- `Without` is worked out, not checked: apt cannot be asked what it would read without a file. It is what off with no record uses to see whether another file still turns updates on.
- Only reads: `apt-config dump`, `ReadDir`, `Stat`, `ReadFile` (`TestOnlyReads`).

## 4. Rules and their sources
| Rule | Source |
|---|---|
| Files: `Dir::Etc::parts`'s, then `Dir::Etc::main` where present. `apt-config dump Dir` must give `Dir "/"`, `Dir::Etc "etc/apt"` (or `etc/apt/`), `parts "apt.conf.d"`, `main "apt.conf"`, else refused | VMs' dumps (`01-inspect-b`, `02-inspect-c`); the order is pkgInitConfig's **[unverified]**, checked by the cross-check |
| Parts order: byte order of the names; `52hardn-auto-upgrades` sorts after `50unattended-upgrades`, before `70debconf` | `01-inspect-b` (`LC_ALL=C sort`); apt's own sort **[unverified]** |
| Names, as apt's `GetListOfFilesInDir` with extensions `conf` and none (`ReadConfigDir`): a name starting with `.` is skipped first; then one with an extension other than `conf` (after the last `.`, as `flExtension`), a byte other than an ASCII letter or digit, `_`, `-`, `:` or `.`, or a `.` last, is skipped too, read for nothing (`20auto-upgrades.ucf-dist`, `foo.txt`, `52hardn~`), not refused. apt notices such a name unless a `Dir::Ignore-Files-Silently` item matches it, which silences only the notice, so hardn does not use those items. An entry with a name apt takes that is not a regular file is refused. The cross-check against `apt-config dump` stays the guard | `apt-pkg/contrib/fileutl.cc` and `configuration.cc`, apt's main branch (salsa.debian.org apt-team/apt, read 2026-11-03); the same code in apt 2.6.1 and 3.0.3 **[unverified]**; whether apt's `isalpha` takes a byte outside ASCII in the host's locale **[unverified]** (such a name is skipped here) |
| `A::B "v";`, blocks `A { B "v"; };` (`}` with or without `;`), list items `"v";` in a block, quoted values on one line | apt.conf(5) SYNTAX; `02-inspect-c` files |
| `#` and `//` comment to the end of the line, `/* */` to its close, all outside quotes (`"http://192.0.2.10/#x"` is a value) | apt.conf(5); `02-inspect-c` files use `#` and `//` |
| Names compare without case; a later value replaces an earlier; a list item always adds | apt.conf(5) |
| Refused: `#clear`, `#include` (any `#` word starting so), an unquoted value, an empty tag or a trailing `::`, an item outside a block, a brace out of place, a block or quote or comment not closed | no VM's file uses any (`02-inspect-c`); apt's reading of them not built |

## 5. Not run
No VM has run `Read`: the cross-check against a real `apt-config dump` of `APT::Periodic` and the origins is the first thing unit (b)'s VM run checks. An empty block (`A { };`) makes no item here; what apt does is **[unverified]**, and the cross-check would refuse a difference under a root.

# Several syslog servers: design

Status: **adopted 2026-11-14.** The maintainer adopted every recommended answer, (a), of §4's twelve decisions, in these words: "Adopt all". Units (a) to (d) are built and tested in process (§6); nothing has run on a host (§5). He decided the same day that hardn.yml's `logForwarding`, one server, becomes `syslogServers`, a list, and hardn forwards to each, "for flexibility and scalability". Nothing of the feature is released, so the old key gets no shim. §1 to §4 are the design as he adopted it; where the build differs, §6 says so.

## 1. What is there

One server everywhere ([screen-logforward.md](screen-logforward.md)):

| Part | Today |
|---|---|
| `pkg/config/logforward.go` | `LogForwarding{Server, Port, Protocol, CAFile}`; `Resolve` checks it and fills the defaults (tcp; 514, or 6514 for tls) |
| `pkg/host/logfwd` | `Read` already returns every forwarding it finds (`Reading.Forwards`): each `omfwd` action and `@` line, each `-R` of `SYSLOGD_OPTS`. `RsyslogFile(s)` writes one action; `SetOpts` adds one `-R` and refuses another that is not hardn's |
| `pkg/engine/controls/logforward` | `Reading.Now` is one forwarding (hardn's, else the first) and `Want` one server; the row, `Differs` and `ReadConn` are about that one |
| `pkg/ui/screen/logforward.go` | one Server, Protocol, CA File and Connection row |
| hardn's file | `/etc/rsyslog.d/60-hardn-forward.conf`: one `*.*` action; `queue.type="linkedList"` for TCP and TLS, no queue for UDP |
| `hardn check` | reads nothing of syslog (`pkg/engine/drift`); adopted for later ([gallery-logforward.md](gallery-logforward.md), decision 14) |

Read on 2026-11-14, changing nothing:

| Host | Fact |
|---|---|
| alpine324-test-116 (Alpine 3.24.4) | BusyBox v1.37.0. `syslogd --help`: `-R HOST[:PORT]`, one host per `-R`. The binary's option string is `m:nO:l:Sts:b:R:*LC::Df:K`: `R:*` is a list, so `-R` may be given more than once. `SYSLOGD_OPTS="-t"`; rsyslog 8.2608.0 is in main, not installed |
| busybox `syslogd.c` (master) | each `-R` is added to `G.remoteHosts`, and every message is sent to each with `sendto(..., MSG_DONTWAIT)`: no host waits on another |
| centos10-test-120 (rsyslog 8.2604.0) | `rsyslogd -N1`, the file given on stdin, accepts three `omfwd` actions in one file, and a queue with `queue.timeoutEnqueue="0"` and `action.resumeRetryCount="-1"` on a TCP and on a UDP action. `rsyslog-gnutls` is not installed |
| ubuntu26-vm | rsyslog 8.2512.0 |
| deb13-vm | no rsyslog (journald alone) |
| rsyslog's documents | an action that fails discards its messages, and `action.resumeRetryCount` is 0 by default, -1 "eternal" ([actions](https://raw.githubusercontent.com/rsyslog/rsyslog/main/doc/source/configuration/actions.rst)). A full queue waits `queue.timeoutEnqueue`, 2000 ms by default, then discards; 0 discards at once. An action queue holds 1000 messages "in 8.30.0", and the default "depends on queue type and rsyslog version" ([queue parameters](https://raw.githubusercontent.com/rsyslog/rsyslog/main/doc/source/rainerscript/queue_parameters.rst)) |

## 2. The design

Each point is the recommended answer of §4.

### 2.1 The key

```yaml
syslogServers:
  - server: logs.example.net        # address or name
    protocol: tls                   # udp, tcp, tls; default tcp
    caFile: /etc/ssl/example-ca.pem # tls only
  - server: 192.0.2.51              # port: 514; 6514 for tls
  - server: 192.0.2.52
    protocol: udp
```

- A list of blocks, each today's four keys with today's checks and defaults. No string form.
- One to four servers, as `ntpServers`. No list, or an empty one: nothing is set.
- Not valid, saying which entry ("server 2: port is not between 1 and 65535"): a block with no `server`; more than four; the same address and port twice, whatever the protocol.
- Protocols may be mixed. `caFile` is each TLS server's own.
- `logForwarding` in a file is then a key nothing reads. *As built:* it is not listed under "Not used" and gets no warning, as any key hardn does not know: that section lists the retired keys of released versions alone (§6.2).

The hardn.yml screen, addresses alone. The example's three are over the row's width, so they stack, as `sshConfigFile` does:

```
  Syslog -------------------------------------------------------

   -  syslogServers
      logs.example.net, 192.0.2.51, 192.0.2.52
```

### 2.2 rsyslog

One file, the same path and first line, one action per server in hardn.yml's order. Every action gets its own queue, UDP too:

```
# Written by hardn (Syslog Settings).
*.* action(type="omfwd" protocol="tcp"
    target="192.0.2.51" port="514"
    queue.type="linkedList" queue.timeoutEnqueue="0"
    action.resumeRetryCount="-1")
*.* action(type="omfwd" protocol="udp"
    target="192.0.2.52" port="514"
    queue.type="linkedList" queue.timeoutEnqueue="0"
    action.resumeRetryCount="-1")
```

- **Today's file** has a queue for TCP and TLS, none for UDP, and nothing else. By rsyslog's documents a server that is away makes the action fail and discard, and a full queue holds each message 2 s: that wait is what would slow the other servers and the local files.
- **Proposed:** retry for ever, so the queue (in memory, rsyslog's own size) holds what it can while the server is away; when it is full, that server's new messages are dropped at once and nothing else waits.
- TLS lines are as today, per action; `rsyslog-gnutls` is installed once.

### 2.3 Alpine

busybox syslogd takes several `-R` (§1), each UDP. hardn adds one `-R` per server, and `-L` as today; its mark names them all:

```
# Added by hardn (Syslog Settings): -R 192.0.2.51:514 -R 192.0.2.52:514 -L
SYSLOGD_OPTS="-t -R 192.0.2.51:514 -R 192.0.2.52:514 -L"
```

One TCP or TLS server in the list refuses the whole list; nothing is set for the others (§4 decision 5):

```
# Warning
# busybox syslogd forwards over UDP only. Set protocol: udp for
# logs.example.net in hardn.yml, then try again.

   1) Set servers          busybox syslogd is UDP only
```

### 2.4 The row and the screen

The row's state and detail share 37 cells at 64 columns, so servers are counted, not named, when there are several:

| Host | Row |
|---|---|
| one server, as asked | `Configured 192.0.2.51:514 TCP` (as today) |
| several, every TCP and TLS connection open | `Configured 3 servers` |
| one or more connections down | `Partial 1 of 3 not reached` |
| hardn's servers are not hardn.yml's list | `Partial not hardn.yml's servers` |
| settings or sockets not read | Unknown, as today |

One connection down is Partial: the host is not doing all that was asked. The row's evidence is one line per server, its file and line, and each server not reached by name.

The main menu's status rows, the last three:

```
  [ ] Syslog               Partial 1 of 3 not reached
  [+] NTP                  Configured 192.0.2.10
  [+] AppArmor             Configured 7 profiles enforced
```

The screen: one row per server, for one server too. The Protocol and Connection rows go into it; a CA File row follows its server.

```
# Syslog Settings ##############################################

  [ ] Syslog               Partial 1 of 3 not reached

  Forwarding ---------------------------------------------------

   -  Logger               rsyslog
  [+] Server               logs.example.net:6514 TLS Open
   -  CA File              /etc/ssl/example-ca.pem
  [ ] Server               192.0.2.51:514 TCP not reached
  [ ] Server               192.0.2.52:514 UDP losses unseen
   -  File                 /etc/rsyslog.d/60-hardn-forward.conf
   -  Set by               hardn
   -  hardn.yml            3 servers

  Select an option

   1) Stop forwarding      Undo hardn's change

   0) Return

    > Option [0-1 or q]:
```

A TCP or TLS row ends in its connection, the status row's words: `192.0.2.51:514 TCP Open`, or `192.0.2.51:514 TCP not reached` (today's Connection row says "None"). "not encrypted" is dropped from the row. The hardn.yml row is the server with one, the count with several, and "differs" as today.

**A long row.** A Server row over the 37 cells stacks, as `sshConfigFile` does on the hardn.yml screen: the key alone, the whole value on the next line (58 cells). A name still too long is cut, ending "...", before its port.

```
  [+] Server
      syslog-collector.prod.example.com:6514 TLS Open
```

**Servers by name.** `ss` prints addresses, not names. Today `ReadConn` calls a named server Open on rsyslogd's first established socket to its port, whichever server that goes to. With a list that is wrong: beside 192.0.2.51 on port 514, a dead logs.example.net would read Open. So `ReadConn` changes, and works per port:

1. Count rsyslogd's established sockets to the port.
2. Take out those whose peer is a server of the list given by address.
3. Compare what is left, n, with the port's named TCP and TLS servers, m.

n = m: each is Open. n = 0: each is not reached. Between: hardn can say how many are down, not which, so each of those rows reads "1 of 2 open", never Open, and the status row counts m - n not reached.

### 2.5 Set servers, Stop forwarding

- **Set servers** (the item's new name, as NTP's) makes hardn's file hold exactly hardn.yml's list: one write, one `rsyslogd -N1`, one restart, one test line. Adding or removing a server is the same change, and the review's diff shows it.
- **All or nothing** (§4 decision 5). One entry not valid, one CA file missing, one TCP server on Alpine: the whole change is refused before anything is asked. rsyslogd refusing the file puts the old file back, every server as before.
- **Stop forwarding** takes out all of hardn's servers. An empty list in hardn.yml stops nothing: Set servers refuses (`SYSLOG_NO_SERVER`), and Stop forwarding is the way.
- **The test line** is one `logger` line: the logger sends it to every server, so one per server is not possible through it.
- The question: "Forward logs to 192.0.2.51?" with one, "Forward logs to 3 servers?" with several.

Adding a third server, reviewed (the plan's first step):

```
# Set servers ##################################################

  Forwarding ---------------------------------------------------

   -  Server               192.0.2.51:514 TCP
   -  Server               192.0.2.52:514 UDP losses unseen
   -  Server               192.0.2.53:514 TCP

   -  Write hardn's forwarding file
      /etc/rsyslog.d/60-hardn-forward.conf
      @@ -7,3 +7,7 @@
           target="192.0.2.52" port="514"
           queue.type="linkedList" queue.timeoutEnqueue="0"
           action.resumeRetryCount="-1")
      +*.* action(type="omfwd" protocol="tcp"
      +    target="192.0.2.53" port="514"
      +    queue.type="linkedList" queue.timeoutEnqueue="0"
      +    action.resumeRetryCount="-1")
      $ rsyslogd -N1

  Forward logs to 3 servers? [y/N, 0 or q]:
```

The restart and the test line follow the file's step, as today.

### 2.6 Servers set outside hardn

As adopted ([gallery-logforward.md](gallery-logforward.md) decision 7 (a); §4 decision 8): hardn shows them, each a Server row, "Set by Not hardn", and Set servers is unavailable, "Set outside hardn". hardn never adds its servers beside another file's. A host that already forwards to exactly hardn.yml's list, by whoever, plans nothing.

### 2.7 `hardn check` and the documents

- **`hardn check`** reads nothing of syslog today. When it does, the fact is the set of servers the host forwards to, hardn's or not, each "address:port protocol"; a server added or removed is one difference line. Connections are not compared: they come and go.
- **Markdown and JSON:** the same row. Its detail is the words of §2.4, and its evidence one line per server. No new field. A plan's change ids stay (`logs.forward/file`, `/options`, `/restart`, `/test`).

### 2.8 Codes

No new code. The words change:

| Code | Change |
|---|---|
| `SYSLOG_NO_SERVER` | "hardn.yml names no syslog server. Add syslogServers, then try again." |
| `SYSLOG_BAD_SERVER` | names the key and the entry; also more than four, a server twice, a block with no `server` |
| `SYSLOG_UDP_ONLY`, `SYSLOG_NO_CA_FILE` | name the server, or its file |
| the other six | as they are |

## 3. Units

| Unit | What | Host run |
|---|---|---|
| (a) the key | `config.SyslogServers`, its `Resolve`; hardn.yml.example, the embedded file, configuration.md; the hardn.yml screen's row | no |
| (b) rsyslog | `RsyslogFile` for a list; the control's reading, row, plan and refusals; the screen; the gallery's hosts | yes: two servers, one stopped. The other still gets lines, `/var/log/messages` is not held up, and the stopped one's row reads not reached. Then a named and an address server on one port, the named one stopped: its row reads not reached |
| (c) Alpine | `SetOpts` for a list, the mark, the refusal | yes: two `-R` on Alpine 3.24, a line seen on both servers; Stop forwarding takes both out |
| (d) `hardn check` | the fact, in the baseline | no |

(a) gates the rest. (b) and (c) can be built side by side. (d) is last, and may wait.

## 4. Decisions for the maintainer

In the order they gate the units of §3.

1. **The key's shape.** (a) *Recommended:* a list of blocks, each `server`, `port`, `protocol`, `caFile`; no other form. (b) Also a string per entry, `192.0.2.51` or `tcp://192.0.2.51:514`: shorter to write, a second format to check and explain. (c) A list of strings alone, as `ntpServers`: no place for `caFile`. Gates unit (a) and every decision below.
2. **How many.** (a) *Recommended:* one to four, as `ntpServers`. (b) Eight: more room, a longer screen. (c) No cap. Gates unit (a)'s check for too many servers and the Syslog Settings screen's height (a Server row for each, and a CA File row for each TLS one).
3. **rsyslog's queues.** (a) *Recommended:* a queue on every action, UDP too, with `queue.timeoutEnqueue="0"` and `action.resumeRetryCount="-1"`: a server that is away is retried, and a full queue drops that server's messages, not everyone's time. In memory; lost at a restart. (b) Today's lines, `queue.type="linkedList"` for TCP and TLS alone: nothing new, and by rsyslog's documents a dead server's messages are discarded and a full queue holds each message 2 s. (c) (a) with the queue kept on disk (`queue.filename`, `queue.saveOnShutdown`): survives a restart; files under rsyslog's work directory, and SELinux and AppArmor to settle. Gates unit (b)'s file and its host run.
4. **Alpine with several servers.** (a) *Recommended:* one `-R` per server, each UDP. Read in busybox's source and the binary's option string; not yet run with two. (b) Refuse more than one (a new code, `SYSLOG_ONE_SERVER`): nothing to verify, and Alpine gets less than the others. (c) The first alone, with a warning: forwards something, and not what hardn.yml says. (d) Install rsyslog on Alpine: every protocol, and hardn swaps a base service. Gates unit (c).
5. **A list that is partly unusable** (one entry not valid, one CA file missing, one TCP or TLS server on Alpine). (a) *Recommended:* refuse it all: one change, one undo, and the host never forwards to less than hardn.yml says without a refusal. (b) Set the usable servers and warn about the rest, the row Partial: some logs go at once; the change and its undo are then about part of the list. Gates units (b) and (c).
6. **One connection of several down.** (a) *Recommended:* Partial, "1 of 3 not reached". (b) Configured while one server is reached, the detail "2 of 3 reached": green with logs going missing. Gates unit (b).
7. **The screen's rows.** (a) *Recommended:* one row per server always, protocol and connection in it ("192.0.2.51:514 TCP Open", "... TCP not reached"); "not encrypted" goes. One form to build and test; a long name does not fit the row and stacks onto a second line (§2.4). (b) Today's four rows for one server, the short row for two or more: nothing changes for one server, two forms. (c) Today's four rows repeated per server: every value has its own row, up to sixteen rows. Gates unit (b).
8. **Servers set outside hardn.** (a) *Recommended:* as adopted: shown, and Set servers unavailable. (b) hardn adds its servers beside them, refusing only a server both would send to: more hosts can use it; hardn then judges its own servers alone, and the host forwards to more than hardn.yml says. Gates units (b) and (c).
9. **The test line.** (a) *Recommended:* one line through the logger, which reaches every server. (b) Also one straight to each server (`logger -n`): says which server was sent to, does not go through rsyslog, and busybox's `logger` was not checked for it. Gates units (b) and (c).
10. **Codes.** (a) *Recommended:* no new code; `SYSLOG_BAD_SERVER` covers too many and a server twice, in words. (b) `SYSLOG_TOO_MANY` and `SYSLOG_DUPLICATE`: a script can tell them apart. Gates units (b) and (c).
11. **`hardn check`.** (a) *Recommended:* unit (d), after the rest: the set of servers, connections not compared. (b) Not in this work: decision 14 of the first design stays a follow-up. Gates unit (d).
12. **The same server twice.** (a) *Recommended:* the same address and port twice is not valid, whatever the protocol. (b) Only the same address, port and protocol: UDP and TCP to one port are then both sent. Gates nothing: one check in unit (a), changed later at little cost.

## 5. Not verified

§6.5 is what ran on hosts. Still open:

- **Two TLS servers with two CA files** in one file: not run. `StreamDriver.CAFile` per action is from rsyslog's documents (8.2108.0).
- **rsyslog 8.2112.0** (Ubuntu 22.04), the oldest supported: hardn's lines not checked there.
- **A dead server for longer than its queue holds**, and a UDP action to a name that does not resolve.
- **A name that resolves to an address also in the list** (counted once too few), and two names resolving to one address.
- **Alpine 3.22 and 3.23**: two `-R` ran on 3.24 alone.
- **The stacked rows** (a long server name) and **`hardn check`'s new fact**: not seen on a host.
- Debian, Ubuntu and Proxmox VE: not run with several servers.

## 6. Built

On `next/names`, 2026-11-14, on top of the rename to Syslog Settings. In process, on `hostfake`, through the real engine; `make check` passes on macOS. No hardn binary ran.

### 6.1 What, and where

| Unit | What | Where |
|---|---|---|
| (a) | `config.SyslogServers`, a list of `SyslogServer` blocks; `Resolve` checks the whole list and names the entry (`ErrSyslogServers`) | `pkg/config/logforward.go`, `config.go`, `embed.go`; `hardn.yml.example` |
| (a) | the hardn.yml screen's `syslogServers` row, addresses alone, stacked when long | `pkg/screens/config/config.go`, `pkg/ui/screen/config.go`; `KeyValue.StackLong` in `pkg/ui/component/keyvalue.go` |
| (b) | `RsyslogFile` for a list, a queue on every action; `Reading.Shown` and `Own` in place of `Now` | `pkg/host/logfwd/rsyslog.go`, `logfwd.go` |
| (b) | `ReadConns`, §2.4's count per port (`ConnState.Open`, `Of`) | `pkg/host/logfwd/conn.go` |
| (b) | the control: `Reading.Now` and `Want` are lists; the row, its evidence, the plan and the refusals | `pkg/engine/controls/logforward` |
| (b) | the screen: a Server row per server, Set servers, the review and the question | `pkg/ui/screen/logforward.go`, `pkg/screens/logforward` |
| (c) | `SetOpts` for a list: one `-R` each, the mark naming them all; the whole list refused for one TCP or TLS server | `pkg/host/logfwd/busybox.go`; the control's `planSet` |
| (d) | the fact `logs.servers`, "Syslog servers", a set, each `192.0.2.51:514 TCP` | `pkg/engine/drift` |
| | Run All's row says "Set servers" | `pkg/engine/runall` |
| | the gallery: hosts `several`, `several-down`, `several-differ`, `long-name`, `names`, `alpine-several`; reviews and refusals for each rule; `config/syslog` | `internal/tools/uigallery/logforward.go`, `config.go`, and their goldens |

### 6.2 Settled where the page was silent, or differs

- **`logForwarding` is not listed under "Not used"** (§2.1 said it would be). No key hardn does not know is: the section lists retired keys of released versions, and this one was never released.
- **The order of the servers does not count.** A host that forwards to hardn.yml's servers in another order is as asked, and plans nothing.
- **Which servers are shown.** hardn's own where it has any, else every server set outside hardn. With the latter in more than one file, the File row reads "2 files".
- **One server's words are as before:** "server not reached", "not hardn.yml's servers" for one too. The row's evidence is "file:line server", ending ", not reached" or ", 1 of 2 open".
- **Sockets not read** (no `ss`): the row is Unknown, "connection not read", unless the servers are not hardn.yml's, which is Partial; each TCP or TLS Server row is Unknown. A list of UDP servers alone does not run `ss`.
- **More of rsyslogd's sockets to a port than it has named servers** does not tell which is whose: Unknown, "connection not read", not Open.
- **A server set outside hardn beside hardn's own** is not shown, and is more than hardn.yml's list: the row is Partial, "not hardn.yml's servers", its evidence lists that server last, the hardn.yml row reads "differs", and Set servers refuses (`SYSLOG_SET_OUTSIDE`). Its sockets are not counted as a named server's. Where it is itself given by name on a named server's port and not every one is reached, that is Unknown too.
- **The notice** with several servers: "Every message the host logs goes to each server, as it is logged."
- **Refusals name the server:** `SYSLOG_UDP_ONLY` says "set protocol: udp for logs.example.net, 192.0.2.51 in hardn.yml"; `SYSLOG_NO_CA_FILE` "logs.example.net's caFile, <path>, is not a file on this host". A block with no `server` is "server 2: no server"; a server twice "server 3: the same address and port as server 1".
- **A long row.** Rows stack through a new `KeyValue.StackLong`; a name too long even stacked is cut by the screen before its port, with room kept for "...". The hardn.yml row of Syslog Settings stacks too. **The status row is not changed:** with one long name it is cut at its 37 cells ("Configured syslog-collector.prod.e...").
- **Two named servers, one connection** stack at 64 columns: "logs-a.example.net:514 TCP 1 of 2 open" is 38 cells.
- **`hardn check`'s baseline** keeps format 1 with one more fact. `hardn check` is not released either; a baseline saved by an earlier build of this branch is refused (`BASELINE_INVALID`) until `hardn check --accept` saves it again.
- **The main menu's rows of §2.4** have no gallery mock of their own: the status row is the same one, drawn above each screen mock.
- **Alpine's restart, the test line and the change ids** are as they were.

### 6.3 Tests

| Rule | Test |
|---|---|
| valid and invalid lists, too many, a server twice, a block with no server, the entry named | `pkg/config`: `TestSyslogServersResolve`, `TestSyslogServersList`, `TestSyslogServersYAML` |
| the file for a list, read back; one bad server refuses the list | `pkg/host/logfwd`: `TestRsyslogFile`, `TestServerCheck`, `TestShownPrefersHardn` |
| several `-R`, the mark, the list changed, cleared; one TCP or TLS entry refused | `pkg/host/logfwd`: `TestSetOptsSeveral` |
| §2.4's count: a name beside an address on its port, two names with one socket | `pkg/host/logfwd`: `TestReadConns` |
| the row: "3 servers", "1 of 3 not reached", "1 of 2 not reached", not hardn.yml's servers; the evidence | `pkg/engine/controls/logforward`: `TestStatus`, `TestEvidence` |
| every refusal; a partly unusable list refused whole, nothing written | `TestRefusals`, `TestRefusedListSetsNothing`, `TestForeignList` |
| the list changed as one change, its diff, its rollback, Stop | `TestSetSeveral`, `TestSetSeveralCheckFails`, `TestAlpineSeveral` |
| the screen's rows, items, review and question; a long name stacking and cut before its port | `pkg/ui/screen`: `TestLogForwardRows`, `TestLogForwardLongName`, `TestLogForwardItems`, `TestLogForwardChanges`; `pkg/ui/component`: the `StackLong` cases |
| the screen on a host, set then stopped, refusals said | `pkg/screens/logforward`: `TestView`, `TestSetSeveral`, `TestRefused` |
| the hardn.yml screen's row; `logForwarding` not read | `pkg/screens/config`: `TestSyslogServersRow` |
| `hardn check`: a server added, removed, reordered, not established | `pkg/engine/drift`: `TestDifferences`, `TestReadEstablishes` |
| the gallery's mocks against the design's drawings, and played | `internal/tools/uigallery`: `TestLogForwardMocks`, `TestLogForwardGolden`, `TestPlayLogForward`, `TestConfigMocks` |

### 6.4 Host runs still needed

Runs 1, 2, 5 and 6 of the first list are done (§6.5). Left:

1. **`rsyslogd -N1` and a restart with hardn's file on rsyslog 8.2112.0** (Ubuntu 22.04).
2. **Two TLS servers with two CA files** in one file: accepted, both connect.
3. **A third server added** to a host forwarding to two: its diff that server, the others still connected after the restart.
4. **`hardn check --accept`, add a server, `hardn check`:** one line, "Syslog server added", exit 3.

### 6.5 On hosts

2026-11-14. rsyslog on centos10-test-120 (CentOS Stream 10, 8.2604.0 as read in §1) and busybox syslogd on alpine324-test-116 (Alpine 3.24, a container), forwarding to rsyslog on centos9-test-119 and oracle10-test-118. All four left as they started.

| Case | Result |
|---|---|
| CentOS: two TCP servers, Set servers | one file, two actions; "Configured 2 servers", both Open; a test line on both |
| the second server stopped, 300 lines logged | logged in 1 s, all in `/var/log/messages` and on the first server; "Partial 1 of 2 not reached" |
| the second server back | its 300 lines arrive from the queue; "Configured 2 servers" |
| the list changed to an address and a name on one port | "Partial not hardn.yml's servers", then Set servers: one changed line, both Open |
| the named server stopped | its row "not reached", the other Open |
| a TLS entry whose `caFile` is not there | Set servers not offered ("hardn.yml is not valid"); the file not changed |
| Stop forwarding | the file removed; `/etc/rsyslog.d` and `rsyslog.conf` as before |
| Alpine: two UDP servers, Set servers | `SYSLOGD_OPTS="-t -R 10.2.8.142:514 -R 10.2.8.141:514 -L"`, syslogd running with both; a test line on both |
| one entry changed to TCP | Set servers refused whole, "busybox syslogd is UDP only"; the file not changed |
| Stop forwarding | `/etc/conf.d/syslog` as before, by checksum |

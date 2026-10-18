package pve

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Root's keys on Proxmox VE. pvecm's setup_ssh_keys makes RootKeys a link
// to ClusterKeys, which pmxcfs replicates to every node; NodeKey is the
// node's own root key, which setup_rootsshconfig generates.
const (
	RootKeys    = "/root/.ssh/authorized_keys"
	ClusterKeys = "/etc/pve/priv/authorized_keys"
	NodeKey     = "/root/.ssh/id_rsa.pub"
	NodesDir    = "/etc/pve/nodes"
	// KeysPerm is the only mode pmxcfs lets a file under /etc/pve/priv
	// have (pmxcfs.c, cfs_fuse_chmod).
	KeysPerm fs.FileMode = 0o600
)

// Keys is root's cluster-wide authorized_keys as ReadKeys found it.
type Keys struct {
	Target string   // RootKeys's target, as stored
	Data   []byte   // ClusterKeys's content
	Nodes  []string // the cluster's nodes: the directories in NodesDir
	Own    string   // NodeKey's type and key ("" when unreadable)
}

// ReadKeys reads root's authorized_keys through its link on a Proxmox VE
// host, and refuses anything else: ErrNotApplicable or ErrProxmoxUnknown off
// Proxmox VE, ErrClusterFSDown when pmxcfs is not mounted (the link would
// point into an empty directory), and ErrKeysLink when RootKeys is not a
// link resolving to ClusterKeys, or ClusterKeys is missing: pvecm
// updatecerts restores that state, hardn does not. The link is resolved with
// ResolveSymlink, so one in a directory another user can change is refused
// (host.ErrSymlinkParent). It changes nothing.
func ReadKeys(ctx context.Context, sys host.System, p detect.Proxmox) (Keys, error) {
	switch ok, err := Applies(p); {
	case err != nil:
		return Keys{}, err
	case !ok:
		return Keys{}, fmt.Errorf("pve: %s: %w", RootKeys, ErrNotApplicable)
	case p.ClusterFS != detect.Yes && p.Err != nil:
		return Keys{}, fmt.Errorf("pve: %s: %w: %w", RootKeys, ErrClusterFSDown, p.Err)
	case p.ClusterFS != detect.Yes:
		return Keys{}, fmt.Errorf("pve: %s: %w", RootKeys, ErrClusterFSDown)
	}
	fi, err := sys.Lstat(ctx, RootKeys)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Keys{}, fmt.Errorf("pve: %s is missing: %w", RootKeys, ErrKeysLink)
	case err != nil:
		return Keys{}, fmt.Errorf("pve: %w", err)
	case fi.Mode()&fs.ModeSymlink == 0:
		return Keys{}, fmt.Errorf("pve: %s is not a link (%v): %w", RootKeys, fi.Mode().Type(), ErrKeysLink)
	}
	target, resolved, err := sys.ResolveSymlink(ctx, RootKeys)
	if err != nil {
		return Keys{}, fmt.Errorf("pve: %w", err)
	}
	if resolved != ClusterKeys {
		return Keys{}, fmt.Errorf("pve: %s points to %s: %w", RootKeys, resolved, ErrKeysLink)
	}
	data, _, err := sys.ReadFileNoFollow(ctx, ClusterKeys)
	if errors.Is(err, fs.ErrNotExist) {
		return Keys{}, fmt.Errorf("pve: %s is missing: %w", ClusterKeys, ErrKeysLink)
	} else if err != nil {
		return Keys{}, fmt.Errorf("pve: %w", err)
	}
	ents, err := sys.ReadDir(ctx, NodesDir)
	if err != nil {
		return Keys{}, fmt.Errorf("pve: the cluster's nodes: %w", err)
	}
	k := Keys{Target: target, Data: data}
	for _, e := range ents {
		if e.IsDir() {
			k.Nodes = append(k.Nodes, e.Name())
		}
	}
	if own, err := sys.ReadFile(ctx, NodeKey); err == nil {
		k.Own, _, _ = parseKey(strings.TrimSpace(string(own)))
	}
	return k, nil
}

// KeyEdit is a change to root's keys: lines to add, and keys to remove
// (each named by a key line; its options and comment are not compared).
type KeyEdit struct {
	Add, Remove []string
}

// KeysPlan is an edit of the cluster-wide keys file.
type KeysPlan struct {
	Target   string   // RootKeys's target when read
	Before   [32]byte // SHA-256 of Old
	Old, New []byte
	Added    []string // lines added
	Removed  []string // lines removed, as they were
	Findings []Finding
}

// Changed reports whether the plan changes the file.
func (p KeysPlan) Changed() bool { return len(p.Added)+len(p.Removed) > 0 }

// Narrows reports whether the plan removes a key: a lockout-risk change
// (p2-revert's RiskLockout), where adding one is not.
func (p KeysPlan) Narrows() bool { return len(p.Removed) > 0 }

// PlanKeys plans e against k, keeping every other line as it is. A line to
// add must be one key line (ErrBadKey otherwise); a key already there is not
// added again. Removing a cluster node's root key (NodeKey, or a key whose
// comment is root@<node> for a node in NodesDir), or the last key, is
// refused with ErrClusterKey: other nodes log in as root with them. A plan
// that changes the file carries a FindingClusterWide: the edit applies to
// every node.
func PlanKeys(k Keys, e KeyEdit) (KeysPlan, error) {
	return plan(k, e, true)
}

func plan(k Keys, e KeyEdit, guard bool) (KeysPlan, error) {
	p := KeysPlan{Target: k.Target, Before: sha256.Sum256(k.Data), Old: k.Data}
	lines := strings.SplitAfter(string(k.Data), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	drop := map[string]bool{}
	for _, r := range e.Remove {
		id, _, ok := parseKey(r)
		if !ok {
			return KeysPlan{}, fmt.Errorf("pve: remove %q: %w", r, ErrBadKey)
		}
		drop[id] = true
	}
	var kept []string
	have := map[string]bool{}
	for _, l := range lines {
		id, comment, ok := parseKey(strings.TrimRight(l, "\r\n"))
		switch {
		case ok && drop[id] && guard && (id == k.Own || slices.ContainsFunc(k.Nodes, func(n string) bool { return comment == "root@"+n })):
			return KeysPlan{}, fmt.Errorf("pve: remove %s (%s): %w", id, comment, ErrClusterKey)
		case ok && drop[id]:
			p.Removed = append(p.Removed, strings.TrimRight(l, "\r\n"))
			continue
		case ok:
			have[id] = true
		}
		kept = append(kept, l)
	}
	for _, a := range e.Add {
		id, _, ok := parseKey(a)
		if !ok || strings.ContainsAny(a, "\r\n\x00") {
			return KeysPlan{}, fmt.Errorf("pve: add %q: %w", a, ErrBadKey)
		}
		if have[id] {
			continue
		}
		have[id] = true
		if n := len(kept); n > 0 && !strings.HasSuffix(kept[n-1], "\n") {
			kept[n-1] += "\n"
		}
		kept = append(kept, a+"\n")
		p.Added = append(p.Added, a)
	}
	if guard && len(have) == 0 && p.Changed() {
		return KeysPlan{}, fmt.Errorf("pve: the edit leaves %s without a key: %w", ClusterKeys, ErrClusterKey)
	}
	p.New = []byte(strings.Join(kept, ""))
	if p.Changed() {
		p.Findings = []Finding{{Kind: FindingClusterWide, File: ClusterKeys,
			Detail: RootKeys + " is a link to " + ClusterKeys + ": the change applies to root on every node of the cluster"}}
	}
	return p, nil
}

// ApplyKeys writes p through the link, never over it: it reads the keys
// again (ReadKeys) and refuses with ErrKeysChanged when they are not what p
// was planned from; takes a RollbackRequired backup of RootKeys in run
// (which records the link and ClusterKeys's content); then writes ClusterKeys
// with host.WriteFileThroughSymlink, mode KeysPerm, owner unchanged. Without
// a run it is ErrNoBackup, and a plan with no change writes nothing.
func ApplyKeys(ctx context.Context, sys host.System, pf detect.Proxmox, run *backup.Run, p KeysPlan) (backup.Result, error) {
	switch {
	case run == nil:
		return backup.Result{}, fmt.Errorf("pve: %w", ErrNoBackup)
	case !p.Changed():
		return backup.Result{}, nil
	}
	cur, err := ReadKeys(ctx, sys, pf)
	if err != nil {
		return backup.Result{}, err
	}
	if sha256.Sum256(cur.Data) != p.Before || cur.Target != p.Target {
		return backup.Result{}, fmt.Errorf("pve: %w", ErrKeysChanged)
	}
	res, err := run.Backup(ctx, RootKeys, backup.RollbackRequired)
	if err != nil {
		return backup.Result{}, fmt.Errorf("pve: %w", err)
	}
	if err := host.WriteFileThroughSymlink(ctx, sys, RootKeys, p.New, KeysPerm, -1, -1); err != nil {
		return res, fmt.Errorf("pve: %w", err)
	}
	return res, nil
}

// UndoKeys reverses p on the keys as they are now: it removes the lines p
// added and puts back the lines it removed, and leaves every other line, so
// a key another node wrote since is kept, where restoring the whole file
// from the backup would drop it. It writes through the link as ApplyKeys
// does, and nothing when there is nothing to reverse. After Ctrl-C, run it
// under context.WithoutCancel.
func UndoKeys(ctx context.Context, sys host.System, pf detect.Proxmox, p KeysPlan) error {
	if !p.Changed() {
		return nil
	}
	cur, err := ReadKeys(ctx, sys, pf)
	if err != nil {
		return err
	}
	inv, err := plan(cur, KeyEdit{Add: p.Removed, Remove: p.Added}, false)
	if err != nil || !inv.Changed() {
		return err
	}
	if err := host.WriteFileThroughSymlink(ctx, sys, RootKeys, inv.New, KeysPerm, -1, -1); err != nil {
		return fmt.Errorf("pve: %w", err)
	}
	return nil
}

// keyType is a key type's form: ssh-ed25519, ecdsa-sha2-nistp256,
// sk-ssh-ed25519@openssh.com, a certificate type.
var keyType = regexp.MustCompile(`\A[a-z0-9][a-z0-9@._-]*\z`)

// parseKey reads an authorized_keys line (sshd(8), AUTHORIZED_KEYS FILE
// FORMAT): options (optional; no spaces except within double quotes), key
// type, base64 key, comment. id is the type and the key, which is valid only
// when its base64 decodes to a blob starting with the type's name, as the
// SSH public key format does (RFC 4253 §6.6). ok is false for a comment, a
// blank line or anything else.
func parseKey(line string) (id, comment string, ok bool) {
	t := strings.TrimSpace(line)
	if t == "" || t[0] == '#' {
		return "", "", false
	}
	for _, rest := range []string{t, skipOptions(t)} {
		f := strings.Fields(rest)
		if len(f) >= 2 && keyType.MatchString(f[0]) && blobIs(f[1], f[0]) {
			return f[0] + " " + f[1], strings.Join(f[2:], " "), true
		}
	}
	return "", "", false
}

// skipOptions returns t after its options field: up to the first space or
// tab outside double quotes (a backslash escapes a quote).
func skipOptions(t string) string {
	quoted := false
	for i := 0; i < len(t); i++ {
		switch c := t[i]; {
		case c == '\\' && quoted:
			i++
		case c == '"':
			quoted = !quoted
		case (c == ' ' || c == '\t') && !quoted:
			return t[i:]
		}
	}
	return ""
}

func blobIs(b64, typ string) bool {
	blob, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(blob) < 4 {
		return false
	}
	n := binary.BigEndian.Uint32(blob)
	return uint64(n) <= uint64(len(blob)-4) && bytes.Equal(blob[4:4+n], []byte(typ))
}

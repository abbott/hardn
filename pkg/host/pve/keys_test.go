package pve_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/pve"
)

var ctx = context.Background()

func TestReadKeys(t *testing.T) {
	h := pve9(t)
	k, err := pve.ReadKeys(ctx, h, pveUp)
	if err != nil {
		t.Fatal(err)
	}
	if k.Target != pve.ClusterKeys || string(k.Data) != keyWeb01+"\n"+keyWeb02+"\n" ||
		!slices.Equal(k.Nodes, []string{"web-01", "web-02"}) || !strings.HasPrefix(keyWeb01, k.Own+" ") {
		t.Errorf("ReadKeys = %+v", k)
	}
	checkReadOnly(t, h)
	if len(h.Runs()) != 0 {
		t.Errorf("ReadKeys ran %q", h.Runs())
	}
}

func TestReadKeysRefuses(t *testing.T) {
	tests := []struct {
		name string
		edit func(h *hostfake.System)
		err  error
	}{
		{name: "not Proxmox VE", err: pve.ErrNotApplicable},
		{name: "unknown", err: pve.ErrProxmoxUnknown},
		{name: "pmxcfs down", err: pve.ErrClusterFSDown},
		{name: "a regular file", edit: func(h *hostfake.System) {
			_ = h.Remove(ctx, pve.RootKeys)
			h.AddFile(pve.RootKeys, []byte(keyAlice+"\n"), 0o600, 0, 0)
		}, err: pve.ErrKeysLink},
		{name: "missing", edit: func(h *hostfake.System) { _ = h.Remove(ctx, pve.RootKeys) }, err: pve.ErrKeysLink},
		{name: "a link elsewhere", edit: func(h *hostfake.System) {
			_ = h.Remove(ctx, pve.RootKeys)
			h.AddFile("/srv/keys", nil, 0o600, 0, 0)
			h.AddSymlink(pve.RootKeys, "/srv/keys")
		}, err: pve.ErrKeysLink},
		{name: "the cluster file missing", edit: func(h *hostfake.System) { _ = h.Remove(ctx, pve.ClusterKeys) }, err: pve.ErrKeysLink},
		{name: "the link in a directory alice owns", edit: func(h *hostfake.System) {
			h.AddDir("/root/.ssh", 0o700, 1000, 1000)
		}, err: host.ErrSymlinkParent},
		{name: "the link in a world-writable directory", edit: func(h *hostfake.System) {
			h.AddDir("/root/.ssh", 0o777, 0, 0)
		}, err: host.ErrSymlinkParent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := pve9(t)
			p := pveUp
			switch tt.name {
			case "not Proxmox VE":
				p = debian
			case "unknown":
				p = unknown
			case "pmxcfs down":
				p = pveDown
			}
			if tt.edit != nil {
				tt.edit(h)
			}
			before := len(h.Calls())
			if _, err := pve.ReadKeys(ctx, h, p); !errors.Is(err, tt.err) {
				t.Errorf("ReadKeys = %v, want %v", err, tt.err)
			}
			for _, c := range h.Calls()[before:] {
				if c.Op != "lstat" && c.Op != "resolve-symlink" && c.Op != "read-nofollow" {
					t.Errorf("unexpected call %s", c)
				}
			}
		})
	}
}

// TestReadKeysFails: a read that fails is the host's error, never "not
// Proxmox VE's link".
func TestReadKeysFails(t *testing.T) {
	for _, tt := range []struct{ op, path string }{{"read-nofollow", pve.ClusterKeys}, {"readdir", pve.NodesDir}} {
		f := &failing{System: pve9(t), op: tt.op, path: tt.path, err: syscall.EIO}
		if _, err := pve.ReadKeys(ctx, f, pveUp); !errors.Is(err, syscall.EIO) || errors.Is(err, pve.ErrKeysLink) {
			t.Errorf("%s %s failing: %v", tt.op, tt.path, err)
		}
	}
	// No id_rsa.pub: the node's own key is not known, and nothing fails.
	h := pve9(t)
	_ = h.Remove(ctx, pve.NodeKey)
	if k, err := pve.ReadKeys(ctx, h, pveUp); err != nil || k.Own != "" {
		t.Errorf("no node key: %+v, %v", k, err)
	}
}

func TestPlanKeys(t *testing.T) {
	base := pve.Keys{Target: pve.ClusterKeys, Data: []byte("# cluster keys\n" + keyWeb01 + "\n\n" + keyWeb02 + "\n" + keyAlice + "\n"),
		Nodes: []string{"web-01", "web-02"}, Own: strings.Join(strings.Fields(keyWeb01)[:2], " ")}
	withOptions := `from="192.0.2.0/24",command="/usr/bin/echo \"hi there\"" ` + keyBob
	tests := []struct {
		name           string
		k              pve.Keys
		e              pve.KeyEdit
		added, removed []string
		data           string // "" when unchanged
		err            error
	}{
		{name: "add bob", k: base, e: pve.KeyEdit{Add: []string{keyBob}}, added: []string{keyBob}, data: string(base.Data) + keyBob + "\n"},
		{name: "add with options", k: base, e: pve.KeyEdit{Add: []string{withOptions}}, added: []string{withOptions}, data: string(base.Data) + withOptions + "\n"},
		{name: "add to a file without a final newline", k: pve.Keys{Data: []byte(keyWeb01)}, e: pve.KeyEdit{Add: []string{keyBob}},
			added: []string{keyBob}, data: keyWeb01 + "\n" + keyBob + "\n"},
		{name: "add one already there, another comment", k: base, e: pve.KeyEdit{Add: []string{strings.Replace(keyAlice, "alice@web-01.example.net", "alice", 1)}}},
		{name: "add the same twice", k: base, e: pve.KeyEdit{Add: []string{keyBob, keyBob}}, added: []string{keyBob}, data: string(base.Data) + keyBob + "\n"},
		{name: "remove alice", k: base, e: pve.KeyEdit{Remove: []string{keyAlice}}, removed: []string{keyAlice},
			data: "# cluster keys\n" + keyWeb01 + "\n\n" + keyWeb02 + "\n"},
		{name: "remove by type and key alone", k: base, e: pve.KeyEdit{Remove: []string{strings.Join(strings.Fields(keyAlice)[:2], " ")}}, removed: []string{keyAlice},
			data: "# cluster keys\n" + keyWeb01 + "\n\n" + keyWeb02 + "\n"},
		{name: "remove one not there", k: base, e: pve.KeyEdit{Remove: []string{keyBob}}},
		{name: "remove another node's key", k: base, e: pve.KeyEdit{Remove: []string{keyWeb02}}, err: pve.ErrClusterKey},
		{name: "remove this node's key", k: pve.Keys{Data: []byte(strings.Replace(keyWeb01, "root@web-01", "renamed", 1) + "\n" + keyAlice + "\n"), Own: base.Own},
			e: pve.KeyEdit{Remove: []string{keyWeb01}}, err: pve.ErrClusterKey},
		{name: "remove the last key", k: pve.Keys{Data: []byte(keyAlice + "\n")}, e: pve.KeyEdit{Remove: []string{keyAlice}}, err: pve.ErrClusterKey},
		// A node's root key is Proxmox VE's: not added, as UndoKeys could
		// not remove it again; one already there is not added either.
		{name: "add this node's key where it is missing", k: pve.Keys{Data: []byte(keyAlice + "\n"), Own: base.Own}, e: pve.KeyEdit{Add: []string{keyWeb01}},
			err: pve.ErrClusterKey},
		{name: "add a key named as a node's", k: pve.Keys{Data: []byte(keyAlice + "\n"), Nodes: base.Nodes}, e: pve.KeyEdit{Add: []string{keyWeb02}},
			err: pve.ErrClusterKey},
		{name: "add this node's key, already there", k: base, e: pve.KeyEdit{Add: []string{keyWeb01}}},
		{name: "add a line that is not a key", k: base, e: pve.KeyEdit{Add: []string{"ssh-ed25519 not-base64 bob"}}, err: pve.ErrBadKey},
		{name: "add a blob of another type", k: base, e: pve.KeyEdit{Add: []string{strings.Replace(keyBob, "ssh-ed25519", "ssh-rsa", 1)}}, err: pve.ErrBadKey},
		{name: "add two lines in one", k: base, e: pve.KeyEdit{Add: []string{keyBob + "\n" + keyAlice}}, err: pve.ErrBadKey},
		{name: "add a comment", k: base, e: pve.KeyEdit{Add: []string{"# " + keyBob}}, err: pve.ErrBadKey},
		{name: "remove a line that is not a key", k: base, e: pve.KeyEdit{Remove: []string{"bob"}}, err: pve.ErrBadKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := pve.PlanKeys(tt.k, tt.e)
			if !errors.Is(err, tt.err) || (tt.err == nil) != (err == nil) {
				t.Fatalf("PlanKeys = %v, want %v", err, tt.err)
			}
			if err != nil {
				return
			}
			if !slices.Equal(p.Added, tt.added) || !slices.Equal(p.Removed, tt.removed) {
				t.Errorf("added %q removed %q, want %q %q", p.Added, p.Removed, tt.added, tt.removed)
			}
			want := tt.data
			if want == "" {
				want = string(tt.k.Data)
			}
			if string(p.New) != want {
				t.Errorf("New =\n%s\nwant\n%s", p.New, want)
			}
			if p.Changed() != (tt.data != "") || p.Narrows() != (len(tt.removed) > 0) {
				t.Errorf("Changed %v Narrows %v", p.Changed(), p.Narrows())
			}
			if cw := slices.ContainsFunc(p.Findings, func(f pve.Finding) bool { return f.Kind == pve.FindingClusterWide }); cw != p.Changed() {
				t.Errorf("cluster-wide finding %v for a change %v: %+v", cw, p.Changed(), p.Findings)
			}
		})
	}
}

// runs numbers the backup runs, whose ids must not repeat on one host.
var runs int

func openRun(t *testing.T, h host.System) *backup.Run {
	t.Helper()
	runs++
	run, err := backup.Open(h, backup.Settings{Path: backup.DefaultPath, Enabled: true}, fmt.Sprintf("20261018T090000Z-%08x", runs))
	if err != nil {
		t.Fatal(err)
	}
	return run
}

// writes lists the paths the fake was written, renamed or removed at.
func writes(h *hostfake.System, from int) []string {
	var ws []string
	for _, c := range h.Calls()[from:] {
		switch c.Op {
		case "write-atomic", "write-in-place", "rename", "remove", "removeall", "chmod", "chown":
			ws = append(ws, c.Op+" "+c.Path)
		}
	}
	return ws
}

// TestApplyKeys: the edit goes through the link, which stays as it is,
// after a backup that records the link and the cluster file.
func TestApplyKeys(t *testing.T) {
	h := pve9(t)
	k, err := pve.ReadKeys(ctx, h, pveUp)
	if err != nil {
		t.Fatal(err)
	}
	p, err := pve.PlanKeys(k, pve.KeyEdit{Add: []string{keyAlice}})
	if err != nil {
		t.Fatal(err)
	}
	before := len(h.Calls())
	run := openRun(t, h)
	res, err := pve.ApplyKeys(ctx, h, pveUp, run, p)
	if err != nil {
		t.Fatal(err)
	}
	if e := res.Entry; e.Kind != backup.KindSymlink || e.Path != pve.RootKeys || e.SymlinkResolved != pve.ClusterKeys || !e.RollbackRequired {
		t.Errorf("backup entry = %+v", e)
	}
	link, _ := h.Entry(pve.RootKeys)
	file, _ := h.Entry(pve.ClusterKeys)
	if link.Mode&fs.ModeSymlink == 0 || link.Target != pve.ClusterKeys {
		t.Errorf("the link is now %+v", link)
	}
	if string(file.Data) != keyWeb01+"\n"+keyWeb02+"\n"+keyAlice+"\n" || file.Mode.Perm() != pve.KeysPerm {
		t.Errorf("%s = %+v", pve.ClusterKeys, file)
	}
	for _, w := range writes(h, before) {
		if strings.HasSuffix(w, pve.RootKeys) {
			t.Errorf("wrote at the link: %s", w)
		}
	}
	if ws := writes(h, before); ws[len(ws)-1] != "write-atomic "+pve.ClusterKeys {
		t.Errorf("last write = %q, want the cluster file", ws)
	}
	// The backup restores through the link too.
	m, err := backup.Load(ctx, h, run.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Restore(ctx, h, m); err != nil {
		t.Fatal(err)
	}
	if file, _ := h.Entry(pve.ClusterKeys); string(file.Data) != keyWeb01+"\n"+keyWeb02+"\n" {
		t.Errorf("restored = %q", file.Data)
	}
	if link, _ := h.Entry(pve.RootKeys); link.Target != pve.ClusterKeys {
		t.Errorf("the restore changed the link: %+v", link)
	}
}

func TestApplyKeysRefuses(t *testing.T) {
	h := pve9(t)
	k, _ := pve.ReadKeys(ctx, h, pveUp)
	p, _ := pve.PlanKeys(k, pve.KeyEdit{Add: []string{keyAlice}})
	before := len(h.Calls())
	if _, err := pve.ApplyKeys(ctx, h, pveUp, nil, p); !errors.Is(err, pve.ErrNoBackup) {
		t.Errorf("no run: %v", err)
	}
	if res, err := pve.ApplyKeys(ctx, h, pveUp, openRun(t, h), pve.KeysPlan{}); err != nil || res.Entry.Path != "" {
		t.Errorf("no change: %+v, %v", res, err)
	}
	// A plan changed or lost on its way: its New is checked against its edit
	// on the keys as they are, with PlanKeys' guards.
	lost := p
	lost.New = nil
	edited := p
	edited.Added = []string{keyBob}
	nodes := pve.KeysPlan{Target: k.Target, Before: sha256.Sum256(k.Data), Old: k.Data, Removed: []string{keyWeb01, keyWeb02}}
	bad := pve.KeysPlan{Target: k.Target, Before: sha256.Sum256(k.Data), Added: []string{"ssh-ed25519 bob"}}
	for _, tt := range []struct {
		name string
		p    pve.KeysPlan
		err  error
	}{{"New lost", lost, pve.ErrPlanMismatch}, {"Added edited", edited, pve.ErrPlanMismatch},
		{"the node keys removed", nodes, pve.ErrClusterKey}, {"not a key", bad, pve.ErrBadKey}} {
		if _, err := pve.ApplyKeys(ctx, h, pveUp, openRun(t, h), tt.p); !errors.Is(err, tt.err) {
			t.Errorf("%s: %v, want %v", tt.name, err, tt.err)
		}
	}
	// Another node adds bob's key after the plan.
	h.AddFile(pve.ClusterKeys, []byte(string(k.Data)+keyBob+"\n"), 0o600, 0, wwwData)
	if _, err := pve.ApplyKeys(ctx, h, pveUp, openRun(t, h), p); !errors.Is(err, pve.ErrKeysChanged) {
		t.Errorf("changed meanwhile: %v", err)
	}
	if _, err := pve.ApplyKeys(ctx, h, pveDown, openRun(t, h), p); !errors.Is(err, pve.ErrClusterFSDown) {
		t.Errorf("pmxcfs down: %v", err)
	}
	if ws := writes(h, before); len(ws) != 0 {
		t.Errorf("wrote %q", ws)
	}
}

// TestApplyKeysFails: a backup that fails stops the write; a write that
// fails (pmxcfs read-only without quorum) leaves the file as it was and
// returns the backup taken.
func TestApplyKeysFails(t *testing.T) {
	for _, tt := range []struct {
		name, path string
		err        error
		backedUp   bool
	}{{"the backup", backup.DefaultPath, syscall.ENOSPC, false}, {"the write", pve.ClusterKeys, syscall.EROFS, true}} {
		f := &failing{System: pve9(t)}
		k, _ := pve.ReadKeys(ctx, f, pveUp)
		p, _ := pve.PlanKeys(k, pve.KeyEdit{Add: []string{keyAlice}})
		run := openRun(t, f)
		f.op, f.path, f.err = "write", tt.path, tt.err
		res, err := pve.ApplyKeys(ctx, f, pveUp, run, p)
		if !errors.Is(err, tt.err) || (res.Entry.Path == pve.RootKeys) != tt.backedUp {
			t.Errorf("%s failing: %+v, %v", tt.name, res, err)
		}
		if file, _ := f.Entry(pve.ClusterKeys); !slices.Equal(file.Data, k.Data) {
			t.Errorf("%s failing: %s = %q", tt.name, pve.ClusterKeys, file.Data)
		}
	}
}

// TestUndoKeys: the undo removes what the edit added and puts back what it
// removed, keeping a key another node wrote in between, which restoring the
// whole file would drop.
func TestUndoKeys(t *testing.T) {
	h := pve9(t)
	k, _ := pve.ReadKeys(ctx, h, pveUp)
	add, _ := pve.PlanKeys(k, pve.KeyEdit{Add: []string{keyAlice}})
	if _, err := pve.ApplyKeys(ctx, h, pveUp, openRun(t, h), add); err != nil {
		t.Fatal(err)
	}
	cur, _ := h.Entry(pve.ClusterKeys)
	h.AddFile(pve.ClusterKeys, append(cur.Data, keyBob+"\n"...), 0o600, 0, wwwData)
	if err := pve.UndoKeys(ctx, h, pveUp, add); err != nil {
		t.Fatal(err)
	}
	if f, _ := h.Entry(pve.ClusterKeys); string(f.Data) != keyWeb01+"\n"+keyWeb02+"\n"+keyBob+"\n" {
		t.Errorf("after undo = %q", f.Data)
	}
	before := len(h.Calls())
	if err := pve.UndoKeys(ctx, h, pveUp, add); err != nil || len(writes(h, before)) != 0 {
		t.Errorf("a second undo: %v, wrote %q", err, writes(h, before))
	}

	k, _ = pve.ReadKeys(ctx, h, pveUp)
	rm, err := pve.PlanKeys(k, pve.KeyEdit{Remove: []string{keyBob}})
	if err != nil || !rm.Narrows() {
		t.Fatalf("PlanKeys = %+v, %v", rm, err)
	}
	if _, err := pve.ApplyKeys(ctx, h, pveUp, openRun(t, h), rm); err != nil {
		t.Fatal(err)
	}
	if err := pve.UndoKeys(ctx, h, pveUp, rm); err != nil {
		t.Fatal(err)
	}
	if f, _ := h.Entry(pve.ClusterKeys); string(f.Data) != keyWeb01+"\n"+keyWeb02+"\n"+keyBob+"\n" {
		t.Errorf("after undoing the removal = %q", f.Data)
	}
	if link, _ := h.Entry(pve.RootKeys); link.Target != pve.ClusterKeys {
		t.Errorf("the link is now %+v", link)
	}
	if err := pve.UndoKeys(ctx, h, pveDown, rm); !errors.Is(err, pve.ErrClusterFSDown) {
		t.Errorf("undo with pmxcfs down: %v", err)
	}
	// A write that fails is the undo's error.
	f := &failing{System: h, op: "write", path: pve.ClusterKeys, err: syscall.EROFS}
	if _, err := pve.ApplyKeys(ctx, h, pveUp, openRun(t, h), rm); err != nil {
		t.Fatal(err)
	}
	if err := pve.UndoKeys(ctx, f, pveUp, rm); !errors.Is(err, syscall.EROFS) {
		t.Errorf("undo, the write failing: %v", err)
	}
}

// TestUndoKeysExact: a key removed from several lines gets every line back,
// and an undo before the apply writes nothing.
func TestUndoKeysExact(t *testing.T) {
	h := pve9(t)
	restricted := `from="192.0.2.0/24" ` + keyBob
	h.AddFile(pve.ClusterKeys, []byte(keyWeb01+"\n"+keyWeb02+"\n"+keyBob+"\n"+restricted+"\n"), 0o600, 0, wwwData)
	k, _ := pve.ReadKeys(ctx, h, pveUp)
	rm, err := pve.PlanKeys(k, pve.KeyEdit{Remove: []string{keyBob}})
	if err != nil || !slices.Equal(rm.Removed, []string{keyBob, restricted}) {
		t.Fatalf("PlanKeys = %+v, %v", rm, err)
	}
	before := len(h.Calls())
	if err := pve.UndoKeys(ctx, h, pveUp, rm); err != nil || len(writes(h, before)) != 0 {
		t.Errorf("undo before the apply: %v, wrote %q", err, writes(h, before))
	}
	if _, err := pve.ApplyKeys(ctx, h, pveUp, openRun(t, h), rm); err != nil {
		t.Fatal(err)
	}
	if err := pve.UndoKeys(ctx, h, pveUp, rm); err != nil {
		t.Fatal(err)
	}
	if f, _ := h.Entry(pve.ClusterKeys); string(f.Data) != string(k.Data) {
		t.Errorf("after undo = %q, want %q", f.Data, k.Data)
	}
}

// TestRollbackKeys: a KeysPlan survives p2-revert's journal (JSON), and
// RollbackKeys undoes the decoded plan, detecting Proxmox VE itself.
func TestRollbackKeys(t *testing.T) {
	h := pve9(t)
	h.Respond(hostfake.Response{Stdout: "install ok installed\t9.2.22\n"}, "dpkg-query", "--show", `--showformat=${Status}\t${Version}\n`, "pve-manager")
	h.AddFile("/usr/bin/pveversion", []byte("#!/usr/bin/perl\n"), 0o755, 0, 0)
	h.AddFile("/proc/self/mounts", []byte("/dev/fuse /etc/pve fuse rw,nosuid,nodev,relatime,user_id=0,group_id=0,default_permissions,allow_other 0 0\n"), 0o444, 0, 0)
	k, _ := pve.ReadKeys(ctx, h, pveUp)
	p, err := pve.PlanKeys(k, pve.KeyEdit{Add: []string{keyAlice, keyBob}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var back pve.KeysPlan
	if err := json.Unmarshal(data, &back); err != nil || !reflect.DeepEqual(back, p) {
		t.Fatalf("round trip = %+v, %v; want %+v", back, err, p)
	}
	before := len(h.Calls())
	if err := pve.RollbackKeys(ctx, h, back); err != nil || len(writes(h, before)) != 0 {
		t.Errorf("rollback before the apply: %v, wrote %q", err, writes(h, before))
	}
	if _, err := pve.ApplyKeys(ctx, h, pveUp, openRun(t, h), p); err != nil {
		t.Fatal(err)
	}
	if err := pve.RollbackKeys(context.WithoutCancel(ctx), h, back); err != nil {
		t.Fatal(err)
	}
	if f, _ := h.Entry(pve.ClusterKeys); string(f.Data) != string(k.Data) {
		t.Errorf("after the rollback = %q", f.Data)
	}
	// Off Proxmox VE, as detected then, it changes nothing.
	off := hostfake.New(host.Options{})
	off.Respond(hostfake.Response{ExitCode: 1}, "dpkg-query", "--show", `--showformat=${Status}\t${Version}\n`, "pve-manager")
	off.AddFile("/proc/self/mounts", nil, 0o444, 0, 0)
	if err := pve.RollbackKeys(ctx, off, back); !errors.Is(err, pve.ErrNotApplicable) {
		t.Errorf("off Proxmox VE: %v", err)
	}
}

// TestKeysPlanCheck: a plan PlanKeys made passes Check, a removal and an
// addition; each field a journal could carry edited by hand is refused
// (ErrPlanMismatch, or ErrBadKey for a line that is not a key), and
// RollbackKeys then reads and writes nothing.
func TestKeysPlanCheck(t *testing.T) {
	h := pve9(t)
	k, err := pve.ReadKeys(ctx, h, pveUp)
	if err != nil {
		t.Fatal(err)
	}
	k.Data = []byte(string(k.Data) + keyAlice + "\n")
	add, err := pve.PlanKeys(k, pve.KeyEdit{Add: []string{keyBob}})
	if err != nil {
		t.Fatal(err)
	}
	remove, err := pve.PlanKeys(k, pve.KeyEdit{Remove: []string{keyAlice}})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []pve.KeysPlan{add, remove} {
		if err := p.Check(); err != nil {
			t.Fatalf("Check of %+v = %v", p, err)
		}
	}
	for _, tc := range []struct {
		name string
		base pve.KeysPlan
		edit func(*pve.KeysPlan)
		want error
	}{
		{"Before of other content", add, func(p *pve.KeysPlan) { p.Before = sha256.Sum256([]byte(keyWeb01 + "\n")) }, pve.ErrPlanMismatch},
		{"a node's key added to Added", add, func(p *pve.KeysPlan) { p.Added = append(p.Added, keyWeb02) }, pve.ErrPlanMismatch},
		{"New not the edit", add, func(p *pve.KeysPlan) { p.New = []byte(keyBob + "\n") }, pve.ErrPlanMismatch},
		{"Removed a key Old does not hold", remove, func(p *pve.KeysPlan) { p.Removed = []string{keyBob} }, pve.ErrPlanMismatch},
		{"Old without the key Removed names", remove, func(p *pve.KeysPlan) {
			p.Old = []byte(keyWeb01 + "\n" + keyWeb02 + "\n")
			p.Before = sha256.Sum256(p.Old)
		}, pve.ErrPlanMismatch},
		{"a line that is not a key", add, func(p *pve.KeysPlan) { p.Added = []string{"command=\"/bin/sh\""} }, pve.ErrBadKey},
		{"the zero plan", pve.KeysPlan{}, func(*pve.KeysPlan) {}, pve.ErrPlanMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.base
			p.Added, p.Removed = slices.Clone(p.Added), slices.Clone(p.Removed)
			tc.edit(&p)
			if err := p.Check(); !errors.Is(err, tc.want) {
				t.Errorf("Check = %v, want %v", err, tc.want)
			}
			before := len(h.Calls())
			if err := pve.RollbackKeys(ctx, h, p); !errors.Is(err, tc.want) || len(h.Calls()) != before {
				t.Errorf("RollbackKeys = %v, calls %q; want refused with nothing read", err, h.Calls()[before:])
			}
		})
	}
}

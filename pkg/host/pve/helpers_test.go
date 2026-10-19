package pve_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// wwwData is the group pmxcfs reports for everything under /etc/pve.
const wwwData = 33

// Synthetic keys (made-up bytes in the SSH public key format): each node's
// root key, as testdata/pve9 has them, and alice's and bob's.
const (
	keyWeb01 = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAAQQCRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGR root@web-01"
	keyWeb02 = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAAQQCSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKS root@web-02"
	keyAlice = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGh alice@web-01.example.net"
	keyBob   = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILCwsLCwsLCwsLCwsLCwsLCwsLCwsLCwsLCwsLCwsLCw bob@web-01.example.net"
)

// Proxmox facts as detect.DetectProxmox gives them.
var (
	pveUp   = detect.Proxmox{Installed: detect.Yes, Version: "9.2.22", ClusterFS: detect.Yes, ClusterFSType: "fuse", EtcPVE: detect.Yes}
	pveDown = detect.Proxmox{Installed: detect.Yes, Version: "9.2.22", ClusterFS: detect.No, EtcPVE: detect.Yes}
	debian  = detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No}
	unknown = detect.Proxmox{Installed: detect.Unknown, Err: detect.ErrConflict}
)

// pve9 is a fake Proxmox VE 9 node web-01 holding testdata/pve9/root, with
// the modes and owners pmxcfs reports (pmxcfs.c, cfs_fuse_getattr: root,
// www-data, 0700 and 0600 under priv/, 0755 and 0640 elsewhere) and the
// links Proxmox VE keeps.
func pve9(t *testing.T) *hostfake.System {
	t.Helper()
	h := hostfake.New(host.Options{})
	h.AddDir("/root", 0o700, 0, 0)
	h.AddDir("/root/.ssh", 0o700, 0, 0)
	root := filepath.Join("testdata", "pve9", "root")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == root {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		name := "/" + filepath.ToSlash(rel)
		pmx, priv := strings.HasPrefix(name, "/etc/pve"), strings.HasPrefix(name, "/etc/pve/priv")
		switch {
		case d.IsDir() && priv:
			h.AddDir(name, 0o700, 0, wwwData)
		case d.IsDir() && pmx:
			h.AddDir(name, 0o755, 0, wwwData)
		case d.IsDir():
			h.AddDir(name, 0o755, 0, 0)
		default:
			data, err := os.ReadFile(p)
			mode, gid := fs.FileMode(0o644), 0
			switch {
			case priv:
				mode, gid = 0o600, wwwData
			case pmx:
				mode, gid = 0o640, wwwData
			}
			h.AddFile(name, data, mode, 0, gid)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h.AddSymlink("/root/.ssh/authorized_keys", "/etc/pve/priv/authorized_keys")
	h.AddSymlink("/etc/pve/local", "nodes/web-01")
	return h
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "pve9", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// checkReadOnly fails on any call that changes the fake, from the call
// numbered from on (0: all).
func checkReadOnly(t *testing.T, h *hostfake.System, from ...int) {
	t.Helper()
	calls := h.Calls()
	if len(from) > 0 {
		calls = calls[from[0]:]
	}
	for _, c := range calls {
		switch c.Op {
		case "write-atomic", "write-in-place", "mkdir", "remove", "removeall", "rename", "chmod", "chown", "start-detached":
			t.Errorf("changed the host: %s", c)
		}
	}
}

// failing is the fake with one operation failing with err: op ("write",
// "read", "read-nofollow", "readdir") on path or below it. Set op after
// the setup it must not break.
type failing struct {
	*hostfake.System
	op, path string
	err      error
}

func (f *failing) fail(op, name string) error {
	if op == f.op && (name == f.path || strings.HasPrefix(name, f.path+"/")) {
		return &fs.PathError{Op: op, Path: name, Err: f.err}
	}
	return nil
}

func (f *failing) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	if err := f.fail("write", name); err != nil {
		return err
	}
	return f.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

func (f *failing) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if err := f.fail("read", name); err != nil {
		return nil, err
	}
	return f.System.ReadFile(ctx, name)
}

func (f *failing) ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error) {
	if err := f.fail("read-nofollow", name); err != nil {
		return nil, nil, err
	}
	return f.System.ReadFileNoFollow(ctx, name)
}

func (f *failing) ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error) {
	if err := f.fail("readdir", name); err != nil {
		return nil, err
	}
	return f.System.ReadDir(ctx, name)
}

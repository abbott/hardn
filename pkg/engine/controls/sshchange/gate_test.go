package sshchange_test

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// TestGate holds what unit (b)'s mutation run found no test holding: sshd
// -T failing another way than /run/sshd, and a legacy file that can't be
// read, refused as not read; DenyGroups alone; StrictModes no, under which
// a group-writable home doesn't stop a key; a user who isn't an admin
// not counted; and Disallow root login's
// proof staying key-only where passwords are off globally, though a Match
// block lets the only admin in by password from the session's address.
func TestGate(t *testing.T) {
	ctx := context.Background()
	admin := func(t *testing.T, v *vm, name string) sshchange.Admin {
		for _, a := range read(t, v, sudoGeorge).LoginPaths(ctx, v.sys(), true) {
			if a.Name == name {
				return a
			}
		}
		return sshchange.Admin{}
	}
	notRead := func(t *testing.T, v *vm) {
		t.Helper()
		_, p := planOn(t, v, disallow)
		if rf := refusalOf(t, p); rf.Code != "SSH_SETTINGS_NOT_READ" {
			t.Errorf("code %s, want SSH_SETTINGS_NOT_READ", rf.Code)
		}
	}
	t.Run("sshd -T fails", func(t *testing.T) {
		v := debianVM(t)
		v.sshdFail = "/etc/ssh/sshd_config line 3: Bad configuration option: Bogus\r\n"
		notRead(t, v)
	})
	t.Run("a legacy file can't be read", func(t *testing.T) {
		v := debianVM(t).file(sshd.DropInDir+"/manage.conf", "# SSH configuration managed by Hardn\nPort 22\n")
		v.noFollowErr = map[string]error{sshd.DropInDir + "/manage.conf": fs.ErrPermission}
		notRead(t, v)
	})
	t.Run("DenyGroups", func(t *testing.T) {
		v := debianVM(t).file(sshd.DropInDir+"/10-site.conf", "DenyGroups adm\n")
		if a := admin(t, v, "george"); a.Key || a.Why != "AllowGroups and DenyGroups aren't read" {
			t.Errorf("george: %+v, want not able, groups not read", a)
		}
	})
	t.Run("StrictModes no", func(t *testing.T) {
		v := debianVM(t)
		v.f.AddDir("/home/george", 0o775, 1000, 1000)
		if a := admin(t, v, "george"); a.Key {
			t.Errorf("StrictModes yes, home 0775: %+v, want not able", a)
		}
		v.file(sshd.DropInDir+"/10-site.conf", "StrictModes no\n")
		if a := admin(t, v, "george"); !a.Key {
			t.Errorf("StrictModes no, home 0775: %+v, want able", a)
		}
	})
	t.Run("a user who isn't an admin, with a key, doesn't count", func(t *testing.T) {
		// carol has a key and a password, so only being no admin keeps her
		// from counting (the becoming-root rule would let a password by).
		carol := account{name: "carol", uid: 1002, pw: hashSet, keys: []string{key(3, "carol@laptop-3")}}
		v := newVM(t, debian13(), false, george, carol)
		data, _ := v.f.ReadFile(ctx, "/etc/group")
		v.f.AddFile("/etc/group", []byte(strings.Replace(string(data), "sudo:x:27:george,carol", "sudo:x:27:", 1)), 0o644, 0, 0)
		_, p := planOn(t, v, disallow)
		if rf := refusalOf(t, p); rf.Code != "SSH_NO_ADMIN_LOGIN" {
			t.Errorf("code %s, want SSH_NO_ADMIN_LOGIN: carol and george can log in by key but neither is an admin", rf.Code)
		}
	})
	t.Run("a Match block lets the only admin in by password", func(t *testing.T) {
		v := newVM(t, debian13(), true, alice).file(sshd.DropInDir+"/20-site.conf", "Match Address 192.0.2.0/24\n    PasswordAuthentication yes\n")
		v.log = ""
		v.logIn("password", "alice", "192.0.2.10")
		_, p := planOn(t, v, disallow)
		if rf := refusalOf(t, p); rf.Code != "SSH_NO_ADMIN_LOGIN" {
			t.Errorf("code %s, want SSH_NO_ADMIN_LOGIN: the proof is by key where passwords are off globally", rf.Code)
		}
	})
}

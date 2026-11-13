package authkeys_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/authkeys"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// Synthetic keys Read leaves out, built here, no one's: a DSA key (four
// numbers) and a 768-bit RSA key.
var (
	dsaKey   = lineOf(authkeys.DSA, blobOf(authkeys.DSA, filled(0, 0xc5, 129), filled(0, 0xc5, 21), filled(0, 0xc5, 128), filled(0, 0xc5, 128)))
	rsa768   = lineOf("ssh-rsa", blobOf("ssh-rsa", []byte{1, 0, 1}, filled(0, 0xc5, 97)))
	dsaShort = lineOf(authkeys.DSA, blobOf(authkeys.DSA, filled(0, 0xc5, 129)))
)

// TestReview: every key in the file's order, each once, a DSA key and an RSA
// key under 1024 bits among them, with options read past; a line that is no
// key is counted, a blank line and a comment are not; Read still leaves the
// two kinds out, and ParseLine refuses them.
func TestReview(t *testing.T) {
	data := "# george\n" + edKey + "\n\n" + dsaKey + "\r\n" + `from="192.0.2.1",command="echo ssh-dss" ` + rsa768 + "\n" + edKey + "\n" +
		"not a key\n" + dsaShort + "\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5 george@old\n" + `no-pty ` + dsaKey + "\n\xff\xfe\x00\n"
	h := keyHost(true, map[string]string{keysFile: data})
	files := authkeys.Read(ctx, h, alpine, george)
	if len(files) != 1 || len(files[0].Keys) != 2 {
		t.Fatalf("Read = %+v", files)
	}
	keys, bad := authkeys.Review(files[0])
	var got []string
	for _, k := range keys {
		got = append(got, k.Type+" "+k.Comment)
		if k.Fingerprint == "" || k.Line == "" {
			t.Errorf("%+v", k)
		}
	}
	if !slices.Equal(got, []string{"ssh-ed25519 george@web-01", "ssh-dss carol@laptop", "ssh-rsa carol@laptop"}) || bad != 4 {
		t.Errorf("Review = %q, %d bad", got, bad)
	}
	if keys[2].RSABits() != 768 || keys[2].Options == "" {
		t.Errorf("the RSA key: %d bits, options %q", keys[2].RSABits(), keys[2].Options)
	}
	for _, l := range []string{dsaKey, rsa768} {
		if _, err := authkeys.ParseLine(l); !errors.Is(err, authkeys.ErrNotKey) {
			t.Errorf("ParseLine(%.20s) = %v", l, err)
		}
		if _, err := authkeys.ParsePublic(l); !errors.Is(err, authkeys.ErrNotKey) {
			t.Errorf("ParsePublic(%.20s) = %v", l, err)
		}
	}
	if keys, bad := authkeys.Review(authkeys.File{Path: keysFile}); keys != nil || bad != 0 {
		t.Errorf("Review, not there = %v, %d", keys, bad)
	}
	if keys, bad := authkeys.Review(authkeys.File{Path: keysFile, Err: fs.ErrPermission}); keys != nil || bad != 0 {
		t.Errorf("Review, not read = %v, %d", keys, bad)
	}
}

// accountsHost is a host with passwd and what add puts on it.
func accountsHost(passwd string, add func(h *hostfake.System)) *hostfake.System {
	h := hostfake.New(host.Options{})
	h.AddDir("/etc", 0o755, 0, 0)
	if passwd != "" {
		h.AddFile(accounts.PasswdFile, []byte(passwd), 0o644, 0, 0)
	}
	if add != nil {
		add(h)
	}
	return h
}

// TestReadAccounts: every account of passwd, system and nologin accounts too,
// the first line of each name, each with the files sshd reads for it: read,
// not there, or not read (a directory in its place); an account with no home
// has no file read; "none" names none. It runs nothing and writes nothing.
func TestReadAccounts(t *testing.T) {
	passwd := "root:x:0:0:root:/root:/bin/bash\n" +
		"daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin\n" +
		"george:x:1000:1000::/home/george:/bin/bash\n" +
		"alice:x:1001:1001::/home/alice:/bin/bash\n" +
		"george:x:1002:1002::/home/other:/bin/bash\n" +
		"nohome:x:1003:1003:::/bin/sh\n"
	h := accountsHost(passwd, func(h *hostfake.System) {
		h.AddDir("/usr/sbin/.ssh", 0o700, 1, 1)
		h.AddFile("/usr/sbin/.ssh/authorized_keys", []byte(edKey+"\n"), 0o600, 1, 1)
		h.AddDir("/home/george/.ssh", 0o700, 1000, 1000)
		h.AddFile(keysFile, []byte(rsaKey+"\n"), 0o600, 1000, 1000)
		h.AddFile(keys2File, []byte(dsaKey+"\n"), 0o600, 1000, 1000)
		h.AddDir("/home/alice/.ssh/authorized_keys", 0o700, 1001, 1001)
	})
	got, err := authkeys.ReadAccounts(ctx, h, append([]string{"none"}, debian...))
	if err != nil || len(got) != 5 {
		t.Fatalf("ReadAccounts = %+v, %v", got, err)
	}
	var names []string
	for _, a := range got {
		names = append(names, a.User.Name)
		if len(a.Files) != 2 {
			t.Errorf("%s: %d files", a.User.Name, len(a.Files))
		}
	}
	if !slices.Equal(names, []string{"root", "daemon", "george", "alice", "nohome"}) {
		t.Errorf("accounts %q", names)
	}
	if f := got[0].Files[0]; f.Data != nil || f.Err != nil || f.Path != "/root/.ssh/authorized_keys" {
		t.Errorf("root: %+v", f)
	}
	if f := got[1].Files[0]; len(f.Keys) != 1 || f.Keys[0].Fingerprint != edFP {
		t.Errorf("daemon: %+v", f)
	}
	if g := got[2]; g.User.UID != 1000 || len(g.Files[0].Keys) != 1 || len(g.Files[1].Keys) != 0 || g.Files[1].Data == nil {
		t.Errorf("george: %+v", g)
	}
	if f := got[3].Files; f[0].Err == nil || f[0].Data != nil || f[1].Err != nil {
		t.Errorf("alice: %+v", f)
	}
	if f := got[4].Files; f[0].Err == nil || f[0].Data != nil || f[0].Path != ".ssh/authorized_keys" {
		t.Errorf("nohome: %+v", f)
	}
	for _, c := range h.Calls() {
		if c.Op != "read" && c.Op != "scan-lines" || c.Path == ".ssh/authorized_keys" {
			t.Errorf("call %s", c)
		}
	}
	if got, err := authkeys.ReadAccounts(ctx, h, []string{"none"}); err != nil || len(got) != 5 || len(got[2].Files) != 0 {
		t.Errorf("none: %+v, %v", got, err)
	}
}

// TestReadAccountsErrors: no passwd, a passwd line not in its form, and a
// context that ended, are errors, never an empty list.
func TestReadAccountsErrors(t *testing.T) {
	if got, err := authkeys.ReadAccounts(ctx, accountsHost("", nil), alpine); !errors.Is(err, fs.ErrNotExist) || got != nil {
		t.Errorf("no passwd: %+v, %v", got, err)
	}
	if got, err := authkeys.ReadAccounts(ctx, accountsHost("root:x:0:0:root:/root:/bin/bash\nnot a line\n", nil), alpine); !errors.Is(err, accounts.ErrMalformed) || got != nil {
		t.Errorf("malformed: %+v, %v", got, err)
	}
	done, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := authkeys.ReadAccounts(done, accountsHost("root:x:0:0:root:/root:/bin/bash\n", nil), alpine); err == nil || got != nil {
		t.Errorf("cancelled: %+v, %v", got, err)
	}
}

// TestReadAccountsGuarded: no account's file can hold the read or fill
// memory. A symlink is followed from a directory that is root's alone
// (Proxmox VE's root), and refused in one the account owns, its target never
// opened; a file past MaxFile, a line past host.MaxLine and a link to itself
// are not read. Each is a File with an error, never one with no key.
func TestReadAccountsGuarded(t *testing.T) {
	passwd := "root:x:0:0:root:/root:/bin/bash\n" +
		"george:x:1000:1000::/home/george:/bin/bash\n" +
		"alice:x:1001:1001::/home/alice:/bin/bash\n" +
		"bob:x:1002:1002::/home/bob:/bin/bash\n" +
		"loop:x:1003:1003::/srv/loop:/bin/bash\n" +
		"carol:x:1004:1004::/home/carol:/bin/bash\n"
	const secret = "/etc/secret"
	h := accountsHost(passwd, func(h *hostfake.System) {
		for _, d := range []string{"/root", "/root/.ssh", "/etc/pve", "/etc/pve/priv", "/home", "/srv", "/srv/loop", "/srv/loop/.ssh"} {
			h.AddDir(d, 0o755, 0, 0)
		}
		h.AddFile("/etc/pve/priv/authorized_keys", []byte(edKey), 0o600, 0, 0)
		h.AddSymlink("/root/.ssh/authorized_keys", "/etc/pve/priv/authorized_keys")
		h.AddFile(secret, []byte(rsaKey+"\n"), 0o600, 0, 0)
		h.AddDir("/home/george", 0o700, 1000, 1000)
		h.AddDir("/home/george/.ssh", 0o700, 1000, 1000)
		h.AddSymlink(keysFile, secret)
		h.AddDir("/home/alice/.ssh", 0o700, 1001, 1001)
		h.AddFile("/home/alice/.ssh/authorized_keys", []byte(strings.Repeat(edKey+"\n", authkeys.MaxFile/len(edKey))), 0o600, 1001, 1001)
		h.AddDir("/home/bob/.ssh", 0o700, 1002, 1002)
		h.AddFile("/home/bob/.ssh/authorized_keys", []byte(edKey+"\n"+strings.Repeat("A", host.MaxLine+1)+"\n"), 0o600, 1002, 1002)
		h.AddSymlink("/srv/loop/.ssh/authorized_keys", "authorized_keys")
		h.AddDir("/home/carol/.ssh", 0o700, 1004, 1004)
		h.AddFile("/home/carol/.ssh/authorized_keys", []byte(strings.Repeat(edKey+"\n", authkeys.MaxFile/(len(edKey)+1))), 0o600, 1004, 1004)
	})
	got, err := authkeys.ReadAccounts(ctx, h, alpine)
	if err != nil || len(got) != 6 {
		t.Fatalf("ReadAccounts = %+v, %v", got, err)
	}
	if f := got[0].Files[0]; f.Err != nil || len(f.Keys) != 1 || f.Keys[0].Fingerprint != edFP || f.Path != "/root/.ssh/authorized_keys" {
		t.Errorf("root, a link in root's directory: %+v", f)
	}
	if f := got[1].Files[0]; !errors.Is(f.Err, host.ErrSymlinkParent) || f.Data != nil || f.Keys != nil {
		t.Errorf("george, a link in his own directory: %+v", f)
	}
	for i, name := range map[int]string{2: "alice, past MaxFile", 3: "bob, a line past MaxLine", 4: "loop, a link to itself"} {
		if f := got[i].Files[0]; f.Err == nil || f.Data != nil || f.Keys != nil {
			t.Errorf("%s: err %v, %d keys", name, f.Err, len(f.Keys))
		}
	}
	if f := got[5].Files[0]; f.Err != nil || len(f.Keys) != authkeys.MaxFile/(len(edKey)+1) {
		t.Errorf("carol, under MaxFile: err %v, %d keys", f.Err, len(f.Keys))
	}
	for _, c := range h.Calls() {
		if c.Path == secret || c.Op != "read" && c.Op != "scan-lines" && c.Op != "resolve-symlink" {
			t.Errorf("call %s", c)
		}
	}
}

// TestReadGuardedFIFO: on this machine's file system, a FIFO where the key
// file would be is not opened: the read returns at once with an error, where
// a plain read would wait for a writer for ever. A regular file is read.
func TestReadGuardedFIFO(t *testing.T) {
	dir := t.TempDir()
	fifo, file := filepath.Join(dir, "fifo"), filepath.Join(dir, "authorized_keys")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	if err := os.WriteFile(file, []byte(edKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sys := host.NewReal(host.Options{})
	done := make(chan authkeys.File, 1)
	go func() { done <- authkeys.ReadGuarded(ctx, sys, fifo) }()
	select {
	case f := <-done:
		if !errors.Is(f.Err, fs.ErrInvalid) || f.Data != nil {
			t.Errorf("a FIFO: %+v", f)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the read of a FIFO did not return")
	}
	if f := authkeys.ReadGuarded(ctx, sys, file); f.Err != nil || len(f.Keys) != 1 {
		t.Errorf("a regular file: %+v", f)
	}
}

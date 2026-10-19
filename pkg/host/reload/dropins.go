package reload

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// dropInDirs are where an ssh.socket drop-in can be: the directories of
// systemd's system unit load path (systemd.unit(5), v257) that hold
// configuration made by hand, at run time or by a package, each with
// ssh.socket.d. The generator directories are left out: the
// sshd-socket-generator's own addresses.conf is there.
var dropInDirs = []string{
	"/etc/systemd/system.control",
	"/run/systemd/system.control",
	"/run/systemd/transient",
	"/etc/systemd/system",
	"/run/systemd/system",
	"/usr/local/lib/systemd/system",
	"/usr/lib/systemd/system",
}

// SocketDropIns is ReadSocketDropIns's answer.
type SocketDropIns struct {
	Read  bool // every directory was read
	Files []SocketDropIn
}

// SocketDropIn is a drop-in of ssh.socket's that hardn did not write, such
// as v0.3.x's listen.conf. It is reported, never edited here
// (p2-sshd-migrate owns listen.conf).
type SocketDropIn struct {
	Path string
	// Listen is the file's ListenStream values in its [Socket] section, in
	// order; "" is the empty assignment, which resets the list. A drop-in
	// with any decides or changes the socket's listeners whatever
	// sshd_config says: v0.3.x's listen.conf is "ListenStream=" then
	// "ListenStream=<port>".
	Listen []string
	// Masked: a symlink to /dev/null, which masks a drop-in of its name in
	// a later directory.
	Masked bool
}

// ReadSocketDropIns lists the *.conf files of ssh.socket.d in every
// non-generator unit directory, in directory order and then by name, and
// reads their ListenStream lines. It writes nothing. A missing directory
// holds none; any other error fails it, since a drop-in it cannot read may
// move the listeners.
func ReadSocketDropIns(ctx context.Context, sys host.System) (SocketDropIns, error) {
	var d SocketDropIns
	for _, dir := range dropInDirs {
		dir += "/" + SocketUnit + ".d"
		entries, err := sys.ReadDir(ctx, dir)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return SocketDropIns{}, fmt.Errorf("reload: %w", err)
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".conf") || e.IsDir() {
				continue
			}
			f, err := readDropIn(ctx, sys, path.Join(dir, e.Name()), e.Type()&fs.ModeSymlink != 0)
			if err != nil {
				return SocketDropIns{}, fmt.Errorf("reload: %w", err)
			}
			d.Files = append(d.Files, f)
		}
	}
	d.Read = true
	return d, nil
}

func readDropIn(ctx context.Context, sys host.System, name string, link bool) (SocketDropIn, error) {
	f := SocketDropIn{Path: name}
	if link {
		target, err := sys.Readlink(ctx, name)
		if err != nil {
			return f, err
		}
		if f.Masked = target == "/dev/null"; f.Masked {
			return f, nil
		}
	}
	data, err := sys.ReadFile(ctx, name)
	if err != nil {
		return f, err
	}
	f.Listen = listenStreams(data)
	return f, nil
}

// listenStreams reads a unit file as systemd.syntax(7) says: lines starting
// with # or ; are comments, a line ending in a backslash continues on the
// next (comment lines between are skipped), "[Section]" starts a section.
func listenStreams(data []byte) []string {
	var out []string
	section, cont := "", ""
	for _, raw := range strings.Split(string(data), "\n") {
		l := strings.TrimSpace(raw)
		if l != "" && (l[0] == '#' || l[0] == ';') {
			continue
		}
		if s, ok := strings.CutSuffix(l, `\`); ok {
			cont += s + " "
			continue
		}
		l, cont = strings.TrimSpace(cont+l), ""
		switch {
		case strings.HasPrefix(l, "["):
			section = l
		case section == "[Socket]":
			if k, v, ok := strings.Cut(l, "="); ok && strings.TrimSpace(k) == "ListenStream" {
				out = append(out, strings.TrimSpace(v))
			}
		}
	}
	return out
}

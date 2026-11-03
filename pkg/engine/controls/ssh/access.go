package ssh

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// AccessLists are sshd's keywords that decide who may log in whatever their
// keys (sshd_config(5); auth.c allowed_user()), in its spelling.
var AccessLists = []string{"AllowUsers", "AllowGroups", "DenyUsers", "DenyGroups"}

// ReadAccessLists is which of AccessLists sshd's configuration sets, read as
// the SSH rows read it (sshd -T, then the files): sshd -T's lines, and, so
// that a block applying to some connections counts, any Match block's line
// in the files. hardn -c -d asks it (slice 5, unit (d)): with one set, a
// key does not show that the user -c creates can log in. An error where
// sshd -T or the files are not read.
func ReadAccessLists(ctx context.Context, sys host.System, f detect.Facts) ([]string, error) {
	r, err := read(ctx, sys, f)
	if err != nil {
		return nil, err
	}
	if r.eff.Keywords == nil {
		return nil, errors.New("sshd -T's output was not read")
	}
	lines := sshd.ReadOrder(r.files.Main, r.files.DropIns)
	var out []string
	for _, k := range AccessLists {
		lk := strings.ToLower(k)
		if len(r.eff.Keywords[lk]) > 0 || slices.ContainsFunc(lines, func(l sshd.Line) bool { return l.Keyword == lk }) {
			out = append(out, k)
		}
	}
	return out, nil
}

package alpine

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// World is apk's world file: the packages asked for, one per line or
// apart by spaces, each a name with an optional constraint or tag
// ("openssh>9", "openssh@edge"); "!name" asks that one never be installed.
const World = "/etc/apk/world"

// OpenSSH is Alpine's openssh meta package, which requires openssh-server
// (apk info -r openssh-server on the Alpine 3.23 VM, 2026-11-11).
const OpenSSH = "openssh"

// serverEntries are the World entries that keep an OpenSSH server
// installed: the meta package and the three server builds.
var serverEntries = []string{OpenSSH, OpenSSHServer, OpenSSHServerPAM, OpenSSHServerKrb5}

// ParseServerWorld returns the entries of World's content that keep an
// OpenSSH server installed (serverEntries), by name, in World's order: apk
// del must be given each to remove sshd. A package installed only as
// another's dependency is not removed by naming it: with openssh in World,
// apk del openssh-server updates World and removes nothing ("not removed
// due to: openssh-server: openssh", apk del --simulate on the Alpine 3.23
// VM, 2026-11-11).
func ParseServerWorld(data []byte) []string {
	var out []string
	for _, e := range strings.Fields(string(data)) {
		if strings.HasPrefix(e, "!") {
			continue
		}
		if i := strings.IndexAny(e, "<>=~@"); i >= 0 {
			e = e[:i]
		}
		if slices.Contains(serverEntries, e) && !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return out
}

// ServerWorld reads World and returns ParseServerWorld's entries. It
// changes nothing.
func ServerWorld(ctx context.Context, sys host.System) ([]string, error) {
	data, err := sys.ReadFile(ctx, World)
	if err != nil {
		return nil, fmt.Errorf("alpine: %w", err)
	}
	return ParseServerWorld(data), nil
}

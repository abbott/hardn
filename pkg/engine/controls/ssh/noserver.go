package ssh

import (
	"context"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// DetailNoServer is the SSH rows' detail where hardn establishes that the
// host has no SSH server (sshd.NoServer): Not Applicable, left out of the
// risk score as AppArmor in a container is (the maintainer, 2026-11-07).
const DetailNoServer = "no SSH server"

// unread is an SSH row's status where reading sshd failed with err: Not
// Applicable, DetailNoServer, nil error, where err says that no sshd can
// run here (noSSHD) and sshd.NoServer establishes that no SSH server is
// installed at all; failed's Unknown, err returned, otherwise. The three
// rows call it, and only they: ReadBypass keeps noSSHD's weaker rule.
func unread(ctx context.Context, sys host.System, f detect.Facts, err error) (engine.Status, error) {
	if noSSHD(err) {
		if ev, ok := sshd.NoServer(ctx, sys, f); ok {
			return engine.Status{State: engine.NotApplicable, Detail: DetailNoServer, Evidence: ev}, nil
		}
	}
	return failed(f.SSH, err)
}

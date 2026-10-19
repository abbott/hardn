package pve

import (
	"fmt"

	"github.com/abbott/hardn/pkg/host/detect"
)

// AccountOp is an account change that stops a password being used.
type AccountOp uint8

const (
	// OpLockPassword: passwd -l, usermod -L.
	OpLockPassword AccountOp = iota + 1
	// OpDeletePassword: passwd -d.
	OpDeletePassword
	// OpExpireAccount: usermod -e 1, chage -E 0.
	OpExpireAccount
)

func (o AccountOp) String() string {
	switch o {
	case OpLockPassword:
		return "lock the password of"
	case OpDeletePassword:
		return "delete the password of"
	case OpExpireAccount:
		return "expire"
	}
	return fmt.Sprintf("AccountOp(%d)", o)
}

// CheckAccount says whether op may be applied to user: ErrRootPassword for
// root on Proxmox VE, whose web UI logs in as root@pam with the password and
// whose cluster join asks for it, even with SSH key-only; ErrProxmoxUnknown
// for root when that is not established; nil otherwise. Changing root's
// password is not one of these ops, and is not refused.
func CheckAccount(p detect.Proxmox, user string, op AccountOp) error {
	if user != "root" {
		return nil
	}
	ok, err := Applies(p)
	switch {
	case err != nil:
		return err
	case ok:
		return fmt.Errorf("pve: %s root: %w", op, ErrRootPassword)
	}
	return nil
}

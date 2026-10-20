package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host/revert"
)

// ConfirmTimeout is the confirm window, Options.Timeout, from the
// --confirm-timeout flag (nil: not given) and cfg's confirmTimeout key
// (decision §3.14): each read with revert.ParseTimeout, the flag winning,
// the default 180 s, at least revert.MinTimeout (revert.ResolveTimeout).
// A value that is not one is revert.ErrTimeout. The wiring that builds
// Options (p3-output, or slice 6 with the first lockout-risk command)
// parses the flag and calls it (next/p3-steps).
func ConfirmTimeout(flag *time.Duration, cfg *config.Config) (time.Duration, error) {
	var key *time.Duration
	if cfg != nil && strings.TrimSpace(cfg.ConfirmTimeout) != "" {
		d, err := revert.ParseTimeout(cfg.ConfirmTimeout)
		if err != nil {
			return 0, fmt.Errorf("engine: confirmTimeout: %w", err)
		}
		key = &d
	}
	d, err := revert.ResolveTimeout(flag, key)
	if err != nil {
		return 0, fmt.Errorf("engine: %w", err)
	}
	return d, nil
}

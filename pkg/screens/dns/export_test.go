package dns

import (
	"context"

	"github.com/abbott/hardn/pkg/config"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// View is the screen's reading of the host and of hardn.yml with o, for
// the tests: what it draws, whole, where a row would cut it.
func View(ctx context.Context, o Options) screen.DNS { return (&runner{o: o}).read(ctx).view }

// AddCheck is Add nameserver's check, for the tests.
func AddCheck(configured []string, m hostdns.Manager) func(string) error {
	return addCheck(configured, m)
}

// Adopt is the adoption of edited, a change's copy of o.Config, after its
// apply, for the tests.
func Adopt(ctx context.Context, o Options, edited *config.Config) {
	(&runner{o: o}).adopt(ctx, edited)
}

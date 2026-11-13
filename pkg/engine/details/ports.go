package details

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/reload"
)

// SourceSS is ss, as Ports' Unread names it.
const SourceSS = "ss"

// Ports is the Listening Ports section (docs/engine/screen-ports.md): every
// listening socket, as ss lists it (reload.ReadListening: ss -Htlnp, then ss
// -Hulnp), in port order, TCP before UDP on one port, a port's sockets in
// ss's order. It is in the text report alone: hardn's JSON document does not
// carry it.
type Ports struct {
	// List are the sockets; a socket's Process is as ss printed it, the
	// host's word, "" where ss named none.
	List []reload.Socket
	// NoSS: ss is not installed. Nothing is listed, and Unread is empty.
	NoSS bool
	// Partial: ss printed lines that were not read, so List may lack
	// sockets.
	Partial bool
	// Root is whether hardn's effective uid is 0 (Users.Root): to another
	// user ss names that user's programs alone.
	Root detect.Tri
	// Unread is ss, where it ran and failed.
	Unread []Unread
}

// ReadPorts reads the listening sockets; root is whether hardn runs as root.
// Read calls it, each run under its bound; the gallery's mocks do too.
func ReadPorts(ctx context.Context, sys host.System, root detect.Tri) Ports {
	p := Ports{Root: root}
	socks, bad, err := reload.ReadListening(ctx, sys)
	switch {
	case errors.Is(err, host.ErrNotFound):
		p.NoSS = true
	case err != nil:
		p.Unread = []Unread{{Source: SourceSS, Err: err}}
	default:
		slices.SortStableFunc(socks, func(a, b reload.Socket) int {
			return cmp.Or(cmp.Compare(a.Port, b.Port), cmp.Compare(a.Proto, b.Proto))
		})
		p.List, p.Partial = socks, len(bad) > 0
	}
	return p
}

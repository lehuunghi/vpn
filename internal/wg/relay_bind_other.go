//go:build !windows

package wg

import (
	"net/netip"

	"github.com/lehuunghi/vpn/internal/relay"
	"golang.zx2c4.com/wireguard/conn"
)

// The relay does not change host routes. Avoid wireguard-go's optional Linux
// netlink route monitor so the portable packet path can run without NET_ADMIN.
type relayBind struct{ conn.Bind }

func newRelayBind() conn.Bind { return &relayBind{conn.NewDefaultBind()} }

func attachRelayHost(r *relay.Router, name string, addrs []netip.Prefix, mtu int) error { return nil }

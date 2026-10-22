package netcfg

import "net/netip"

// Rules describes the firewall vpn20 wants.
type Rules struct {
	// Relay owns packet forwarding; host configuration only opens WireGuard UDP.
	Relay bool
	// WindowsMode is auto, routed (peer/LAN access), or nat (require WinNAT).
	WindowsMode string
	// Iface is the WireGuard interface name, e.g. wg0.
	Iface string
	// Egress is the interface peers reach the outside world through. Empty
	// means "any interface that is not Iface", which is what most single-NIC
	// containers want.
	Egress string
	// ListenPort is the UDP port to accept WireGuard traffic on.
	ListenPort int
	// Subnets are the tunnel networks to masquerade (v4 and/or v6).
	Subnets []netip.Prefix
	// PeerIsolation drops traffic between peers when true.
	PeerIsolation bool
	// ClampMSS rewrites the MSS of forwarded SYNs to fit the path MTU. It
	// costs almost nothing and removes the single most common cause of
	// "the VPN connects but websites hang".
	ClampMSS bool
	// Table names the nftables table, so a host with its own rules never
	// collides with ours. Defaults to "vpn20".
	Table string
}

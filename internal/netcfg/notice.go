package netcfg

// NetworkNotice reports a working routed VPN without automatic Internet NAT.
// Firewall/isolation failures remain ordinary, fatal errors on Windows.
type NetworkNotice struct {
	Mode    string
	Message string
}

func (n *NetworkNotice) Error() string { return n.Message }

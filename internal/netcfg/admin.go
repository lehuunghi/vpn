package netcfg

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// lanAdminPort returns zero for a loopback-only listener. A Windows LAN rule
// must follow the configured port, including installations using custom ports.
func lanAdminPort(address string) (int, error) {
	if address == "" {
		return 0, nil
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return 0, fmt.Errorf("admin listener: %w", err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return 0, fmt.Errorf("admin listener has an invalid TCP port")
	}
	a, _ := netip.ParseAddr(host)
	if a.IsLoopback() || strings.EqualFold(host, "localhost") {
		return 0, nil
	}
	return n, nil
}

//go:build windows

package netcfg

import (
	"context"
	"fmt"
)

// A separate group keeps ordinary WireGuard rule refreshes from removing HTTPS.
func AllowPublicHTTPS(ctx context.Context, address string) error {
	port, err := lanAdminPort(address)
	if err != nil {
		return err
	}
	if port == 0 {
		return nil
	}
	_, err = powershell(ctx, fmt.Sprintf(
		"Get-NetFirewallRule -Name 'VPN20-Public-HTTPS' -ErrorAction SilentlyContinue | Remove-NetFirewallRule\n"+
			"$null = New-NetFirewallRule -Name 'VPN20-Public-HTTPS' -DisplayName 'VPN20 HTTPS' -Group 'VPN20-Public' -Direction Inbound -Action Allow -Protocol TCP -LocalPort %d -Profile Any", port))
	return err
}

func RemovePublicHTTPS(ctx context.Context) error {
	_, err := powershell(ctx, "Get-NetFirewallRule -Name 'VPN20-Public-HTTPS' -ErrorAction SilentlyContinue | Remove-NetFirewallRule")
	return err
}

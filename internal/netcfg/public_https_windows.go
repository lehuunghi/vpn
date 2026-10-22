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
	// Missing rules are normal when the feature was never enabled. An ignored
	// Get-NetFirewallRule error must not become powershell.exe's exit status.
	_, err := powershell(ctx, removePublicHTTPSScript)
	return err
}

const removePublicHTTPSScript = "$rules = @(Get-NetFirewallRule -Name 'VPN20-Public-HTTPS' -ErrorAction SilentlyContinue); if ($rules.Count) { $rules | Remove-NetFirewallRule -ErrorAction Stop }; exit 0"

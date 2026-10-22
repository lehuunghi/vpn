//go:build windows

package netcfg

import (
	"context"
	"testing"
)

func TestPublicHTTPSRemovalPowerShellExitStatus(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		wantError    bool
	}{
		{"never enabled", "function Get-NetFirewallRule { Write-Error 'Rule not found' -ErrorAction SilentlyContinue }; function Remove-NetFirewallRule { throw 'Must not remove a missing rule' }; ", false},
		{"present", "function Get-NetFirewallRule { [pscustomobject]@{Name='VPN20-Public-HTTPS'} }; function Remove-NetFirewallRule { }; ", false},
		{"permission denied", "function Get-NetFirewallRule { [pscustomobject]@{Name='VPN20-Public-HTTPS'} }; function Remove-NetFirewallRule { throw 'Permission denied' }; ", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := powershell(context.Background(), tc.script+removePublicHTTPSScript)
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected removal status: %v", err)
			}
		})
	}
}

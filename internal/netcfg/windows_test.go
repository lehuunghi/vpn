//go:build windows

package netcfg

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

// Exercise the shipped script using Windows PowerShell 5.1. Mock only OS
// mutations: missing providers and existing NATs must not disable peer routing.
func TestWindowsNetworkModes(t *testing.T) {
	for _, tc := range []struct {
		name, mode, scenario, uplink, want string
		wantError                          bool
	}{
		{"missing provider", "auto", "missing", "Ethernet", "routed", false},
		{"foreign NAT", "auto", "foreign", "Ethernet", "routed", false},
		{"routed offline", "routed", "missing", "", "routed", false},
		{"NAT available", "auto", "available", "Ethernet", "nat", false},
		{"strict NAT", "nat", "missing", "Ethernet", "", true},
		{"firewall failure", "auto", "firewall", "Ethernet", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := networkScript + `
$global:createdRules = 0
$global:createdNATs = 0
function Get-NetIPInterface { [pscustomobject]@{InterfaceIndex=17; InterfaceAlias='Ethernet'; AddressFamily='IPv4'; Forwarding='Disabled'} }
function Set-NetIPInterface { }
function Get-NetFirewallRule { }
function Remove-NetFirewallRule { }
function New-NetFirewallRule {
    if ($global:scenario -eq 'firewall') { throw 'Firewall denied' }
    $global:createdRules++
}
function Get-NetNat {
    if ($global:scenario -eq 'missing') { throw 'Invalid class MSFT_NetNat' }
    if ($global:scenario -eq 'foreign') { [pscustomobject]@{Name='OtherApplication'; InternalIPInterfaceAddressPrefix='172.20.0.0/16'} }
}
function New-NetNat { $global:createdNATs++ }
function Remove-NetNat { throw 'This test must never remove another application NAT' }
`
			script += fmt.Sprintf("\n$global:scenario=%s; $result=Set-VPN20Network -Tunnel 'wg0' -Uplink %s -Subnet '10.8.0.0/24' -Port 51820 -StatePath %s -Mode %s; [pscustomobject]@{Network=$result; Rules=$global:createdRules; NATs=$global:createdNATs} | ConvertTo-Json -Depth 5 -Compress", psQuote(tc.scenario), psQuote(tc.uplink), psQuote(filepath.Join(t.TempDir(), "state.json")), psQuote(tc.mode))
			out, err := powershell(context.Background(), script)
			if tc.wantError {
				if err == nil {
					t.Fatal("required network error was suppressed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				Network     struct{ Mode, Warning string }
				Rules, NATs int
			}
			if err = json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if got.Network.Mode != tc.want || got.Rules != 2 {
				t.Fatalf("unexpected network result: %+v", got)
			}
			if tc.want == "routed" && (got.NATs != 0 || got.Network.Warning == "") {
				t.Fatalf("routed fallback lost its warning or created NAT: %+v", got)
			}
		})
	}
}

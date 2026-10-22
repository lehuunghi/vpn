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

func TestWindowsRelayDoesNotRequireNATOrForwarding(t *testing.T) {
	script := networkScript + `
$global:createdRules = 0
function Get-NetNat { param([string]$Name); if ($Name -ne 'VPN20') { throw 'Relay must only query its own NAT' } }
function New-NetNat { throw 'Relay must not create NAT' }
function Remove-NetNat { throw 'No owned NAT exists in this scenario' }
function Get-NetIPInterface { throw 'Relay must not require an uplink or tunnel adapter' }
function Set-NetIPInterface { throw 'Relay must not enable forwarding' }
function Get-NetFirewallRule { }
function Remove-NetFirewallRule { }
function New-NetFirewallRule { param([string]$Name,[string]$DisplayName,[string]$Group,[string]$Direction,[string]$Action,[string]$Protocol,[int]$LocalPort,[string]$Profile,[string]$InterfaceAlias); if ($Name -eq 'VPN20-WireGuard' -and $LocalPort -ne 51820) { throw 'Unexpected listener firewall rule' }; $global:createdRules++ }
`
	script += fmt.Sprintf("\nSet-VPN20RelayNetwork -Port 51820 -Tunnel wg0 -StatePath %s; $global:createdRules", psQuote(filepath.Join(t.TempDir(), "state.json")))
	out, err := powershell(context.Background(), script)
	if err != nil || out != "2" {
		t.Fatalf("relay firewall without WinNAT: %q %v", out, err)
	}
}

func TestWindowsAdminFirewallIsLANOnly(t *testing.T) {
	script := networkScript + `
$global:adminRules = 0
function Get-NetNat { }
function Get-NetFirewallRule { }
function Remove-NetFirewallRule { }
function New-NetFirewallRule {
    param([string]$Name,[string]$DisplayName,[string]$Group,[string]$Direction,[string]$Action,[string]$Protocol,[int]$LocalPort,[string]$Profile,[string]$InterfaceAlias,[string]$RemoteAddress)
    if ($Name -eq 'VPN20-Admin-LAN') {
        if ($Protocol -ne 'TCP' -or $LocalPort -ne 51999 -or $RemoteAddress -ne 'LocalSubnet' -or $Direction -ne 'Inbound' -or $Group -ne 'VPN20') { throw 'Admin firewall has incorrect port or scope' }
        $global:adminRules++
    }
}
`
	script += fmt.Sprintf("\nSet-VPN20RelayNetwork -Port 51820 -AdminPort 51999 -Tunnel wg0 -StatePath %s; $global:adminRules", psQuote(filepath.Join(t.TempDir(), "state.json")))
	out, err := powershell(context.Background(), script)
	if err != nil || out != "1" {
		t.Fatalf("LAN admin firewall: %q %v", out, err)
	}
}

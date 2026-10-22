# Run in an elevated Windows PowerShell after connecting WireGuard.
# Examples: .\Enable-VPN20-PeerAccess.ps1 -EnableRemoteDesktop
#           .\Enable-VPN20-PeerAccess.ps1 -Undo
[CmdletBinding()]
param(
    [string]$VPNSubnet = '__VPN20_SUBNET__',
    [string]$InterfaceAlias = '',
    [switch]$EnableRemoteDesktop,
    [switch]$Undo
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (!$admin) { throw 'Open Windows PowerShell using Run as administrator, then run this script again.' }
$group = 'VPN20 Peer Access'
$statePath = Join-Path $env:ProgramData 'VPN20\peer-access-state.json'
$terminal = 'HKLM:\SYSTEM\CurrentControlSet\Control\Terminal Server'
$rdp = $terminal + '\WinStations\RDP-Tcp'

if ($Undo) {
    Get-NetFirewallRule -Group $group -ErrorAction SilentlyContinue | Remove-NetFirewallRule
    if (Test-Path -LiteralPath $statePath) {
        $state = Get-Content -Raw -LiteralPath $statePath | ConvertFrom-Json
        Set-ItemProperty $terminal -Name fDenyTSConnections -Value $state.Deny
        Set-ItemProperty $rdp -Name UserAuthentication -Value $state.NLA
        if ($state.ServiceStart -eq 'Disabled') { Stop-Service TermService -Force; Set-Service TermService -StartupType Disabled }
        elseif ($state.ServiceStart -eq 'Manual') { Set-Service TermService -StartupType Manual }
        else { Set-Service TermService -StartupType Automatic }
        Remove-Item -LiteralPath $statePath -Force
    }
    Write-Output 'VPN20 ping/RDP firewall rules removed; saved RDP settings restored.'
    return
}

function Test-VPNAddress([string]$Address, [string]$Prefix) {
    $parts = $Prefix.Split('/')
    if ($parts.Count -ne 2) { throw 'VPNSubnet must be an IPv4 CIDR, e.g. 10.8.0.0/24.' }
    $bits = [int]$parts[1]
    $network = [Net.IPAddress]::Parse($parts[0]).GetAddressBytes()
    $ip = [Net.IPAddress]::Parse($Address).GetAddressBytes()
    if ($bits -lt 1 -or $bits -gt 32 -or $network.Length -ne 4 -or $ip.Length -ne 4) { throw 'VPNSubnet must be an IPv4 CIDR.' }
    for ($i=0; $i -lt $bits; $i++) {
        $mask = 1 -shl (7 - ($i % 8))
        $byte = [int][Math]::Floor($i / 8)
        if (($ip[$byte] -band $mask) -ne ($network[$byte] -band $mask)) { return $false }
    }
    return $true
}
$candidates = @(Get-NetIPAddress -AddressFamily IPv4 | Where-Object { Test-VPNAddress $_.IPAddress $VPNSubnet })
if ($InterfaceAlias) { $candidates = @($candidates | Where-Object InterfaceAlias -eq $InterfaceAlias) }
else {
    $candidates = @($candidates | Where-Object { (Get-NetAdapter -InterfaceIndex $_.InterfaceIndex).InterfaceDescription -match 'WireGuard' })
}
$indices = @($candidates | ForEach-Object { $_.InterfaceIndex } | Select-Object -Unique)
if ($indices.Count -ne 1) { throw 'Connect the WireGuard VPN first. If detection is ambiguous, specify -InterfaceAlias with the VPN adapter name.' }
$adapter = Get-NetAdapter -InterfaceIndex $indices[0]
$vpnIP = ($candidates | Select-Object -First 1).IPAddress

# Only echo requests arriving over this VPN from this VPN subnet are allowed.
Get-NetFirewallRule -Name 'VPN20-Peer-Ping' -ErrorAction SilentlyContinue | Remove-NetFirewallRule
$null = New-NetFirewallRule -Name 'VPN20-Peer-Ping' -DisplayName 'VPN20 peer ping' -Group $group -Direction Inbound -Action Allow -Protocol ICMPv4 -IcmpType 8 -RemoteAddress $VPNSubnet -LocalAddress $vpnIP -InterfaceAlias $adapter.Name -Profile Any
Write-Output "Ping enabled for $vpnIP from $VPNSubnet over $($adapter.Name)."

if ($EnableRemoteDesktop) {
    $edition = (Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion').EditionID
    if ($edition -match '^(Core|Starter)' -or !(Test-Path -LiteralPath $rdp)) {
        throw 'Ping is enabled. This Windows edition cannot host Microsoft Remote Desktop; use Pro/Enterprise/Education/Server for incoming RDP.'
    }
    $policyPath = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows NT\Terminal Services'
    if (Test-Path -LiteralPath $policyPath) {
        $policy = Get-ItemProperty $policyPath
        if ($policy.PSObject.Properties['fDenyTSConnections'] -and $policy.fDenyTSConnections -eq 1) { throw 'Remote Desktop is disabled by policy. Contact your administrator.' }
    }
    if (!(Test-Path -LiteralPath $statePath)) {
        $null = New-Item -ItemType Directory -Force (Split-Path -Parent $statePath)
        $state = [pscustomobject]@{
            Deny = (Get-ItemProperty $terminal).fDenyTSConnections
            NLA = (Get-ItemProperty $rdp).UserAuthentication
            ServiceStart = [string](Get-Service TermService).StartType
        }
        $state | ConvertTo-Json | Set-Content -LiteralPath $statePath -Encoding UTF8
        $acl = New-Object Security.AccessControl.FileSecurity
        $acl.SetAccessRuleProtection($true, $false)
        foreach ($sid in @('S-1-5-18','S-1-5-32-544')) {
            $id = New-Object Security.Principal.SecurityIdentifier($sid)
            $rule = New-Object Security.AccessControl.FileSystemAccessRule($id, 'FullControl', 'Allow')
            $acl.AddAccessRule($rule)
        }
        Set-Acl -LiteralPath $statePath -AclObject $acl
    }
    Set-ItemProperty $terminal -Name fDenyTSConnections -Value 0
    Set-ItemProperty $rdp -Name UserAuthentication -Value 1
    if ((Get-Service TermService).StartType -eq 'Disabled') { Set-Service TermService -StartupType Manual }
    Start-Service TermService
    $port = [int](Get-ItemProperty $rdp).PortNumber
    foreach ($protocol in @('TCP','UDP')) {
        $name = 'VPN20-Peer-RDP-' + $protocol
        Get-NetFirewallRule -Name $name -ErrorAction SilentlyContinue | Remove-NetFirewallRule
        $null = New-NetFirewallRule -Name $name -DisplayName "VPN20 peer RDP $protocol" -Group $group -Direction Inbound -Action Allow -Protocol $protocol -LocalPort $port -RemoteAddress $VPNSubnet -LocalAddress $vpnIP -InterfaceAlias $adapter.Name -Profile Any
    }
    Write-Output "Remote Desktop with NLA enabled. Connect to ${vpnIP}:$port using a permitted Windows account with a password."
}

# The admin interface is reachable from the LAN without opening it to the WAN.
function Add-VPN20AdminFirewall {
    param([int]$AdminPort)
    if ($AdminPort -gt 0) {
        $null = New-NetFirewallRule -Name 'VPN20-Admin-LAN' -DisplayName 'VPN20 administration (LAN)' -Group 'VPN20' -Direction Inbound -Action Allow -Protocol TCP -LocalPort $AdminPort -RemoteAddress LocalSubnet -Profile Any
    }
}

# Relay uses ordinary host sockets and needs no host NAT or forwarding.
function Set-VPN20RelayNetwork {
    param([int]$Port, [string]$Tunnel, [string]$StatePath, [int]$AdminPort = 0)
    # Restore forwarding settings and remove VPN20's NAT left by a previous
    # native installation. Never touch NATs or firewall groups owned by others.
    Remove-VPN20Network -StatePath $StatePath
    $null = New-NetFirewallRule -Name 'VPN20-WireGuard' -DisplayName 'VPN20 WireGuard UDP' -Group 'VPN20' -Direction Inbound -Action Allow -Protocol UDP -LocalPort $Port -Profile Any
    $null = New-NetFirewallRule -Name 'VPN20-Tunnel' -DisplayName 'VPN20 relay host traffic' -Group 'VPN20' -Direction Inbound -Action Allow -InterfaceAlias $Tunnel -Profile Any
    Add-VPN20AdminFirewall -AdminPort $AdminPort
}

# Native routing and firewall rules are independent of optional Internet NAT.
function Set-VPN20Network {
    param([string]$Tunnel, [string]$Uplink, [string]$Subnet, [int]$Port, [string]$StatePath,
        [ValidateSet('auto','routed','nat')][string]$Mode = 'auto', [switch]$IPv6, [int]$AdminPort = 0)
    $states = @()
    if (Test-Path -LiteralPath $StatePath) {
        $snapshot = Get-Content -Raw -LiteralPath $StatePath | ConvertFrom-Json
        $states = @($snapshot.Interfaces)
    }
    $rows = @()
    if ($Uplink) {
        $rows = @(Get-NetIPInterface -InterfaceAlias $Uplink -AddressFamily IPv4 -PolicyStore ActiveStore)
        if ($IPv6) { $rows += @(Get-NetIPInterface -InterfaceAlias $Uplink -AddressFamily IPv6 -PolicyStore ActiveStore) }
    }
    foreach ($row in $rows) {
        if (!($states | Where-Object { $_.InterfaceIndex -eq $row.InterfaceIndex -and $_.AddressFamily -eq [string]$row.AddressFamily })) {
            $states += [pscustomobject]@{InterfaceIndex=$row.InterfaceIndex; InterfaceAlias=$row.InterfaceAlias; AddressFamily=[string]$row.AddressFamily; Forwarding=[string]$row.Forwarding}
        }
    }
    $null = New-Item -ItemType Directory -Force -Path (Split-Path -Parent $StatePath)
    [pscustomobject]@{Version=1; Interfaces=@($states)} | ConvertTo-Json -Depth 5 |
        Set-Content -LiteralPath ($StatePath + '.tmp') -Encoding UTF8
    Move-Item -LiteralPath ($StatePath + '.tmp') -Destination $StatePath -Force
    if ($rows.Count) { $rows | Set-NetIPInterface -Forwarding Enabled -PolicyStore ActiveStore }
    Get-NetFirewallRule -Group 'VPN20' -ErrorAction SilentlyContinue | Remove-NetFirewallRule
    $null = New-NetFirewallRule -Name 'VPN20-WireGuard' -DisplayName 'VPN20 WireGuard UDP' -Group 'VPN20' -Direction Inbound -Action Allow -Protocol UDP -LocalPort $Port -Profile Any
    $null = New-NetFirewallRule -Name 'VPN20-Tunnel' -DisplayName 'VPN20 tunnel traffic' -Group 'VPN20' -Direction Inbound -Action Allow -InterfaceAlias $Tunnel -Profile Any
    Add-VPN20AdminFirewall -AdminPort $AdminPort
    $result = [pscustomobject]@{Mode='routed'; Warning='VPN ngang hang dang hoat dong. Truy cap Internet qua may chu chua duoc bat.'}
    if ($Mode -eq 'routed') {
        # A previous crash may have left our own NAT behind. Other NATs are untouched.
        try { Get-NetNat -Name 'VPN20' -ErrorAction Stop | Remove-NetNat -Confirm:$false } catch { }
        return $result
    }
    try {
        if (!$Uplink) { throw 'Khong co tuyen Internet mac dinh.' }
        if (!(Get-Command New-NetNat -ErrorAction SilentlyContinue)) { throw 'Windows khong co WinNAT/NetNat.' }
        $allNats = @(Get-NetNat -ErrorAction Stop)
        $foreign = @($allNats | Where-Object Name -ne 'VPN20')
        if ($foreign.Count -gt 0) { throw ('WinNAT dang duoc ung dung khac su dung: ' + ($foreign.Name -join ', ')) }
        $existing = $allNats | Where-Object Name -eq 'VPN20'
        if ($existing -and $existing.InternalIPInterfaceAddressPrefix -ne $Subnet) { throw 'VPN20 NAT dang dung subnet khac; dung dich vu truoc khi doi subnet.' }
        if (!$existing) { $null = New-NetNat -Name 'VPN20' -InternalIPInterfaceAddressPrefix $Subnet }
        $result.Mode = 'nat'
        $result.Warning = ''
    } catch {
        if ($Mode -eq 'nat') { throw }
        $result.Warning = 'VPN ngang hang van hoat dong; chua NAT Internet: ' + $_.Exception.Message
    }
    return $result
}

function Remove-VPN20Network {
    param([string]$StatePath)
    # Missing/disabled WinNAT must not prevent stopping or uninstalling VPN20.
    try { $ownedNat = @(Get-NetNat -Name 'VPN20' -ErrorAction Stop) } catch { $ownedNat = @() }
    if ($ownedNat.Count) { $ownedNat | Remove-NetNat -Confirm:$false }
    Get-NetFirewallRule -Group 'VPN20' -ErrorAction SilentlyContinue | Remove-NetFirewallRule
    if (Test-Path -LiteralPath $StatePath) {
        $snapshot = Get-Content -Raw -LiteralPath $StatePath | ConvertFrom-Json
        $states = @($snapshot.Interfaces)
        foreach ($state in $states) {
            if ($null -eq $state -or $null -eq $state.InterfaceIndex -or !$state.AddressFamily) {
                throw 'VPN20 forwarding-state file is invalid; original forwarding settings were not overwritten.'
            }
            $row = Get-NetIPInterface -InterfaceIndex $state.InterfaceIndex -AddressFamily $state.AddressFamily -ErrorAction SilentlyContinue
            if ($row -and $row.InterfaceAlias -eq $state.InterfaceAlias) {
                $row | Set-NetIPInterface -Forwarding $state.Forwarding -PolicyStore ActiveStore
            }
        }
        Remove-Item -LiteralPath $StatePath -Force
    }
}

# Only resources named VPN20 and forwarding states recorded by VPN20 are owned.
function Set-VPN20Network {
    param([string]$Tunnel, [string]$Uplink, [string]$Subnet, [int]$Port, [string]$StatePath)
    if (!(Get-Command New-NetNat -ErrorAction SilentlyContinue)) {
        throw 'WinNAT is unavailable. Use Windows 10/11 Pro or Enterprise, or Windows Server 2016+ with the NetNat provider installed.'
    }
    $allNats = @(Get-NetNat -ErrorAction Stop)
    $foreign = @($allNats | Where-Object Name -ne 'VPN20')
    if ($foreign.Count -gt 0) {
        throw ('WinNAT already belongs to another application: ' + ($foreign.Name -join ', ') + '. VPN20 did not modify it. See docs/windows.md for routed deployment.')
    }
    $existing = $allNats | Where-Object Name -eq 'VPN20'
    if ($existing -and $existing.InternalIPInterfaceAddressPrefix -ne $Subnet) {
        throw 'The existing VPN20 NAT uses another subnet. Stop VPN20 before changing VPN20_SUBNET.'
    }
    # Persist the original uplink state before changing it, including after an
    # unexpected process exit. Never overwrite the original snapshot on restart.
    $states = @()
    if (Test-Path -LiteralPath $StatePath) {
        $snapshot = Get-Content -Raw -LiteralPath $StatePath | ConvertFrom-Json
        $states = @($snapshot.Interfaces)
    }
    $rows = @(Get-NetIPInterface -InterfaceAlias $Uplink -PolicyStore ActiveStore)
    foreach ($row in $rows) {
        if (!($states | Where-Object { $_.InterfaceIndex -eq $row.InterfaceIndex -and $_.AddressFamily -eq [string]$row.AddressFamily })) {
            $states += [pscustomobject]@{InterfaceIndex=$row.InterfaceIndex; InterfaceAlias=$row.InterfaceAlias; AddressFamily=[string]$row.AddressFamily; Forwarding=[string]$row.Forwarding}
        }
    }
    $null = New-Item -ItemType Directory -Force -Path (Split-Path -Parent $StatePath)
    # An object envelope avoids Windows PowerShell 5.1 adding another array
    # level each time a JSON array is passed through ConvertFrom-Json.
    [pscustomobject]@{Version=1; Interfaces=@($states)} | ConvertTo-Json -Depth 5 |
        Set-Content -LiteralPath ($StatePath + '.tmp') -Encoding UTF8
    Move-Item -LiteralPath ($StatePath + '.tmp') -Destination $StatePath -Force
    $rows | Set-NetIPInterface -Forwarding Enabled -PolicyStore ActiveStore
    if (!$existing) {
        $null = New-NetNat -Name 'VPN20' -InternalIPInterfaceAddressPrefix $Subnet
    }
    Get-NetFirewallRule -Group 'VPN20' -ErrorAction SilentlyContinue | Remove-NetFirewallRule
    $null = New-NetFirewallRule -Name 'VPN20-WireGuard' -DisplayName 'VPN20 WireGuard UDP' -Group 'VPN20' -Direction Inbound -Action Allow -Protocol UDP -LocalPort $Port -Profile Any
    $null = New-NetFirewallRule -Name 'VPN20-Tunnel' -DisplayName 'VPN20 tunnel traffic' -Group 'VPN20' -Direction Inbound -Action Allow -InterfaceAlias $Tunnel -Profile Any
}

function Remove-VPN20Network {
    param([string]$StatePath)
    Get-NetNat -Name 'VPN20' -ErrorAction SilentlyContinue | Remove-NetNat -Confirm:$false
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

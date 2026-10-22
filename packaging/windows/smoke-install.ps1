param([Parameter(Mandatory)][string]$Installer)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$app = Join-Path $env:ProgramFiles 'VPN20'
$data = Join-Path $env:ProgramData 'VPN20'
$installed = $false
try {
    $p = Start-Process -FilePath $Installer -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-' -Wait -PassThru
    if ($p.ExitCode -ne 0) { throw "Installer exited $($p.ExitCode)" }
    $installed = $true
    $service = Get-CimInstance Win32_Service -Filter "Name='VPN20'"
    if ($service.StartMode -ne 'Auto' -or $service.State -ne 'Running') { throw 'VPN20 is not an automatic running service' }
    if ($service.StartName -ne 'LocalSystem') { throw 'Unexpected service account' }
    if ((Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Services\VPN20').DelayedAutoStart -ne 1) { throw 'Delayed automatic startup was not configured' }
    $acl = Get-Acl $data
    $broadSids = @('S-1-1-0', 'S-1-5-11', 'S-1-5-32-545')
    foreach ($ace in $acl.Access) {
        if ($ace.AccessControlType -eq 'Allow' -and $broadSids -contains $ace.IdentityReference.Translate([System.Security.Principal.SecurityIdentifier]).Value) { throw 'VPN20 private data is accessible to non-administrators' }
    }
    $health = Invoke-WebRequest 'http://127.0.0.1:51821/api/health' -UseBasicParsing
    if ($health.StatusCode -ne 200) { throw 'Health endpoint failed' }
    $page = Invoke-WebRequest 'http://127.0.0.1:51821/' -UseBasicParsing
    if ($page.Content -notmatch '<html') { throw 'Embedded administration UI is missing' }
    # Create a real administrator and device through the installed API.
    $base = 'http://127.0.0.1:51821'
    $headers = @{Origin=$base}
    $body = @{username='releaseadmin';password='VPN20-release-test-only!82';endpointHost='vpn.example.com'} | ConvertTo-Json
    $null = Invoke-RestMethod "$base/api/setup" -Method Post -ContentType 'application/json' -Headers $headers -Body $body -SessionVariable session
    $peer = Invoke-RestMethod "$base/api/peers" -Method Post -ContentType 'application/json' -Headers $headers -WebSession $session -Body '{"name":"Windows release smoke"}'
    if (!$peer.id) { throw 'Peer creation failed' }
    $conf = Invoke-WebRequest "$base/api/peers/$($peer.id)/config" -Headers $headers -WebSession $session -UseBasicParsing
    $configText = $conf.Content
    if ($configText -is [byte[]]) { $configText = [System.Text.Encoding]::UTF8.GetString($configText) }
    if ($configText -notmatch '\[Interface\]') { throw 'Peer configuration export failed' }
    if ($configText -notmatch 'AllowedIPs = 10\.8\.0\.0/24' -or $configText -match '0\.0\.0\.0/0|(?m)^DNS =') { throw 'Fresh Windows devices must use peer routes and preserve local Internet/DNS' }
    $helperPath = Join-Path $env:TEMP 'Enable-VPN20-PeerAccess.ps1'
    Invoke-WebRequest "$base/api/windows/peer-access.ps1" -Headers $headers -WebSession $session -UseBasicParsing -OutFile $helperPath
    $ps51 = "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe"
    $oldDeny = (Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\Terminal Server').fDenyTSConnections
    & $ps51 -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $helperPath -InterfaceAlias wg0 -EnableRemoteDesktop
    if ($LASTEXITCODE) { throw 'Peer ping/RDP helper failed in Windows PowerShell 5.1' }
    foreach ($name in @('VPN20-Peer-Ping', 'VPN20-Peer-RDP-TCP', 'VPN20-Peer-RDP-UDP')) {
        $rule = Get-NetFirewallRule -Name $name
        $remote = @(@($rule | Get-NetFirewallAddressFilter).RemoteAddress)
        # NetSecurity normalizes CIDR to dotted masks on some Windows builds.
        if ($remote.Count -ne 1 -or $remote[0] -notin @('10.8.0.0/24', '10.8.0.0/255.255.255.0')) { throw "Peer access was not restricted to the VPN subnet: $($remote -join ',')" }
        if ((@($rule | Get-NetFirewallAddressFilter).LocalAddress -join ',') -ne '10.8.0.1') { throw 'Peer access was not restricted to the VPN IP' }
        if ((@($rule | Get-NetFirewallInterfaceFilter).InterfaceAlias -join ',') -ne 'wg0') { throw 'Peer access was not restricted to the VPN adapter' }
    }
    if ((Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\Terminal Server\WinStations\RDP-Tcp').UserAuthentication -ne 1) { throw 'RDP helper did not enable NLA' }
    & $ps51 -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $helperPath -Undo
    if ($LASTEXITCODE -or (Get-NetFirewallRule -Group 'VPN20 Peer Access' -ErrorAction SilentlyContinue)) { throw 'Peer access undo failed' }
    if ((Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\Terminal Server').fDenyTSConnections -ne $oldDeny) { throw 'Peer access undo did not restore RDP configuration' }
    $qr = Invoke-WebRequest "$base/api/peers/$($peer.id)/qr.png" -Headers $headers -WebSession $session -UseBasicParsing
    if ($qr.StatusCode -ne 200) { throw 'QR export failed' }
    $null = Invoke-RestMethod "$base/api/peers/$($peer.id)/disable" -Method Post -ContentType 'application/json' -Headers $headers -WebSession $session -Body '{}'
    $null = Invoke-RestMethod "$base/api/peers/$($peer.id)/enable" -Method Post -ContentType 'application/json' -Headers $headers -WebSession $session -Body '{}'
    & "$app\vpn20.exe" stop-service
    if ($LASTEXITCODE) { throw 'Service stop failed' }
    if (Get-NetNat -Name VPN20 -ErrorAction SilentlyContinue) { throw 'VPN20 NAT was not cleaned up' }
    Start-Service VPN20
    for ($i=0; $i -lt 40; $i++) {
        try { $null=Invoke-RestMethod "$base/api/health"; break } catch { Start-Sleep -Milliseconds 500 }
    }
    $null=Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType 'application/json' -Headers $headers -Body (@{username='releaseadmin';password='VPN20-release-test-only!82'} | ConvertTo-Json) -SessionVariable session
    $saved = @(Invoke-RestMethod "$base/api/peers" -Headers $headers -WebSession $session)
    if ($saved.Count -ne 1 -or $saved[0].id -ne $peer.id) { throw 'Service restart lost saved devices' }
    # Upgrade in routed mode must preserve data and start without Internet NAT.
    & "$app\vpn20.exe" stop-service
    if ($LASTEXITCODE) { throw 'Could not stop before changing network mode' }
    $settings = Get-Content "$data\config.json" -Raw | ConvertFrom-Json
    $settings | Add-Member -NotePropertyName VPN20_WINDOWS_NETWORK_MODE -NotePropertyValue routed -Force
    $settings | ConvertTo-Json | Set-Content "$data\config.json" -Encoding utf8NoBOM
    $p = Start-Process -FilePath $Installer -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-' -Wait -PassThru
    if ($p.ExitCode -ne 0) { throw 'Installer upgrade failed' }
    $null = Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType 'application/json' -Headers $headers -Body (@{username='releaseadmin';password='VPN20-release-test-only!82'} | ConvertTo-Json) -SessionVariable session
    $status = Invoke-RestMethod "$base/api/status" -Headers $headers -WebSession $session
    if ($status.networkMode -ne 'routed' -or !$status.networkWarning) { throw 'Installer did not start in routed mode' }
    if (Get-NetNat -Name VPN20 -ErrorAction SilentlyContinue) { throw 'Routed mode unexpectedly enabled VPN20 NAT' }
    # A service failure must make silent Setup fail, with actionable details.
    & "$app\vpn20.exe" stop-service
    if ($LASTEXITCODE) { throw 'Could not stop before testing a port conflict' }
    $blocker = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 51821)
    $blocker.Start()
    try {
        $p = Start-Process -FilePath $Installer -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-' -Wait -PassThru
        if ($p.ExitCode -eq 0) { throw 'Installer reported success with a blocked administration port' }
        if ((Get-Content "$data\install-error.txt" -Raw) -notmatch 'listen.*51821') { throw 'Installer did not preserve the actual port error' }
    } finally { $blocker.Stop() }
    $p = Start-Process -FilePath $Installer -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-' -Wait -PassThru
    if ($p.ExitCode -ne 0) { throw 'Installer did not recover after releasing the administration port' }
    $null = Invoke-RestMethod "$base/api/health"
    Write-Output 'PASS: install, automatic service, UI/API, peer config/QR, scoped ping/RDP helper and undo, restart/persistence, routed upgrade, failed-install exit code and recovery'
} finally {
    if (Test-Path "$app\unins000.exe") {
        $p=Start-Process "$app\unins000.exe" -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART' -Wait -PassThru
        if ($p.ExitCode -ne 0) { throw 'Uninstaller failed' }
    }
    if (Get-Service VPN20 -ErrorAction SilentlyContinue) { throw 'Uninstaller left the VPN20 service behind' }
    if ($installed -and !(Test-Path "$data\vpn20.db")) { throw 'Uninstaller unexpectedly removed saved data' }
}

param([Parameter(Mandatory)][string]$Installer,
    [ValidateSet('x64','x86')][string]$Architecture = 'x64')
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$app = Join-Path $env:ProgramFiles 'VPN20'
if ($Architecture -eq 'x86' -and ${env:ProgramFiles(x86)}) { $app = Join-Path ${env:ProgramFiles(x86)} 'VPN20' }
$data = Join-Path $env:ProgramData 'VPN20'
$installed = $false
$adminPort = 51821
try {
    # Hyper-V/HNS can reserve default UDP/TCP ports on a hosted runner. Exercise
    # the supported custom-port configuration in that case, without deleting
    # exclusions or changing another application's network configuration.
    if (!(Test-Path "$data\config.json")) {
        $seed = @{}
        $probe = [Net.Sockets.UdpClient]::new([Net.Sockets.AddressFamily]::InterNetwork)
        try { $probe.Client.Bind([Net.IPEndPoint]::new([Net.IPAddress]::Any, 51820)) }
        catch {
            $failure = $_.Exception
            while ($failure.InnerException) { $failure = $failure.InnerException }
            if ($failure -isnot [Net.Sockets.SocketException] -or $failure.NativeErrorCode -notin @(10013,10048)) { throw }
            $probe.Dispose()
            $probe = [Net.Sockets.UdpClient]::new([Net.IPEndPoint]::new([Net.IPAddress]::Any, 0))
            $port = [string]$probe.Client.LocalEndPoint.Port
            $seed.VPN20_PORT = $port
            Write-Output "Default UDP port is unavailable on this runner; testing configured VPN20_PORT=$port"
        } finally { $probe.Dispose() }
        $tcp = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Any, $adminPort)
        try { $tcp.Start() }
        catch {
            $failure = $_.Exception
            while ($failure.InnerException) { $failure = $failure.InnerException }
            if ($failure -isnot [Net.Sockets.SocketException] -or $failure.NativeErrorCode -notin @(10013,10048)) { throw }
            $tcp.Stop()
            $tcp = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Any, 0)
            $tcp.Start()
            $adminPort = $tcp.LocalEndpoint.Port
            $seed.VPN20_HTTP_LISTEN = "0.0.0.0:$adminPort"
            Write-Output "Default TCP port is unavailable on this runner; testing configured VPN20_HTTP_LISTEN=0.0.0.0:$adminPort"
        } finally { $tcp.Stop() }
        if ($seed.Count) {
            $null = New-Item -ItemType Directory -Force $data
            [IO.File]::WriteAllText("$data\config.json", ($seed | ConvertTo-Json), [Text.UTF8Encoding]::new($false))
        }
    }
    $base = "http://127.0.0.1:$adminPort"
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
    $health = Invoke-WebRequest "$base/api/health" -UseBasicParsing
    if ($health.StatusCode -ne 200) { throw 'Health endpoint failed' }
    $adminRule = Get-NetFirewallRule -Name 'VPN20-Admin-LAN'
    if ((@($adminRule | Get-NetFirewallAddressFilter).RemoteAddress -join ',') -ne 'LocalSubnet') { throw 'Admin firewall must be limited to the LAN' }
    $portFilter = $adminRule | Get-NetFirewallPortFilter
    if ($portFilter.Protocol -ne 'TCP' -or [int]$portFilter.LocalPort -ne $adminPort) { throw 'Admin firewall did not follow the configured TCP port' }
    $lanAddress = Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.InterfaceAlias -ne 'wg0' -and $_.IPAddress -notlike '127.*' -and $_.IPAddress -notlike '169.254.*' -and $_.AddressState -eq 'Preferred' } | Select-Object -First 1
    if (!$lanAddress) { throw 'LAN integration test requires an IPv4 interface' }
    $lanHealth = Invoke-RestMethod "http://$($lanAddress.IPAddress):$adminPort/api/health"
    if (!$lanHealth.ok) { throw 'Admin listener is not reachable through the actual LAN IP' }
    $page = Invoke-WebRequest "$base/" -UseBasicParsing
    if ($page.Content -notmatch '<html') { throw 'Embedded administration UI is missing' }
    # Create a real administrator and device through the installed API.
    $headers = @{Origin=$base}
    $body = @{username='releaseadmin';password='VPN20-release-test-only!82';endpointHost='vpn.example.com'} | ConvertTo-Json
    $null = Invoke-RestMethod "$base/api/setup" -Method Post -ContentType 'application/json' -Headers $headers -Body $body -SessionVariable session
    $peer = Invoke-RestMethod "$base/api/peers" -Method Post -ContentType 'application/json' -Headers $headers -WebSession $session -Body '{"name":"Windows release smoke"}'
    if (!$peer.id) { throw 'Peer creation failed' }
    $conf = Invoke-WebRequest "$base/api/peers/$($peer.id)/config" -Headers $headers -WebSession $session -UseBasicParsing
    $status = Invoke-RestMethod "$base/api/status" -Headers $headers -WebSession $session
    if ($status.backend -ne 'relay' -or $status.networkMode -ne 'relay' -or !$status.settings.clampMSS) { throw 'Fresh Windows installation did not enable the relay with MSS support' }
    $updateStatus = Invoke-RestMethod "$base/api/update" -Headers $headers -WebSession $session
    if (!$updateStatus.supported -or $updateStatus.method -ne 'windows') { throw ('Installed service did not enable the administrator updater: ' + $updateStatus.reason) }
    & "$app\vpn20.exe" healthcheck
    if ($LASTEXITCODE) { throw 'Installed command-line healthcheck failed' }
    $configText = $conf.Content
    if ($configText -is [byte[]]) { $configText = [System.Text.Encoding]::UTF8.GetString($configText) }
    if ($configText -notmatch '\[Interface\]') { throw 'Peer configuration export failed' }
    if ($configText -notmatch 'AllowedIPs = 10\.8\.0\.0/24' -or $configText -match '0\.0\.0\.0/0|(?m)^DNS =') { throw 'Fresh Windows devices must use peer routes and preserve local Internet/DNS' }
    $qr = Invoke-WebRequest "$base/api/peers/$($peer.id)/qr.png" -Headers $headers -WebSession $session -UseBasicParsing
    if ($qr.StatusCode -ne 200) { throw 'QR export failed' }
    $null = Invoke-RestMethod "$base/api/peers/$($peer.id)/disable" -Method Post -ContentType 'application/json' -Headers $headers -WebSession $session -Body '{}'
    $null = Invoke-RestMethod "$base/api/peers/$($peer.id)/enable" -Method Post -ContentType 'application/json' -Headers $headers -WebSession $session -Body '{}'
    & "$app\vpn20.exe" stop-service
    if ($LASTEXITCODE) { throw 'Service stop failed' }
    if (Get-NetNat -Name VPN20 -ErrorAction SilentlyContinue) { throw 'VPN20 NAT was not cleaned up' }
    if (Get-NetFirewallRule -Name 'VPN20-Admin-LAN' -ErrorAction SilentlyContinue) { throw 'Admin LAN rule was not cleaned up' }
    # Reproduce an older loopback-only installation, then opt in using the CLI.
    $settings = Get-Content "$data\config.json" -Raw | ConvertFrom-Json
    $settings | Add-Member -NotePropertyName VPN20_HTTP_LISTEN -NotePropertyValue "127.0.0.1:$adminPort" -Force
    $settings | ConvertTo-Json | Set-Content "$data\config.json" -Encoding utf8NoBOM
    Start-Service VPN20
    for ($i=0; $i -lt 40; $i++) {
        try { $null=Invoke-RestMethod "$base/api/health"; break } catch { Start-Sleep -Milliseconds 500 }
    }
    if (Get-NetFirewallRule -Name 'VPN20-Admin-LAN' -ErrorAction SilentlyContinue) { throw 'Loopback listener unexpectedly opened LAN firewall' }
    & "$app\vpn20.exe" enable-lan
    if ($LASTEXITCODE) { throw 'LAN opt-in command failed' }
    if ((Get-Content "$data\config.json" -Raw | ConvertFrom-Json).VPN20_HTTP_LISTEN -ne "0.0.0.0:$adminPort") { throw 'LAN command lost the configured port' }
    $null = Invoke-RestMethod "http://$($lanAddress.IPAddress):$adminPort/api/health"
    $null=Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType 'application/json' -Headers $headers -Body (@{username='releaseadmin';password='VPN20-release-test-only!82'} | ConvertTo-Json) -SessionVariable session
    $saved = @(Invoke-RestMethod "$base/api/peers" -Headers $headers -WebSession $session)
    if ($saved.Count -ne 1 -or $saved[0].id -ne $peer.id) { throw 'Service restart lost saved devices' }
    # Upgrade in routed mode must preserve data and start without Internet NAT.
    $networkSettings = Invoke-RestMethod "$base/api/settings" -Headers $headers -WebSession $session
    $networkSettings.clampMSS = $false
    $null = Invoke-RestMethod "$base/api/settings" -Method Put -ContentType 'application/json' -Headers $headers -WebSession $session -Body ($networkSettings | ConvertTo-Json)
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
    # A service failure must make silent Setup fail, with actionable details.
    & "$app\vpn20.exe" stop-service
    if ($LASTEXITCODE) { throw 'Could not stop before testing a port conflict' }
    $blocker = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $adminPort)
    $blocker.Start()
    try {
        $p = Start-Process -FilePath $Installer -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-' -Wait -PassThru
        if ($p.ExitCode -eq 0) { throw 'Installer reported success with a blocked administration port' }
        if ((Get-Content "$data\install-error.txt" -Raw) -notmatch "listen.*$adminPort") { throw 'Installer did not preserve the actual port error' }
    } finally { $blocker.Stop() }
    $p = Start-Process -FilePath $Installer -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-' -Wait -PassThru
    if ($p.ExitCode -ne 0) { throw 'Installer did not recover after releasing the administration port' }
    $null = Invoke-RestMethod "$base/api/health"
    Write-Output 'PASS: install, actual LAN IP, scoped admin TCP firewall, loopback opt-in, automatic service, UI/API, peer config/QR, scoped ping/RDP helper and undo, restart/persistence, routed upgrade, failed-install exit code and recovery'
} finally {
    if (Test-Path "$app\unins000.exe") {
        $p=Start-Process "$app\unins000.exe" -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART' -Wait -PassThru
        if ($p.ExitCode -ne 0) { throw 'Uninstaller failed' }
    }
    if (Get-Service VPN20 -ErrorAction SilentlyContinue) { throw 'Uninstaller left the VPN20 service behind' }
    if ($installed -and !(Test-Path "$data\vpn20.db")) { throw 'Uninstaller unexpectedly removed saved data' }
}

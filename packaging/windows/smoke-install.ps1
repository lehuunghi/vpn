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
    # Upgrade must preserve the same SQLite database and first administrator.
    $p = Start-Process -FilePath $Installer -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-' -Wait -PassThru
    if ($p.ExitCode -ne 0) { throw 'Installer upgrade failed' }
    $null = Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType 'application/json' -Headers $headers -Body (@{username='releaseadmin';password='VPN20-release-test-only!82'} | ConvertTo-Json)
    Write-Output 'PASS: install, automatic service, UI, admin setup, peer CRUD/config/QR, stop, restart, persistence, upgrade'
} finally {
    if (Test-Path "$app\unins000.exe") {
        $p=Start-Process "$app\unins000.exe" -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART' -Wait -PassThru
        if ($p.ExitCode -ne 0) { throw 'Uninstaller failed' }
    }
    if (Get-Service VPN20 -ErrorAction SilentlyContinue) { throw 'Uninstaller left the VPN20 service behind' }
    if ($installed -and !(Test-Path "$data\vpn20.db")) { throw 'Uninstaller unexpectedly removed saved data' }
}

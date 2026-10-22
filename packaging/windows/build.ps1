param(
    [string]$Version = (Get-Content "$PSScriptRoot\VERSION" -Raw).Trim(),
    [ValidateSet('x64','x86')][string]$Architecture = 'x64'
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
& "$PSScriptRoot\prepare-signing.ps1" -Required:($env:VPN20_REQUIRE_SIGNING -eq 'true')
if ($Version -notmatch '^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$') { throw 'Invalid release version' }
$root = (Resolve-Path "$PSScriptRoot\..\..").Path
Set-Location $root
$env:GOOS = 'windows'
$env:GOARCH = @{x64='amd64'; x86='386'}[$Architecture]
$env:CGO_ENABLED = '0'
$stage = Join-Path $root 'dist\windows'
$out = Join-Path $root 'dist\release'
New-Item -ItemType Directory -Force $stage, $out | Out-Null

Push-Location web
try {
    npm ci --ignore-scripts --no-audit --no-fund
    if ($LASTEXITCODE) { throw 'npm ci failed' }
    npm run test:locale
    if ($LASTEXITCODE) { throw 'Frontend tests failed' }
    npm run build
    if ($LASTEXITCODE) { throw 'Frontend build failed' }
} finally { Pop-Location }

# Official signed Wintun provides the host's VPN interface in relay mode.
$wintunArchive = Join-Path $root 'dist\wintun-0.14.1.zip'
Invoke-WebRequest 'https://www.wintun.net/builds/wintun-0.14.1.zip' -OutFile $wintunArchive
if ((Get-FileHash $wintunArchive -Algorithm SHA256).Hash -ne '07C256185D6EE3652E09FA55C0B673E2624B565E02C4B9091C79CA7D2F24EF51') { throw 'Wintun download checksum mismatch' }
$wintunDirectory = Join-Path $root 'dist\wintun'
Expand-Archive $wintunArchive -DestinationPath $wintunDirectory -Force
$driverArchitecture = @{x64='amd64'; x86='x86'}[$Architecture]
$wintunDLL = Join-Path $wintunDirectory "wintun\bin\$driverArchitecture\wintun.dll"
if ((Get-AuthenticodeSignature $wintunDLL).Status -ne 'Valid') { throw 'Wintun Authenticode validation failed' }
Copy-Item $wintunDLL "$stage\wintun.dll"
$wintunLicense = Join-Path $stage 'licenses\Wintun'
New-Item -ItemType Directory -Force $wintunLicense | Out-Null
Copy-Item (Join-Path $wintunDirectory 'wintun\LICENSE.txt') $wintunLicense
$env:VPN20_WINTUN_DLL = "$stage\wintun.dll"

go vet ./...
if ($LASTEXITCODE) { throw 'go vet failed' }
go test -count=1 ./...
if ($LASTEXITCODE) { throw 'Go tests failed' }
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
if ($LASTEXITCODE) { throw 'Windows vulnerability scan failed' }
$env:CGO_ENABLED = '0'
go build -trimpath -ldflags "-s -w -X github.com/lehuunghi/vpn/internal/engine.Version=$Version" -o "$stage\vpn20.exe" ./cmd/vpn20
if ($LASTEXITCODE) { throw 'Windows executable build failed' }
if ($env:VPN20_SIGN_CERTIFICATE_THUMBPRINT) { & "$PSScriptRoot\sign.ps1" -Path "$stage\vpn20.exe" }

# Pin both the official WireGuardNT version and its publisher-supplied hash.
# Source: WireGuard/wireguard-windows build.bat, WireGuardNT 1.1.
$archive = Join-Path $root 'dist\wireguard-nt-1.1.zip'
Invoke-WebRequest 'https://download.wireguard.com/wireguard-nt/wireguard-nt-1.1.zip' -OutFile $archive
$expected = 'DCEB30A9BC4BE48CCE0F74160FC88A585A2C2627366E8F846FC6658F9038DACE'
if ((Get-FileHash $archive -Algorithm SHA256).Hash -ne $expected) { throw 'WireGuardNT download checksum mismatch' }
$unpacked = Join-Path $root 'dist\wireguard-nt'
Expand-Archive $archive -DestinationPath $unpacked -Force
$driverArchitecture = @{x64='amd64'; x86='x86'}[$Architecture]
$dll = @(Get-ChildItem $unpacked -Recurse -Filter wireguard.dll | Where-Object { $_.Directory.Name -eq $driverArchitecture })
if ($dll.Count -ne 1) { throw "The official archive did not contain one $driverArchitecture driver DLL" }
$signature = Get-AuthenticodeSignature $dll[0].FullName
if ($signature.Status -ne 'Valid') { throw "WireGuardNT Authenticode validation failed: $($signature.Status)" }
Copy-Item $dll[0].FullName "$stage\wireguard.dll"
$licenses = Join-Path $stage 'licenses\WireGuardNT'
New-Item -ItemType Directory -Force $licenses | Out-Null
$notices = @(Get-ChildItem $unpacked -Recurse -File | Where-Object { $_.Name -match '^(COPYING|LICENSE|NOTICE)(\..*)?$' })
if ($notices.Count -eq 0) { throw 'Missing WireGuardNT distribution license' }
$notices | ForEach-Object { Copy-Item $_.FullName (Join-Path $licenses $_.Name) }
Copy-Item LICENSE, NOTICE $stage
Copy-Item docs\windows.md "$stage\README-Windows.md"
Copy-Item docs\installation.md, docs\performance.md, docs\GHCR.md $stage
('Corresponding source: https://github.com/lehuunghi/vpn/tree/' + (git rev-parse HEAD)) | Set-Content "$stage\SOURCE.txt" -Encoding UTF8
Copy-Item packaging\windows\config.example.json $stage
Copy-Item web\public\favicon.ico "$stage\vpn20.ico"
$helper = (Get-Content internal\server\peer_access.ps1 -Raw).Replace('__VPN20_SUBNET__', '10.8.0.0/24')
[IO.File]::WriteAllText("$stage\Enable-VPN20-PeerAccess.ps1", $helper, [Text.UTF8Encoding]::new($true))
Copy-Item "$stage\Enable-VPN20-PeerAccess.ps1" $out
go version -m "$stage\vpn20.exe" | Set-Content "$stage\build-info.txt" -Encoding UTF8

# Include third-party license texts in binary packages as well as source.
go list -m -f '{{.Path}}|{{.Dir}}' all | ForEach-Object {
    $parts = $_ -split '\|', 2
    if ($parts.Count -eq 2 -and $parts[1]) {
        $target = Join-Path $stage ('licenses\Go\' + ($parts[0] -replace '[^a-zA-Z0-9_.-]', '_'))
        $texts = @(Get-ChildItem -LiteralPath $parts[1] -File | Where-Object { $_.Name -match '^(LICENSE|COPYING|NOTICE|AUTHORS|PATENTS|COPYRIGHT)' })
        if ($texts.Count) {
            New-Item -ItemType Directory -Force $target | Out-Null
            $texts | ForEach-Object { Copy-Item $_.FullName $target }
        }
    }
}
Get-ChildItem web/node_modules -Recurse -Filter package.json | ForEach-Object {
    $pkg = Get-Content -Raw -LiteralPath $_.FullName | ConvertFrom-Json
    if ($pkg.PSObject.Properties['name'] -and $pkg.PSObject.Properties['version']) {
        $target = Join-Path $stage ('licenses\npm\' + (($pkg.name + '-' + $pkg.version) -replace '[^a-zA-Z0-9_.-]', '_'))
        $texts = @(Get-ChildItem $_.Directory.FullName -File | Where-Object { $_.Name -match '^(LICENSE|COPYING|NOTICE|COPYRIGHT)' })
        if ($texts.Count) {
            New-Item -ItemType Directory -Force $target | Out-Null
            $texts | ForEach-Object { Copy-Item $_.FullName $target }
        }
    }
}

# Native tests must pass before an installer or release is published.
$env:VPN20_DRIVER_DLL = "$stage\wireguard.dll"
$env:VPN20_WINDOWS_INTEGRATION = '1'
try {
    go test -count=1 -v -timeout 5m ./internal/wg -run '^TestWindowsNativeTunnel$'
    if ($LASTEXITCODE) { throw 'Native WireGuardNT/WinNAT/WFP integration tests failed' }
} finally { Remove-Item Env:\VPN20_WINDOWS_INTEGRATION -ErrorAction SilentlyContinue }

$iscc = "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe"
if (!(Test-Path $iscc)) {
    choco install innosetup --yes --no-progress
    if ($LASTEXITCODE) { throw 'Inno Setup installation failed' }
}
$innoArgs = @("/DVersion=$Version", "/DArchitecture=$Architecture", "/DSourceDir=$stage", "/DOutputDir=$out")
if ($env:VPN20_SIGN_CERTIFICATE_THUMBPRINT) {
    $signScript = "$PSScriptRoot\sign.ps1"
    $innoArgs += '/DSignedBuild=1'
    $innoArgs += ('/Svpn20sign=powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $q' + $signScript + '$q -Path $f')
}
& $iscc @innoArgs "$PSScriptRoot\vpn20.iss"
if ($LASTEXITCODE) { throw 'Windows installer build failed' }
if ($env:VPN20_SIGN_CERTIFICATE_THUMBPRINT) {
    $signature = Get-AuthenticodeSignature "$out\VPN20-$Version-windows-$Architecture-setup.exe"
    if ($signature.Status -ne 'Valid' -or !$signature.TimeStamperCertificate) { throw 'Installer signature/timestamp verification failed' }
}
Compress-Archive -Path "$stage\*" -DestinationPath "$out\VPN20-$Version-windows-$Architecture.zip" -Force
Write-Output "Built Windows release $Version in $out"

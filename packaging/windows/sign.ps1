param([Parameter(Mandatory)][string]$Path)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if (!$env:VPN20_SIGN_CERTIFICATE_THUMBPRINT) { throw 'No code-signing certificate configured' }
$signtool = Get-Command signtool.exe -ErrorAction SilentlyContinue
if ($signtool) { $tool = $signtool.Source }
else {
    $sdk = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'
    $tool = @(Get-ChildItem $sdk -Filter signtool.exe -Recurse | Where-Object { $_.Directory.Name -eq 'x64' } | Sort-Object FullName -Descending | Select-Object -First 1).FullName
}
if (!$tool) { throw 'Install Windows SDK SignTool before signing a release' }
$timestamp = $env:VPN20_SIGN_TIMESTAMP_URL
if (!$timestamp) { $timestamp = 'https://timestamp.digicert.com' }
& $tool sign /sha1 $env:VPN20_SIGN_CERTIFICATE_THUMBPRINT /s My /fd SHA256 /tr $timestamp /td SHA256 /d 'VPN20' /du 'https://20.com.vn' $Path
if ($LASTEXITCODE) { throw 'Authenticode signing/timestamp failed' }
& $tool verify /pa /all $Path
if ($LASTEXITCODE) { throw 'Authenticode certificate-chain verification failed' }
$signature = Get-AuthenticodeSignature -LiteralPath $Path
if ($signature.Status -ne 'Valid' -or !$signature.TimeStamperCertificate -or $signature.SignerCertificate.Thumbprint -ne $env:VPN20_SIGN_CERTIFICATE_THUMBPRINT) { throw 'Signed file did not pass publisher/timestamp verification' }

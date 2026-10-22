param([switch]$Required)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if ($env:VPN20_SIGN_PFX_BASE64) {
    $pfx = Join-Path ([IO.Path]::GetTempPath()) ('vpn20-sign-' + [Guid]::NewGuid().ToString('N') + '.pfx')
    try {
        [IO.File]::WriteAllBytes($pfx, [Convert]::FromBase64String($env:VPN20_SIGN_PFX_BASE64))
        $password = ConvertTo-SecureString $env:VPN20_SIGN_PFX_PASSWORD -AsPlainText -Force
        $certs = @(Import-PfxCertificate -FilePath $pfx -CertStoreLocation Cert:\CurrentUser\My -Password $password | Where-Object { $_.HasPrivateKey })
        if ($certs.Count -ne 1) { throw 'The signing PFX must contain exactly one signing certificate with a private key' }
        $cert = $certs[0]
        $env:VPN20_SIGN_CERTIFICATE_THUMBPRINT = $cert.Thumbprint
    } finally { Remove-Item -LiteralPath $pfx -Force -ErrorAction SilentlyContinue }
}
if (!$env:VPN20_SIGN_CERTIFICATE_THUMBPRINT) {
    if ($Required) { throw 'Release signing is required. Configure a trusted code-signing certificate (VPN20_SIGN_PFX_BASE64/PASSWORD or VPN20_SIGN_CERTIFICATE_THUMBPRINT). No unsigned public installer will be published.' }
    Write-Output 'Development build: application and installer are unsigned. Official Wintun/WireGuardNT drivers retain their publisher signatures.'
    return
}
$cert = Get-Item ('Cert:\CurrentUser\My\' + $env:VPN20_SIGN_CERTIFICATE_THUMBPRINT)
if (!$cert.HasPrivateKey -or $cert.NotAfter -le (Get-Date) -or $cert.NotBefore -gt (Get-Date)) { throw 'Signing certificate/private key is missing or expired' }
if (!($cert.EnhancedKeyUsageList | Where-Object { $_.ObjectId.Value -eq '1.3.6.1.5.5.7.3.3' })) { throw 'Certificate is not valid for code signing' }
Write-Output ('Signing publisher: ' + $cert.Subject)

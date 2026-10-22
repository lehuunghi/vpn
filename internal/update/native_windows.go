//go:build windows

package update

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/svc/mgr"
)

func nativeAvailable(mode string) string {
	if mode != "windows" {
		return "Cập nhật tự động chưa bật cho kiểu cài đặt này."
	}
	m, err := mgr.Connect()
	if err != nil {
		return "Cần quyền quản trị để cập nhật dịch vụ VPN20."
	}
	defer m.Disconnect()
	s, err := m.OpenService("VPN20")
	if err != nil {
		return "Cài bằng bộ cài Windows để bật cập nhật tự động."
	}
	defer s.Close()
	c, err := s.Config()
	if err != nil {
		return err.Error()
	}
	exe, err := os.Executable()
	if err != nil {
		return err.Error()
	}
	if !strings.HasPrefix(strings.ToLower(c.BinaryPathName), strings.ToLower("\""+exe+"\"")) {
		return "Chương trình đang chạy không phải dịch vụ VPN20 đã cài."
	}
	return ""
}

func ps(ctx context.Context, script string) error {
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference='Stop'; "+script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Windows update: %w: %s", err, out)
	}
	return nil
}
func quotePS(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func installedSignature() *SigningInfo {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	script := "[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false); $s=Get-AuthenticodeSignature -LiteralPath " + quotePS(exe) + "; $publisher=''; if ($s.SignerCertificate) {$publisher=$s.SignerCertificate.Subject}; @{status=[string]$s.Status;publisher=$publisher;timestamped=($null -ne $s.TimeStamperCertificate)} | ConvertTo-Json -Compress"
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	b, err := cmd.Output()
	if err != nil {
		return &SigningInfo{Status: "Unknown"}
	}
	var info SigningInfo
	if json.Unmarshal(b, &info) != nil {
		return &SigningInfo{Status: "Unknown"}
	}
	return &info
}

func verifyNativePayload(ctx context.Context, path, thumbprint string) error {
	// Both callers have already verified the GitHub asset's SHA-256 digest.
	// Unsigned releases are supported unless the operator pins a certificate.
	script := "$s=Get-AuthenticodeSignature -LiteralPath " + quotePS(path)
	if thumbprint == "" {
		script += "; if ($s.Status -eq 'NotSigned') { exit 0 }"
	}
	script += "; if ($s.Status -ne 'Valid') { throw ('Chữ ký Authenticode không hợp lệ: ' + $s.Status) }; if (!$s.TimeStamperCertificate) { throw 'Bộ cài thiếu dấu thời gian ký số' }"
	if thumbprint != "" {
		script += "; if ($s.SignerCertificate.Thumbprint -ne " + quotePS(thumbprint) + ") { throw 'Chứng chỉ nhà phát hành không khớp cấu hình' }"
	}
	return ps(ctx, script)
}

func launchHelper(exe, plan, logPath string) error {
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.Command(exe, "apply-update", plan)
	cmd.Stdout = f
	cmd.Stderr = f
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000200 | 0x08000000}
	if err = cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func stopNative(ctx context.Context, p Plan) error {
	return ps(ctx, "$s=Get-Service VPN20; if ($s.Status -ne 'Stopped') { Stop-Service VPN20; $s.WaitForStatus('Stopped',[TimeSpan]::FromSeconds(90)) }")
}
func startNative(ctx context.Context, p Plan) error { return ps(ctx, "Start-Service VPN20") }
func backupNative(p Plan, dir string) error {
	for _, name := range []string{"vpn20.exe", "wintun.dll", "wireguard.dll"} {
		src := filepath.Join(filepath.Dir(p.Executable), name)
		if exists(src) {
			if err := copyFile(src, filepath.Join(dir, name), 0600); err != nil {
				return err
			}
		}
	}
	return nil
}
func restoreNative(p Plan, dir string) error {
	for _, name := range []string{"vpn20.exe", "wintun.dll", "wireguard.dll"} {
		src := filepath.Join(dir, name)
		if exists(src) {
			if err := copyFile(src, filepath.Join(filepath.Dir(p.Executable), name), 0600); err != nil {
				return err
			}
		}
	}
	return nil
}
func installNative(ctx context.Context, p Plan) error {
	cmd := exec.CommandContext(ctx, p.Payload, "/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/SP-", "/LOG="+filepath.Join(filepath.Dir(p.Payload), "installer.log"))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Windows installer: %w: %s", err, out)
	}
	return nil
}

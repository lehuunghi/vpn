//go:build linux

package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func nativeAvailable(mode string) string {
	if mode != "systemd" {
		return "Cập nhật tự động chưa bật cho kiểu cài đặt này."
	}
	if os.Geteuid() != 0 {
		return "Dịch vụ cần chạy với quyền root để cập nhật."
	}
	exe, err := os.Executable()
	if err != nil {
		return err.Error()
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil || exe != "/usr/local/bin/vpn20" || !exists("/run/systemd/system") || (!exists("/etc/systemd/system/vpn20.service") && !exists("/usr/lib/systemd/system/vpn20.service")) {
		return "Cài bằng packaging/linux/install.sh để bật cập nhật dịch vụ systemd."
	}
	return ""
}

func installedSignature() *SigningInfo { return nil }

func verifyNativePayload(context.Context, string, string) error { return nil } // GitHub digest verified before extraction.

func launchHelper(exe, plan, logPath string) error {
	// A mere detached child still belongs to the service's systemd cgroup and
	// would be killed when stopping vpn20. A transient unit survives that stop.
	unit := "vpn20-update-" + filepath.Base(filepath.Dir(plan))
	out, err := exec.Command("systemd-run", "--unit="+unit, "--collect", "--property=Type=exec", "--property=StandardOutput=append:"+logPath, "--property=StandardError=append:"+logPath, exe, "apply-update", plan).CombinedOutput()
	if err != nil {
		return fmt.Errorf("start update unit: %w: %s", err, out)
	}
	return nil
}

func serviceCommand(ctx context.Context, action string) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", action, "vpn20.service").CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", action, err, out)
	}
	return nil
}
func stopNative(ctx context.Context, p Plan) error  { return serviceCommand(ctx, "stop") }
func startNative(ctx context.Context, p Plan) error { return serviceCommand(ctx, "start") }
func backupNative(p Plan, dir string) error {
	return copyFile(p.Executable, filepath.Join(dir, "vpn20"), 0700)
}
func restoreNative(p Plan, dir string) error {
	return replaceBinary(filepath.Join(dir, "vpn20"), p.Executable)
}

func replaceBinary(src, dest string) error {
	tmp := dest + ".update-new"
	defer os.Remove(tmp)
	if err := copyFile(src, tmp, 0755); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func extractBinary(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	g, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer g.Close()
	r := tar.NewReader(io.LimitReader(g, 512<<20))
	found := false
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if h.Name != "vpn20" {
			continue
		}
		if found || h.Typeflag != tar.TypeReg || h.Size < 1 || h.Size > maxDownload {
			return errors.New("invalid binary archive entry")
		}
		found = true
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, r)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if !found {
		return errors.New("release archive has no vpn20 binary")
	}
	e, err := elf.Open(dest)
	if err != nil {
		return err
	}
	defer e.Close()
	want := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64, "386": elf.EM_386}[runtime.GOARCH]
	if e.Machine != want {
		return errors.New("release binary architecture does not match this host")
	}
	return nil
}

func installNative(ctx context.Context, p Plan) error {
	binary := filepath.Join(filepath.Dir(p.Payload), "vpn20-new")
	if err := extractBinary(p.Payload, binary); err != nil {
		return err
	}
	return replaceBinary(binary, p.Executable)
}

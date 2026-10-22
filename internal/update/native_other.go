//go:build !linux && !windows

package update

import (
	"context"
	"errors"
)

func installedSignature() *SigningInfo { return nil }

func nativeAvailable(string) string {
	return "Máy chủ VPN20 hỗ trợ cập nhật trên Windows và Linux/systemd hoặc Docker Linux."
}
func verifyNativePayload(context.Context, string, string) error {
	return errors.New("unsupported platform")
}
func launchHelper(string, string, string) error { return errors.New("unsupported platform") }
func stopNative(context.Context, Plan) error    { return errors.New("unsupported platform") }
func startNative(context.Context, Plan) error   { return errors.New("unsupported platform") }
func backupNative(Plan, string) error           { return errors.New("unsupported platform") }
func restoreNative(Plan, string) error          { return errors.New("unsupported platform") }
func installNative(context.Context, Plan) error { return errors.New("unsupported platform") }

//go:build windows

package update

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWindowsUnsignedUpdateAndCertificatePin(t *testing.T) {
	exe, err := os.Executable() // Go's test executable is unsigned.
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err = verifyNativePayload(ctx, exe, ""); err != nil {
		t.Fatalf("unsigned release rejected without a certificate pin: %v", err)
	}
	if err = verifyNativePayload(ctx, exe, strings.Repeat("A", 40)); err == nil {
		t.Fatal("unsigned release bypassed the operator's certificate pin")
	}
}

//go:build windows

package update

import (
	"context"
	"os"
	"path/filepath"
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

func TestWindowsJobStateSurvivesOpenReader(t *testing.T) {
	root := t.TempDir()
	if err := writeJob(root, Job{Phase: "checking"}); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	done := make(chan error, 1)
	go func() { done <- writeJob(root, Job{Phase: "complete"}) }()
	// Keep a genuine Windows read handle open across the first replace.
	time.Sleep(50 * time.Millisecond)
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("job state remained blocked after the reader closed")
	}
	if job := readJob(root); job.Phase != "complete" {
		t.Fatalf("lost committed job state: %+v", job)
	}
}

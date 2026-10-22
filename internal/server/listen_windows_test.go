//go:build windows

package server

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestWindowsListenWaitsForReleasedPort(t *testing.T) {
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	address := busy.Addr().String()
	timer := time.AfterFunc(150*time.Millisecond, func() { _ = busy.Close() })
	defer timer.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ln, err := listenAdmin(ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
}

func TestWindowsListenCancellation(t *testing.T) {
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	ln, err := listenAdmin(ctx, busy.Addr().String())
	if ln != nil {
		ln.Close()
		t.Fatal("occupied port was taken over")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("listener ignored cancellation: %v", err)
	}
}

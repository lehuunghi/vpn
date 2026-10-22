//go:build windows

package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/sys/windows"
)

func listenAdmin(ctx context.Context, address string) (net.Listener, error) {
	var lc net.ListenConfig
	deadline := time.Now().Add(10 * time.Second)
	for {
		ln, err := lc.Listen(ctx, "tcp", address)
		if err == nil {
			return ln, nil
		}
		// Windows can briefly retain a port during rapid service upgrades.
		// Keep normal socket ownership checks; never force SO_REUSEADDR or
		// remove another application's listener or reserved port ranges.
		if !errors.Is(err, windows.WSAEACCES) && !errors.Is(err, windows.WSAEADDRINUSE) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w; Windows refused the admin port for 10 seconds; check other listeners/reserved ports or change VPN20_HTTP_LISTEN in config.json", err)
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

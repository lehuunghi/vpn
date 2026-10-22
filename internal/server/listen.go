//go:build !windows

package server

import (
	"context"
	"net"
)

func listenAdmin(ctx context.Context, address string) (net.Listener, error) {
	var lc net.ListenConfig
	return lc.Listen(ctx, "tcp", address)
}

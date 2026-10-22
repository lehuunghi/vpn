//go:build !windows

package netcfg

import "context"

// Distribution firewalls are managed by the host administrator.
func AllowPublicHTTPS(ctx context.Context, address string) error { return nil }

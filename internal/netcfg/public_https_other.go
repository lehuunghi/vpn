//go:build !windows

package netcfg

import "context"

// Distribution firewalls are managed by the host administrator.
func AllowPublicHTTPS(ctx context.Context, address string) error { return nil }
func RemovePublicHTTPS(ctx context.Context) error                { return nil }

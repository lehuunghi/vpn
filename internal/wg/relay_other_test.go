//go:build !windows

package wg_test

import (
	"github.com/lehuunghi/vpn/internal/wg"
	"testing"
)

func prepareRelayTest(t *testing.T)                             {}
func prepareRelayNetwork(t *testing.T, be wg.Backend, port int) {}
func checkRelayHostAccess(t *testing.T, client relayClient)     {}

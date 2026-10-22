//go:build windows

package netcfg

import (
	"context"
	_ "embed"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tailscale/wf"
	"golang.org/x/sys/windows"
)

//go:embed windows_network.ps1
var networkScript string

var firewall struct {
	sync.Mutex
	session *wf.Session
}

// Windows has no Linux sysctls. Forwarding on the tunnel is configured through
// IP Helper; forwarding on the selected uplink is owned by the network script.
func Wanted(ipv6 bool) []Sysctl                { return nil }
func ApplyAll(want []Sysctl) ([]Result, error) { return nil, nil }

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func powershell(ctx context.Context, script string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	path := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, path, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference='Stop'; "+script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Windows networking: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func DefaultEgress() (string, error) {
	return powershell(context.Background(), `$r=Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' | Sort-Object @{Expression={$_.RouteMetric+$_.InterfaceMetric}} | Select-Object -First 1; if (!$r) { throw 'No IPv4 default route; connect the Internet uplink and restart VPN20' }; $r.InterfaceAlias`)
}

func networkStatePath() string {
	dir := os.Getenv("VPN20_DATA_DIR")
	if dir == "" {
		dir = filepath.Join(os.Getenv("ProgramData"), "VPN20")
	}
	return filepath.Join(dir, "network-state.json")
}

func Apply(ctx context.Context, r Rules) error {
	firewall.Lock()
	defer firewall.Unlock()
	if r.ClampMSS {
		return fmt.Errorf("TCP MSS rewriting is unavailable on Windows; disable it and use a suitable tunnel MTU")
	}
	if r.Egress == "" {
		return fmt.Errorf("Windows requires an Internet uplink; set VPN20_EGRESS_INTERFACE or provide an IPv4 default route")
	}
	iface, err := net.InterfaceByName(r.Iface)
	if err != nil {
		return err
	}
	// Build replacement filters before removing the old filters. Failure must
	// never silently turn isolation off. Dynamic objects disappear on a crash.
	session, err := wf.New(&wf.Options{Name: "VPN20 peer isolation", Dynamic: true})
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = session.Close()
		}
	}()
	if r.PeerIsolation {
		id, err := windows.GenerateGUID()
		if err != nil {
			return err
		}
		sub := wf.SublayerID(id)
		if err := session.AddSublayer(&wf.Sublayer{ID: sub, Name: "VPN20", Weight: 65535}); err != nil {
			return err
		}
		for _, layer := range []wf.LayerID{wf.LayerIPForwardV4, wf.LayerIPForwardV6} {
			id, err := windows.GenerateGUID()
			if err != nil {
				return err
			}
			err = session.AddRule(&wf.Rule{ID: wf.RuleID(id), Name: "VPN20 block peer-to-peer forwarding", Layer: layer, Sublayer: sub, Weight: 1, Action: wf.ActionBlock, Conditions: []*wf.Match{
				{Field: wf.FieldSourceInterfaceIndex, Op: wf.MatchTypeEqual, Value: uint32(iface.Index)},
				{Field: wf.FieldDestinationInterfaceIndex, Op: wf.MatchTypeEqual, Value: uint32(iface.Index)},
			}})
			if err != nil {
				return fmt.Errorf("install peer isolation: %w", err)
			}
		}
	}
	var subnet string
	for _, s := range r.Subnets {
		if s.Addr().Is4() {
			subnet = s.Masked().String()
			break
		}
	}
	if subnet == "" {
		return fmt.Errorf("an IPv4 VPN subnet is required for WinNAT")
	}
	_, err = powershell(ctx, networkScript+fmt.Sprintf("\nSet-VPN20Network -Tunnel %s -Uplink %s -Subnet %s -Port %d -StatePath %s", psQuote(r.Iface), psQuote(r.Egress), psQuote(subnet), r.ListenPort, psQuote(networkStatePath())))
	if err != nil {
		return err
	}
	if firewall.session != nil {
		_ = firewall.session.Close()
	}
	firewall.session = session
	ok = true
	return nil
}

func Remove(ctx context.Context, table string) error {
	firewall.Lock()
	defer firewall.Unlock()
	if firewall.session != nil {
		_ = firewall.session.Close()
		firewall.session = nil
	}
	_, err := powershell(ctx, networkScript+"\nRemove-VPN20Network -StatePath "+psQuote(networkStatePath()))
	return err
}

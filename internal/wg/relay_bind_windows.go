//go:build windows

package wg

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"

	"github.com/lehuunghi/vpn/internal/relay"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

func newRelayBind() conn.Bind { return conn.NewDefaultBind() }

func attachRelayHost(r *relay.Router, name string, addrs []netip.Prefix, mtu int) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	h, err := windows.LoadLibraryEx(filepath.Join(filepath.Dir(exe), "wintun.dll"), 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		return fmt.Errorf("load bundled Wintun: %w; reinstall VPN20 using the Windows installer", err)
	}
	_ = windows.FreeLibrary(h)
	host, err := tun.CreateTUN(name, mtu)
	if err != nil {
		return fmt.Errorf("create Wintun host adapter: %w (run as Administrator or use the installed service)", err)
	}
	luid := winipcfg.LUID(host.(*tun.NativeTun).LUID())
	ok := false
	defer func() {
		if !ok {
			_ = host.Close()
		}
	}()
	if err = luid.SetIPAddresses(addrs); err != nil {
		return fmt.Errorf("set relay host addresses: %w", err)
	}
	families := []winipcfg.AddressFamily{windows.AF_INET}
	for _, p := range addrs {
		if p.Addr().Is6() {
			families = append(families, windows.AF_INET6)
			break
		}
	}
	setMTU := func(value int) error {
		for _, family := range families {
			row, err := luid.IPInterface(family)
			if err != nil {
				return err
			}
			row.NLMTU = uint32(value)
			row.ForwardingEnabled = false
			row.RouterDiscoveryBehavior = winipcfg.RouterDiscoveryDisabled
			row.DadTransmits = 0
			if err = row.Set(); err != nil {
				return fmt.Errorf("configure relay host adapter: %w", err)
			}
		}
		return nil
	}
	if err = setMTU(mtu); err != nil {
		return err
	}
	r.AttachHost(host, setMTU)
	ok = true
	return nil
}

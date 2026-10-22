//go:build windows

package wg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/driver"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

// WireGuardNT keeps encryption in the Windows kernel. Only adapter control and
// counters cross into this process; the UI and database use the same engine.
type windowsBackend struct {
	mu       sync.Mutex
	name     string
	adapter  *driver.Adapter
	families []winipcfg.AddressFamily
}

func KernelAvailable() bool { return true }

func NewKernel(name string, log *slog.Logger) (Backend, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	// Fail with a useful error rather than letting the driver's lazy loader panic.
	h, err := windows.LoadLibraryEx(filepath.Join(filepath.Dir(exe), "wireguard.dll"), 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		return nil, fmt.Errorf("load bundled WireGuardNT: %w; reinstall VPN20 using the Windows installer", err)
	}
	_ = windows.FreeLibrary(h)
	return &windowsBackend{name: name}, nil
}

func NewUserspace(name string, log *slog.Logger) (Backend, error) {
	return nil, errors.New("Windows uses WireGuardNT; set VPN20_BACKEND=auto or kernel")
}

func (b *windowsBackend) Kind() string { return "wireguardnt" }

func (b *windowsBackend) Up(ctx context.Context, cfg DeviceConfig, addrs []netip.Prefix, mtu int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.adapter != nil {
		return errors.New("WireGuardNT adapter is already up")
	}
	a, err := driver.CreateAdapter(b.name, "WireGuard", nil)
	if err != nil {
		return fmt.Errorf("create WireGuardNT adapter: %w (run as Administrator or use the installed service)", err)
	}
	b.adapter = a
	ok := false
	defer func() {
		if !ok {
			_ = a.Close()
			b.adapter = nil
		}
	}()
	var builder driver.ConfigBuilder
	builder.AppendInterface(&driver.Interface{Flags: driver.InterfaceHasPrivateKey | driver.InterfaceHasListenPort | driver.InterfaceReplacePeers, PrivateKey: cfg.PrivateKey, ListenPort: uint16(cfg.ListenPort)})
	if err := b.configure(&builder); err != nil {
		return err
	}
	b.families = []winipcfg.AddressFamily{windows.AF_INET}
	for _, p := range addrs {
		if p.Addr().Is6() {
			b.families = append(b.families, windows.AF_INET6)
			break
		}
	}
	if err := a.LUID().SetIPAddresses(addrs); err != nil {
		return fmt.Errorf("set tunnel addresses: %w", err)
	}
	if err := b.setMTU(mtu); err != nil {
		return err
	}
	if err := a.SetAdapterState(driver.AdapterStateUp); err != nil {
		return err
	}
	ok = true
	return nil
}

func (b *windowsBackend) configure(builder *driver.ConfigBuilder) error {
	i, size := builder.Interface()
	err := b.adapter.SetConfiguration(i, size)
	runtime.KeepAlive(builder)
	return err
}

func (b *windowsBackend) setMTU(mtu int) error {
	for _, family := range b.families {
		row, err := b.adapter.LUID().IPInterface(family)
		if err != nil {
			return err
		}
		row.NLMTU = uint32(mtu)
		row.ForwardingEnabled = true
		row.RouterDiscoveryBehavior = winipcfg.RouterDiscoveryDisabled
		row.DadTransmits = 0
		if err := row.Set(); err != nil {
			return fmt.Errorf("configure tunnel IP interface: %w", err)
		}
	}
	return nil
}

func (b *windowsBackend) SetMTU(ctx context.Context, mtu int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.adapter == nil {
		return errors.New("adapter is down")
	}
	return b.setMTU(mtu)
}

func (b *windowsBackend) Down(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.adapter == nil {
		return nil
	}
	err := b.adapter.SetAdapterState(driver.AdapterStateDown)
	err = errors.Join(err, b.adapter.Close())
	b.adapter = nil
	return err
}

func (b *windowsBackend) Device(ctx context.Context) (*DeviceState, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.adapter == nil {
		return nil, errors.New("adapter is down")
	}
	d, err := b.adapter.Configuration()
	if err != nil {
		return nil, err
	}
	out := &DeviceState{Name: b.name, PublicKey: Key(d.PublicKey), ListenPort: int(d.ListenPort)}
	p := d.FirstPeer()
	for n := uint32(0); n < d.PeerCount; n++ {
		state := PeerState{PublicKey: Key(p.PublicKey), ReceiveBytes: int64(p.RxBytes), TransmitBytes: int64(p.TxBytes), PersistentKeepalive: time.Duration(p.PersistentKeepalive) * time.Second}
		if ep := p.Endpoint.AddrPort(); ep.IsValid() && ep.Port() != 0 {
			state.Endpoint = net.UDPAddrFromAddrPort(ep)
		}
		if p.LastHandshake != 0 {
			state.LastHandshake = time.Unix(int64(p.LastHandshake/10000000)-11644473600, int64(p.LastHandshake%10000000)*100)
		}
		ip := p.FirstAllowedIP()
		for j := uint32(0); j < p.AllowedIPsCount; j++ {
			var addr netip.Addr
			if ip.AddressFamily == windows.AF_INET {
				addr = netip.AddrFrom4([4]byte(ip.Address[:4]))
			} else {
				addr = netip.AddrFrom16(ip.Address)
			}
			state.AllowedIPs = append(state.AllowedIPs, netip.PrefixFrom(addr, int(ip.Cidr)))
			ip = ip.NextAllowedIP()
		}
		out.Peers = append(out.Peers, state)
		p = p.NextPeer()
	}
	runtime.KeepAlive(d)
	return out, nil
}

func appendWindowsPeer(builder *driver.ConfigBuilder, p PeerConfig) {
	peer := driver.Peer{Flags: driver.PeerHasPublicKey | driver.PeerHasPersistentKeepalive | driver.PeerReplaceAllowedIPs | driver.PeerHasPresharedKey, PublicKey: p.PublicKey, PersistentKeepalive: uint16(p.PersistentKeepalive / time.Second), AllowedIPsCount: uint32(len(p.AllowedIPs))}
	if p.PresharedKey != nil {
		peer.PresharedKey = *p.PresharedKey
	}
	builder.AppendPeer(&peer)
	for _, pfx := range p.AllowedIPs {
		ip := driver.AllowedIP{Cidr: uint8(pfx.Bits())}
		if pfx.Addr().Is4() {
			ip.AddressFamily = windows.AF_INET
			a := pfx.Addr().As4()
			copy(ip.Address[:4], a[:])
		} else {
			ip.AddressFamily = windows.AF_INET6
			ip.Address = pfx.Addr().As16()
		}
		builder.AppendAllowedIP(&ip)
	}
}

func (b *windowsBackend) SetPeer(ctx context.Context, p PeerConfig) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.adapter == nil {
		return errors.New("adapter is down")
	}
	var builder driver.ConfigBuilder
	builder.AppendInterface(&driver.Interface{PeerCount: 1})
	appendWindowsPeer(&builder, p)
	return b.configure(&builder)
}

func (b *windowsBackend) RemovePeer(ctx context.Context, pub Key) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.adapter == nil {
		return errors.New("adapter is down")
	}
	var builder driver.ConfigBuilder
	builder.AppendInterface(&driver.Interface{PeerCount: 1})
	builder.AppendPeer(&driver.Peer{Flags: driver.PeerHasPublicKey | driver.PeerRemove, PublicKey: pub})
	return b.configure(&builder)
}

func (b *windowsBackend) ReplacePeers(ctx context.Context, peers []PeerConfig) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.adapter == nil {
		return errors.New("adapter is down")
	}
	var builder driver.ConfigBuilder
	builder.AppendInterface(&driver.Interface{Flags: driver.InterfaceReplacePeers, PeerCount: uint32(len(peers))})
	for _, p := range peers {
		appendWindowsPeer(&builder, p)
	}
	return b.configure(&builder)
}

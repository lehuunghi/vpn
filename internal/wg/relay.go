package wg

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lehuunghi/vpn/internal/relay"
	"golang.zx2c4.com/wireguard/device"
)

type relayBackend struct {
	mu     sync.Mutex
	name   string
	log    *slog.Logger
	router *relay.Router
	device *device.Device
	key    Key
	peers  map[Key][]netip.Prefix
}

// NewRelay uses host sockets for outbound traffic and Wintun for host access on Windows. Linux
// keeps its kernel backend; this constructor also permits unprivileged Linux tests
// of the exact Windows packet path with real encrypted WireGuard clients.
func NewRelay(name string, log *slog.Logger) (Backend, error) {
	return &relayBackend{name: name, log: log, peers: map[Key][]netip.Prefix{}}, nil
}

func (b *relayBackend) Kind() string { return "relay" }

func (b *relayBackend) Up(ctx context.Context, cfg DeviceConfig, addrs []netip.Prefix, mtu int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.device != nil {
		return errors.New("relay is already running")
	}
	router, err := relay.New(b.name, addrs, mtu)
	if err != nil {
		return err
	}
	if err = attachRelayHost(router, b.name, addrs, mtu); err != nil {
		_ = router.Close()
		return err
	}
	logger := &device.Logger{
		Verbosef: func(format string, args ...any) { b.log.Debug(fmt.Sprintf(format, args...)) },
		Errorf:   func(format string, args ...any) { b.log.Error(fmt.Sprintf(format, args...)) },
	}
	d := device.NewDevice(router, newRelayBind(), logger)
	text := fmt.Sprintf("private_key=%x\nlisten_port=%d\nreplace_peers=true\n", cfg.PrivateKey[:], cfg.ListenPort)
	if err = d.IpcSet(text); err == nil {
		err = d.Up()
	}
	if err != nil {
		d.Close()
		return fmt.Errorf("start WireGuard relay: %w", err)
	}
	b.router, b.device, b.key = router, d, cfg.PrivateKey
	return nil
}

func (b *relayBackend) Down(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.device != nil {
		b.device.Close()
		b.device = nil
		b.router = nil
	}
	b.peers = map[Key][]netip.Prefix{}
	return nil
}

func (b *relayBackend) Device(ctx context.Context) (*DeviceState, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.device == nil {
		return nil, errors.New("relay is down")
	}
	text, err := b.device.IpcGet()
	if err != nil {
		return nil, err
	}
	state := &DeviceState{Name: b.name, PublicKey: b.key.PublicKey()}
	var peer *PeerState
	var seconds, nanos int64
	finish := func() {
		if peer != nil && seconds != 0 {
			peer.LastHandshake = time.Unix(seconds, nanos)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "listen_port":
			state.ListenPort, _ = strconv.Atoi(value)
		case "public_key":
			finish()
			seconds, nanos = 0, 0
			decoded, err := hex.DecodeString(value)
			if err != nil || len(decoded) != 32 {
				return nil, errors.New("invalid relay peer state")
			}
			state.Peers = append(state.Peers, PeerState{PublicKey: Key(decoded)})
			peer = &state.Peers[len(state.Peers)-1]
		default:
			if peer == nil {
				continue
			}
			switch key {
			case "endpoint":
				if ep, err := netip.ParseAddrPort(value); err == nil {
					peer.Endpoint = net.UDPAddrFromAddrPort(ep)
				}
			case "rx_bytes":
				peer.ReceiveBytes, _ = strconv.ParseInt(value, 10, 64)
			case "tx_bytes":
				peer.TransmitBytes, _ = strconv.ParseInt(value, 10, 64)
			case "last_handshake_time_sec":
				seconds, _ = strconv.ParseInt(value, 10, 64)
			case "last_handshake_time_nsec":
				nanos, _ = strconv.ParseInt(value, 10, 64)
			case "persistent_keepalive_interval":
				n, _ := strconv.Atoi(value)
				peer.PersistentKeepalive = time.Duration(n) * time.Second
			case "allowed_ip":
				if p, err := netip.ParsePrefix(value); err == nil {
					peer.AllowedIPs = append(peer.AllowedIPs, p)
				}
			}
		}
	}
	finish()
	return state, nil
}

func relayPeerConfig(p PeerConfig) (string, error) {
	var s strings.Builder
	fmt.Fprintf(&s, "public_key=%x\nreplace_allowed_ips=true\npersistent_keepalive_interval=%d\n", p.PublicKey[:], int(p.PersistentKeepalive/time.Second))
	psk := Key{}
	if p.PresharedKey != nil {
		psk = *p.PresharedKey
	}
	fmt.Fprintf(&s, "preshared_key=%x\n", psk[:])
	for _, prefix := range p.AllowedIPs {
		if !prefix.IsValid() || prefix.Bits() != prefix.Addr().BitLen() {
			return "", errors.New("relay peers require individual /32 or /128 addresses")
		}
		fmt.Fprintf(&s, "allowed_ip=%s\n", prefix)
	}
	return s.String(), nil
}

func (b *relayBackend) syncPeers() {
	var addrs []netip.Addr
	for _, prefixes := range b.peers {
		for _, p := range prefixes {
			addrs = append(addrs, p.Addr())
		}
	}
	b.router.SetPeers(addrs)
}

func (b *relayBackend) SetPeer(ctx context.Context, p PeerConfig) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.device == nil {
		return errors.New("relay is down")
	}
	text, err := relayPeerConfig(p)
	if err != nil {
		return err
	}
	if err = b.device.IpcSet(text); err != nil {
		return err
	}
	b.peers[p.PublicKey] = append([]netip.Prefix(nil), p.AllowedIPs...)
	b.syncPeers()
	return nil
}

func (b *relayBackend) RemovePeer(ctx context.Context, pub Key) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.device == nil {
		return errors.New("relay is down")
	}
	if err := b.device.IpcSet(fmt.Sprintf("public_key=%x\nremove=true\n", pub[:])); err != nil {
		return err
	}
	delete(b.peers, pub)
	b.syncPeers()
	return nil
}

func (b *relayBackend) ReplacePeers(ctx context.Context, peers []PeerConfig) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.device == nil {
		return errors.New("relay is down")
	}
	text := "replace_peers=true\n"
	for _, p := range peers {
		part, err := relayPeerConfig(p)
		if err != nil {
			return err
		}
		text += part
	}
	if err := b.device.IpcSet(text); err != nil {
		return err
	}
	b.peers = map[Key][]netip.Prefix{}
	for _, p := range peers {
		b.peers[p.PublicKey] = append([]netip.Prefix(nil), p.AllowedIPs...)
	}
	b.syncPeers()
	return nil
}

func (b *relayBackend) SetMTU(ctx context.Context, mtu int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.router == nil {
		return errors.New("relay is down")
	}
	return b.router.SetMTU(mtu)
}

func (b *relayBackend) SetForwarding(ctx context.Context, cfg ForwardingConfig) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.router == nil {
		return errors.New("relay is down")
	}
	return b.router.SetPolicy(relay.Policy{PeerIsolation: cfg.PeerIsolation, ClampMSS: cfg.ClampMSS, Egress: cfg.Egress})
}

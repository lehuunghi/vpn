package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.zx2c4.com/wireguard/tun"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

const maxSessions = 4096

type Policy struct {
	PeerIsolation, ClampMSS bool
	Egress                  string
}

type policyState struct {
	Policy
	local4, local6 netip.Addr
}

type session struct {
	source        netip.Addr
	local, remote net.Conn
}

func (s *session) close() { _ = s.local.Close(); _ = s.remote.Close() }

// Router implements a TUN in memory. No host route to a VPN subnet is needed:
// WireGuard supplies authenticated packets, netstack terminates TCP/UDP, and
// normal host sockets provide the LAN/Internet side of each connection.
type Router struct {
	name      string
	addresses map[netip.Addr]bool
	subnets   []netip.Prefix
	stack     *stack.Stack
	ep        *channel.Endpoint
	ctx       context.Context
	cancel    context.CancelFunc
	events    chan tun.Event
	out       chan []byte
	mtu       atomic.Int32
	policy    atomic.Pointer[policyState]
	peers     atomic.Pointer[map[netip.Addr]bool]
	mu        sync.Mutex
	closed    bool
	sessions  map[*session]bool
	workers   sync.WaitGroup
	slots     chan struct{}
	pingSlots chan struct{}
	// Replaced only by tests before packets are injected.
	ping     func(context.Context, netip.Addr, netip.Addr, []byte) ([]byte, error)
	hostDial func(*net.Dialer, string, string) (net.Conn, error)
	host     tun.Device
	hostMTU  func(int) error
}

func New(name string, addrs []netip.Prefix, mtu int) (*Router, error) {
	if mtu < 1280 || mtu > 9000 {
		return nil, errors.New("relay MTU must be 1280-9000")
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Router{name: name, ctx: ctx, cancel: cancel, addresses: map[netip.Addr]bool{}, events: make(chan tun.Event, 2), out: make(chan []byte, 1024), sessions: map[*session]bool{}, slots: make(chan struct{}, maxSessions), pingSlots: make(chan struct{}, 64), ping: pingHost}
	r.mtu.Store(int32(mtu))
	r.policy.Store(&policyState{Policy: Policy{ClampMSS: true}})
	empty := map[netip.Addr]bool{}
	r.peers.Store(&empty)
	r.stack = stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol4, icmp.NewProtocol6},
	})
	r.ep = channel.New(1024, uint32(mtu), "")
	ok := false
	defer func() {
		if !ok {
			_ = r.Close()
		}
	}()
	if err := r.stack.CreateNIC(1, r.ep); err != nil {
		return nil, fmt.Errorf("relay NIC: %s", err)
	}
	for _, addr := range addrs {
		if !addr.IsValid() {
			return nil, errors.New("invalid relay address")
		}
		r.addresses[addr.Addr()] = true
		r.subnets = append(r.subnets, addr.Masked())
		protocol := ipv4.ProtocolNumber
		if addr.Addr().Is6() {
			protocol = ipv6.ProtocolNumber
		}
		pa := tcpip.ProtocolAddress{Protocol: protocol, AddressWithPrefix: tcpip.AddrFromSlice(addr.Addr().AsSlice()).WithPrefix()}
		if err := r.stack.AddProtocolAddress(1, pa, stack.AddressProperties{}); err != nil {
			return nil, fmt.Errorf("relay address: %s", err)
		}
	}
	if err := r.stack.SetPromiscuousMode(1, true); err != nil {
		return nil, fmt.Errorf("relay promiscuous mode: %s", err)
	}
	if err := r.stack.SetSpoofing(1, true); err != nil {
		return nil, fmt.Errorf("relay source addresses: %s", err)
	}
	r.stack.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: 1}, {Destination: header.IPv6EmptySubnet, NIC: 1}})
	sack := tcpip.TCPSACKEnabled(true)
	if err := r.stack.SetTransportProtocolOption(tcp.ProtocolNumber, &sack); err != nil {
		return nil, fmt.Errorf("relay SACK: %s", err)
	}
	r.stack.SetTransportProtocolHandler(tcp.ProtocolNumber, tcp.NewForwarder(r.stack, 0, 128, r.forwardTCP).HandlePacket)
	r.stack.SetTransportProtocolHandler(udp.ProtocolNumber, r.forwardUDP)
	r.workers.Add(1)
	go func() { defer r.workers.Done(); r.pump() }()
	ok = true
	return r, nil
}

func (r *Router) SetPolicy(p Policy) error {
	s := &policyState{Policy: p}
	if p.Egress != "" {
		iface, err := net.InterfaceByName(p.Egress)
		if err != nil {
			return fmt.Errorf("relay egress: %w", err)
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return err
		}
		for _, a := range addrs {
			prefix, err := netip.ParsePrefix(a.String())
			if err != nil || !prefix.Addr().IsGlobalUnicast() {
				continue
			}
			if prefix.Addr().Is4() && !s.local4.IsValid() {
				s.local4 = prefix.Addr()
			}
			if prefix.Addr().Is6() && !s.local6.IsValid() {
				s.local6 = prefix.Addr()
			}
		}
		if !s.local4.IsValid() && !s.local6.IsValid() {
			return errors.New("relay egress has no usable IP address")
		}
	}
	r.policy.Store(s)
	return nil
}

// SetPeers also revokes existing relayed connections when a device is disabled,
// expired, removed, or has its key replaced. Subnet membership alone is not auth.
func (r *Router) SetPeers(peers []netip.Addr) {
	current := make(map[netip.Addr]bool, len(peers))
	for _, a := range peers {
		current[a] = true
	}
	r.peers.Store(&current)
	r.mu.Lock()
	defer r.mu.Unlock()
	for s := range r.sessions {
		if !current[s.source] {
			s.close()
		}
	}
}

func (r *Router) File() *os.File           { return nil }
func (r *Router) Name() (string, error)    { return r.name, nil }
func (r *Router) MTU() (int, error)        { return int(r.mtu.Load()), nil }
func (r *Router) Events() <-chan tun.Event { return r.events }
func (r *Router) BatchSize() int           { return 1 }

func (r *Router) SetMTU(mtu int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return os.ErrClosed
	}
	if mtu < 1280 || mtu > 9000 {
		return errors.New("relay MTU must be 1280-9000")
	}
	if r.hostMTU != nil {
		if err := r.hostMTU(mtu); err != nil {
			return err
		}
	}
	if err := r.stack.SetNICMTU(1, uint32(mtu)); err != nil {
		return fmt.Errorf("relay MTU: %s", err)
	}
	r.mtu.Store(int32(mtu))
	select {
	case r.events <- tun.EventMTUUpdate:
	default:
	}
	return nil
}

func (r *Router) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	for {
		select {
		case <-r.ctx.Done():
			return 0, os.ErrClosed
		case p := <-r.out:
			i, ok := inspectPacket(p)
			if !ok || !(*r.peers.Load())[i.dst] {
				continue
			}
			if len(bufs) == 0 || offset < 0 || offset+len(p) > len(bufs[0]) {
				return 0, io.ErrShortBuffer
			}
			sizes[0] = copy(bufs[0][offset:], p)
			return 1, nil
		}
	}
}

func (r *Router) Write(bufs [][]byte, offset int) (int, error) {
	if r.ctx.Err() != nil {
		return 0, os.ErrClosed
	}
	for _, b := range bufs {
		if offset < 0 || offset >= len(b) {
			continue
		}
		p := b[offset:]
		i, ok := inspectPacket(p)
		if !ok || !(*r.peers.Load())[i.src] {
			continue
		}
		p = append([]byte(nil), p[:i.length]...)
		policy := r.policy.Load()
		if r.addresses[i.dst] && r.host != nil {
			// Only packets addressed to the host enter its IP stack. Foreign
			// destinations stay in the relay and never require Windows forwarding.
			_, _ = r.host.Write([][]byte{p}, 0)
			continue
		}
		if (*r.peers.Load())[i.dst] {
			if policy.PeerIsolation {
				continue
			}
			if !decrementTTL(p, i) {
				continue
			}
			if policy.ClampMSS {
				clampMSS(p, i, int(r.mtu.Load()))
			}
			r.emit(p)
			continue
		}
		// Unknown tunnel addresses must never fall through to host routing.
		if r.inSubnet(i.dst) && !r.addresses[i.dst] {
			continue
		}
		if !i.dst.IsGlobalUnicast() || i.dst.IsLoopback() || i.dst.IsLinkLocalUnicast() {
			continue
		}
		// Promiscuous netstack would answer a reassembled foreign echo locally.
		// Only TCP/UDP fragments may enter it; external ICMP uses the real host
		// echo API and never fabricates a reply for an arbitrary destination.
		if i.fragment {
			protocol := i.protocol
			if protocol == 44 {
				protocol = p[i.offset]
			}
			if protocol != 6 && protocol != 17 {
				continue
			}
		}
		if !i.fragment && i.offset+8 <= i.length && ((i.protocol == 1 && p[i.offset] == 8) || (i.protocol == 58 && p[i.offset] == 128)) {
			r.echo(p, i)
			continue
		}
		pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(p)})
		protocol := header.IPv4ProtocolNumber
		if i.src.Is6() {
			protocol = header.IPv6ProtocolNumber
		}
		r.ep.InjectInbound(protocol, pkt)
		pkt.DecRef()
	}
	return len(bufs), nil
}

func decrementTTL(p []byte, i packetInfo) bool {
	pos := 8
	if i.src.Is6() {
		pos = 7
	}
	if p[pos] <= 1 {
		return false
	}
	p[pos]--
	if i.src.Is4() {
		p[10], p[11] = 0, 0
		binaryPutChecksum(p)
	}
	return true
}

func binaryPutChecksum(p []byte) {
	sum := checksum(p[:int(p[0]&15)*4])
	p[10], p[11] = byte(sum>>8), byte(sum)
}

func (r *Router) inSubnet(a netip.Addr) bool {
	for _, p := range r.subnets {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func (r *Router) emit(p []byte) {
	select {
	case r.out <- p:
	default:
	} // bounded queue: never stall WireGuard workers
}

func (r *Router) pump() {
	for {
		pkt := r.ep.ReadContext(r.ctx)
		if pkt == nil {
			return
		}
		view := pkt.ToView()
		p := append([]byte(nil), view.AsSlice()...)
		view.Release()
		pkt.DecRef()
		r.emit(p)
	}
}

func (r *Router) begin() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	select {
	case r.slots <- struct{}{}:
	default:
		return false
	}
	r.workers.Add(1)
	return true
}

func (r *Router) finish() { <-r.slots; r.workers.Done() }

func (r *Router) dial(network string, dst netip.Addr, port uint16) (net.Conn, error) {
	p := r.policy.Load()
	// Expose services bound to a host network address, just as a real tunnel
	// would. Mapping the virtual address to loopback would expose localhost-only
	// services (including the private admin listener) to every VPN device.
	if r.addresses[dst] {
		local := p.local4
		if dst.Is6() {
			local = p.local6
		}
		if !local.IsValid() {
			local = hostAddress(dst.Is6())
		}
		if !local.IsValid() {
			return nil, errors.New("host has no address for this IP family; use a peer VPN address")
		}
		dst = local
	}
	d := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	local := p.local4
	if dst.Is6() {
		local = p.local6
	}
	if p.Egress != "" && !dst.IsLoopback() {
		if !local.IsValid() {
			return nil, errors.New("selected relay egress does not have this IP family")
		}
		if network == "tcp" {
			d.LocalAddr = &net.TCPAddr{IP: net.IP(local.AsSlice())}
		} else {
			d.LocalAddr = &net.UDPAddr{IP: net.IP(local.AsSlice())}
		}
	}
	family := "4"
	if dst.Is6() {
		family = "6"
	}
	address := net.JoinHostPort(dst.String(), strconv.Itoa(int(port)))
	if r.hostDial != nil {
		return r.hostDial(&d, network+family, address)
	}
	return d.DialContext(r.ctx, network+family, address)
}

func hostAddress(ipv6 bool) netip.Addr {
	// UDP connect selects a source address without sending a packet. Prefer
	// the default route before examining other adapters on a multihomed host.
	network, probe := "udp4", "192.0.2.1:9"
	if ipv6 {
		network, probe = "udp6", "[2001:db8::1]:9"
	}
	if c, err := net.Dial(network, probe); err == nil {
		a, _ := netip.AddrFromSlice(c.LocalAddr().(*net.UDPAddr).IP)
		_ = c.Close()
		if a.IsGlobalUnicast() {
			return a.Unmap()
		}
	}
	addrs, _ := net.InterfaceAddrs()
	for _, address := range addrs {
		p, err := netip.ParsePrefix(address.String())
		if err == nil && p.Addr().Is6() == ipv6 && p.Addr().IsGlobalUnicast() && !p.Addr().IsLinkLocalUnicast() {
			return p.Addr()
		}
	}
	return netip.Addr{}
}

func addr(a tcpip.Address) netip.Addr { v, _ := netip.AddrFromSlice(a.AsSlice()); return v }

func (r *Router) forwardTCP(req *tcp.ForwarderRequest) {
	if !r.begin() {
		req.Complete(true)
		return
	}
	defer r.finish()
	completed := false
	defer func() {
		if !completed {
			req.Complete(true)
		}
	}()
	id := req.ID()
	if !(*r.peers.Load())[addr(id.RemoteAddress)] {
		return
	}
	var q waiter.Queue
	ep, e := req.CreateEndpoint(&q)
	if e != nil {
		return
	}
	req.Complete(false)
	completed = true
	local := gonet.NewTCPConn(&q, ep)
	defer local.Close()
	// Finish the client handshake before opening a host connection. A peer
	// revoked during SYN/SYN-ACK must not leave an untracked upstream socket
	// waiting for an ACK that WireGuard will now correctly discard.
	if !(*r.peers.Load())[addr(id.RemoteAddress)] || r.ctx.Err() != nil {
		return
	}
	remote, err := r.dial("tcp", addr(id.LocalAddress), id.LocalPort)
	if err != nil {
		return
	}
	defer remote.Close()
	r.copySession(&session{source: addr(id.RemoteAddress), local: local, remote: remote}, false)
}

func (r *Router) forwardUDP(id stack.TransportEndpointID, pkt *stack.PacketBuffer) bool {
	if !r.begin() {
		return true
	}
	// Use the public endpoint API instead of retaining a ForwarderRequest's
	// packet clone for the lifetime of every UDP flow.
	var q waiter.Queue
	ep, err := r.stack.NewEndpoint(udp.ProtocolNumber, pkt.NetworkProtocolNumber, &q)
	if err != nil {
		r.finish()
		return true
	}
	ep.SocketOptions().SetReuseAddress(true)
	ep.SocketOptions().SetReusePort(true)
	if err = ep.Bind(tcpip.FullAddress{NIC: 1, Addr: id.LocalAddress, Port: id.LocalPort}); err == nil {
		err = ep.Connect(tcpip.FullAddress{NIC: 1, Addr: id.RemoteAddress, Port: id.RemotePort})
	}
	if err != nil {
		ep.Close()
		r.finish()
		return true
	}
	ep.(stack.TransportEndpoint).HandlePacket(id, pkt)
	local := gonet.NewUDPConn(&q, ep)
	go func() {
		defer r.finish()
		defer local.Close()
		remote, err := r.dial("udp", addr(id.LocalAddress), id.LocalPort)
		if err != nil {
			return
		}
		defer remote.Close()
		r.copySession(&session{source: addr(id.RemoteAddress), local: local, remote: remote}, true)
	}()
	return true
}

func (r *Router) copySession(s *session, datagram bool) {
	r.mu.Lock()
	if r.closed || !(*r.peers.Load())[s.source] {
		r.mu.Unlock()
		return
	}
	r.sessions[s] = true
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.sessions, s); r.mu.Unlock(); s.close() }()
	// Long idle RDP/SSH sessions must survive normal work breaks. Host TCP
	// keepalives detect a dead uplink; revocation and the session limit bound
	// retained state independently of application traffic.
	idle := 24 * time.Hour
	if datagram {
		idle = 2 * time.Minute
	}
	touch := func() {
		deadline := time.Now().Add(idle)
		_ = s.local.SetDeadline(deadline)
		_ = s.remote.SetDeadline(deadline)
	}
	touch()
	copyOne := func(dst, src net.Conn) {
		buf := make([]byte, 65535)
		for {
			n, err := src.Read(buf)
			if n > 0 || (datagram && err == nil) {
				touch()
				written, e := dst.Write(buf[:n])
				if e != nil || written != n {
					s.close()
					return
				}
			}
			if err != nil {
				if !datagram && errors.Is(err, io.EOF) {
					if c, ok := dst.(interface{ CloseWrite() error }); ok {
						_ = c.CloseWrite()
						return
					}
				}
				s.close()
				return
			}
		}
	}
	done := make(chan struct{})
	go func() { defer close(done); copyOne(s.remote, s.local) }()
	copyOne(s.local, s.remote)
	<-done
}

func (r *Router) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.cancel()
	for s := range r.sessions {
		s.close()
	}
	r.mu.Unlock()
	if r.host != nil {
		_ = r.host.Close()
	}
	r.stack.Close()
	r.stack.RemoveNIC(1)
	r.ep.Close()
	r.workers.Wait()
	r.stack.Wait()
	close(r.events)
	return nil
}

// AttachHost gives Windows applications a real VPN interface, so the server
// can initiate connections to peers and services can bind the VPN address.
// Call it before handing the router to WireGuard.
func (r *Router) AttachHost(host tun.Device, setMTU func(int) error) {
	r.host, r.hostMTU = host, setMTU
	r.workers.Add(1)
	go func() {
		defer r.workers.Done()
		buf := make([]byte, 65535)
		for {
			sizes := []int{0}
			_, err := host.Read([][]byte{buf}, sizes, 0)
			if err != nil {
				return
			}
			r.emit(append([]byte(nil), buf[:sizes[0]]...))
		}
	}()
}

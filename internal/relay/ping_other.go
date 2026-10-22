//go:build !windows

package relay

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"net"
	"net/netip"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

func pingHost(ctx context.Context, source, destination netip.Addr, data []byte) ([]byte, error) {
	protocol, network, bind := 1, "ip4:icmp", "0.0.0.0"
	var request, response icmp.Type = ipv4.ICMPTypeEcho, ipv4.ICMPTypeEchoReply
	if destination.Is6() {
		protocol, network, bind, request, response = 58, "ip6:ipv6-icmp", "::", ipv6.ICMPTypeEchoRequest, ipv6.ICMPTypeEchoReply
	}
	if source.IsValid() {
		bind = source.String()
	}
	c, err := icmp.ListenPacket(network, bind)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	deadline := time.Now().Add(3 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.SetDeadline(deadline)
	var nonce [4]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	id, seq := int(binary.BigEndian.Uint16(nonce[:2])), int(binary.BigEndian.Uint16(nonce[2:]))
	packet, err := (&icmp.Message{Type: request, Body: &icmp.Echo{ID: id, Seq: seq, Data: data}}).Marshal(nil)
	if err != nil {
		return nil, err
	}
	if _, err = c.WriteTo(packet, &net.IPAddr{IP: net.IP(destination.AsSlice())}); err != nil {
		return nil, err
	}
	buf := make([]byte, 65535)
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		n, from, err := c.ReadFrom(buf)
		if err != nil {
			return nil, err
		}
		if from.String() != destination.String() {
			continue
		}
		msg, err := icmp.ParseMessage(protocol, buf[:n])
		if err != nil || msg.Type != response {
			continue
		}
		body, ok := msg.Body.(*icmp.Echo)
		if ok && body.ID == id && body.Seq == seq && bytes.Equal(body.Data, data) {
			return body.Data, nil
		}
	}
}

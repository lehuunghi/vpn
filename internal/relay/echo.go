package relay

import (
	"context"
	"encoding/binary"
	"time"
)

func (r *Router) echo(p []byte, i packetInfo) {
	msg := p[i.offset:i.length]
	if msg[1] != 0 {
		return
	}
	valid := checksum(msg) == 0
	if i.src.Is6() {
		valid = transportChecksum(i.src, i.dst, 58, msg) == 0
	}
	if !valid {
		return
	}
	select {
	case r.pingSlots <- struct{}{}:
	default:
		return
	}
	if !r.begin() {
		<-r.pingSlots
		return
	}
	go func() {
		defer r.finish()
		defer func() { <-r.pingSlots }()
		payload := msg[8:]
		if !r.addresses[i.dst] {
			policy := r.policy.Load()
			local := policy.local4
			if i.dst.Is6() {
				local = policy.local6
			}
			if policy.Egress != "" && !local.IsValid() {
				return
			}
			ctx, cancel := context.WithTimeout(r.ctx, 3*time.Second)
			defer cancel()
			var err error
			payload, err = r.ping(ctx, local, i.dst, payload)
			if err != nil {
				return
			} // Never invent replies for an unreachable host.
		}
		if !(*r.peers.Load())[i.src] || r.ctx.Err() != nil {
			return
		}
		reply := append([]byte(nil), msg[:8]...)
		reply = append(reply, payload...)
		reply[0], reply[2], reply[3] = 0, 0, 0
		if i.src.Is6() {
			reply[0] = 129
			binary.BigEndian.PutUint16(reply[2:], transportChecksum(i.dst, i.src, 58, reply))
		} else {
			binary.BigEndian.PutUint16(reply[2:], checksum(reply))
		}
		r.emit(ipPacket(i.dst, i.src, i.protocol, reply))
	}()
}

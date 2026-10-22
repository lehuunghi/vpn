// Package relay routes decrypted WireGuard packets between peers and relays
// outbound TCP, UDP and ICMP echo through the host's own network connections.
package relay

import (
	"encoding/binary"
	"net/netip"
)

type packetInfo struct {
	src, dst       netip.Addr
	protocol       byte
	offset, length int
	fragment       bool
}

func inspectPacket(p []byte) (packetInfo, bool) {
	var i packetInfo
	if len(p) < 20 {
		return i, false
	}
	switch p[0] >> 4 {
	case 4:
		i.length = int(binary.BigEndian.Uint16(p[2:]))
		i.offset = int(p[0]&15) * 4
		if i.offset < 20 || i.length < i.offset || i.length > len(p) || checksum(p[:i.offset]) != 0 {
			return i, false
		}
		i.src, i.dst = netip.AddrFrom4([4]byte(p[12:16])), netip.AddrFrom4([4]byte(p[16:20]))
		i.protocol, i.fragment = p[9], binary.BigEndian.Uint16(p[6:])&0x3fff != 0
	case 6:
		if len(p) < 40 {
			return i, false
		}
		i.length, i.offset, i.protocol = 40+int(binary.BigEndian.Uint16(p[4:])), 40, p[6]
		if i.length > len(p) {
			return i, false
		}
		i.src, i.dst = netip.AddrFrom16([16]byte(p[8:24])), netip.AddrFrom16([16]byte(p[24:40]))
		// Walk extension headers before looking at transport headers. Fragmented
		// packets go to netstack for reassembly; peer routing preserves fragments.
		for n := 0; n < 8; n++ {
			size := 0
			switch i.protocol {
			case 0, 43, 60:
				if i.offset+2 > i.length {
					return i, false
				}
				size = (int(p[i.offset+1]) + 1) * 8
			case 44:
				if i.offset+8 > i.length {
					return i, false
				}
				i.fragment = true
				return i, true
			case 51:
				if i.offset+2 > i.length {
					return i, false
				}
				size = (int(p[i.offset+1]) + 2) * 4
			default:
				return i, true
			}
			if i.offset+size > i.length {
				return i, false
			}
			i.protocol, i.offset = p[i.offset], i.offset+size
		}
		return i, false
	default:
		return i, false
	}
	return i, true
}

func checksum(p []byte) uint16 {
	var sum uint32
	for len(p) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(p))
		p = p[2:]
	}
	if len(p) > 0 {
		sum += uint32(p[0]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}

func transportChecksum(src, dst netip.Addr, protocol byte, payload []byte) uint16 {
	pseudo := append([]byte(nil), src.AsSlice()...)
	pseudo = append(pseudo, dst.AsSlice()...)
	if src.Is4() {
		pseudo = append(pseudo, 0, protocol, byte(len(payload)>>8), byte(len(payload)))
	} else {
		pseudo = binary.BigEndian.AppendUint32(pseudo, uint32(len(payload)))
		pseudo = append(pseudo, 0, 0, 0, protocol)
	}
	return checksum(append(pseudo, payload...))
}

// clampMSS never grows an option or increases the advertised MSS. Recompute
// the checksum for both address families, including IPv4 options/IPv6 extensions.
func clampMSS(p []byte, i packetInfo, mtu int) {
	if i.fragment || i.protocol != 6 || i.offset+20 > i.length {
		return
	}
	tcp := p[i.offset:i.length]
	end := int(tcp[12]>>4) * 4
	if tcp[13]&2 == 0 || end < 20 || end > len(tcp) {
		return
	}
	max := mtu - i.offset - 20
	if max < 536 {
		max = 536
	}
	for pos := 20; pos < end; {
		kind := tcp[pos]
		if kind == 0 {
			break
		}
		if kind == 1 {
			pos++
			continue
		}
		if pos+2 > end || tcp[pos+1] < 2 || pos+int(tcp[pos+1]) > end {
			return
		}
		if kind == 2 && tcp[pos+1] == 4 && int(binary.BigEndian.Uint16(tcp[pos+2:])) > max {
			binary.BigEndian.PutUint16(tcp[pos+2:], uint16(max))
			tcp[16], tcp[17] = 0, 0
			binary.BigEndian.PutUint16(tcp[16:], transportChecksum(i.src, i.dst, 6, tcp))
		}
		pos += int(tcp[pos+1])
	}
}

func ipPacket(src, dst netip.Addr, protocol byte, payload []byte) []byte {
	size := 20
	if src.Is6() {
		size = 40
	}
	p := make([]byte, size+len(payload))
	if src.Is4() {
		p[0], p[8], p[9] = 0x45, 64, protocol
		binary.BigEndian.PutUint16(p[2:], uint16(len(p)))
		copy(p[12:16], src.AsSlice())
		copy(p[16:20], dst.AsSlice())
		binary.BigEndian.PutUint16(p[10:], checksum(p[:20]))
	} else {
		p[0], p[6], p[7] = 0x60, protocol, 64
		binary.BigEndian.PutUint16(p[4:], uint16(len(payload)))
		copy(p[8:24], src.AsSlice())
		copy(p[24:40], dst.AsSlice())
	}
	copy(p[size:], payload)
	return p
}

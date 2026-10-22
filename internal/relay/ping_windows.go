//go:build windows

package relay

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/netip"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var icmpAPI = windows.NewLazySystemDLL("iphlpapi.dll")
var icmpCreate = icmpAPI.NewProc("IcmpCreateFile")
var icmp6Create = icmpAPI.NewProc("Icmp6CreateFile")
var icmpClose = icmpAPI.NewProc("IcmpCloseHandle")
var icmpSend = icmpAPI.NewProc("IcmpSendEcho2Ex")
var icmp6Send = icmpAPI.NewProc("Icmp6SendEcho2")

// The IP Helper echo API works on Home as well as Server, without raw sockets,
// spawning ping.exe, a driver, WinNAT, or depending on localized command output.
func pingHost(ctx context.Context, source, destination netip.Addr, data []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) > 65500 {
		return nil, fmt.Errorf("ICMP payload is too large")
	}
	create := icmpCreate
	if destination.Is6() {
		create = icmp6Create
	}
	handle, _, err := create.Call()
	if handle == ^uintptr(0) {
		return nil, err
	}
	defer icmpClose.Call(handle)
	request := append(append([]byte(nil), data...), 0) // nonnil for a zero-length echo
	reply := make([]byte, 128+len(data))
	timeout := 3 * time.Second
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < timeout {
		timeout = time.Until(deadline)
	}
	if timeout <= 0 {
		return nil, context.DeadlineExceeded
	}
	ms := uintptr(max(1, timeout.Milliseconds()))
	var count uintptr
	statusOffset := 4
	if destination.Is4() {
		dst := destination.As4()
		var src uint32
		if source.IsValid() {
			a := source.As4()
			src = binary.LittleEndian.Uint32(a[:])
		}
		count, _, err = icmpSend.Call(handle, 0, 0, 0, uintptr(src), uintptr(binary.LittleEndian.Uint32(dst[:])), uintptr(unsafe.Pointer(&request[0])), uintptr(len(data)), 0, uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), ms)
	} else {
		src := windows.RawSockaddrInet6{Family: windows.AF_INET6}
		if source.IsValid() {
			src.Addr = source.As16()
		}
		dst := windows.RawSockaddrInet6{Family: windows.AF_INET6, Addr: destination.As16()}
		count, _, err = icmp6Send.Call(handle, 0, 0, 0, uintptr(unsafe.Pointer(&src)), uintptr(unsafe.Pointer(&dst)), uintptr(unsafe.Pointer(&request[0])), uintptr(len(data)), 0, uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), ms)
		statusOffset = 28 // IPV6_ADDRESS_EX, followed by ULONG Status
		runtime.KeepAlive(src)
		runtime.KeepAlive(dst)
	}
	runtime.KeepAlive(request)
	if count == 0 {
		return nil, fmt.Errorf("ICMP echo: %w", err)
	}
	if status := binary.LittleEndian.Uint32(reply[statusOffset:]); status != 0 {
		return nil, fmt.Errorf("ICMP echo status %d", status)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// IP Helper matched a successful reply to this exact echo transaction.
	return append([]byte(nil), data...), nil
}

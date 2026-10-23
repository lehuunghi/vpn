package server

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// One TCP listener serves bootstrap/LAN HTTP and automatic HTTPS on the
// existing admin port. Detection is lazy per connection, so an idle client
// cannot block Accept() for other clients. No second HTTPS port is opened.
type adminListener struct {
	net.Listener
	ctx context.Context
	tls *tls.Config
}

func (l *adminListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &adminConn{Conn: conn, ctx: l.ctx, config: l.tls}, nil
}

type protocolKey struct{}
type adminConn struct {
	net.Conn
	ctx    context.Context
	config *tls.Config
	once   sync.Once
	stream net.Conn
	tls    *tls.Conn
	err    error
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

func (c *adminConn) init() {
	c.once.Do(func() {
		reader := bufio.NewReader(c.Conn)
		first, err := reader.Peek(1)
		if err != nil {
			c.err = err
			return
		}
		stream := &bufferedConn{Conn: c.Conn, reader: reader}
		c.stream = stream
		if first[0] != 0x16 {
			return
		} // TLS handshake record.
		conn := tls.Server(stream, c.config)
		ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
		defer cancel()
		_ = c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		c.err = conn.HandshakeContext(ctx)
		_ = c.Conn.SetWriteDeadline(time.Time{})
		if c.err != nil {
			return
		}
		c.tls = conn
		c.stream = conn
	})
}
func (c *adminConn) Read(p []byte) (int, error) {
	c.init()
	if c.err != nil {
		return 0, c.err
	}
	return c.stream.Read(p)
}
func (c *adminConn) Write(p []byte) (int, error) {
	c.init()
	if c.err != nil {
		return 0, c.err
	}
	return c.stream.Write(p)
}

func (s *Server) automaticHTTPS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if conn, ok := r.Context().Value(protocolKey{}).(*adminConn); ok && conn.tls != nil {
			state := conn.tls.ConnectionState()
			r.TLS = &state // net/http only detects concrete *tls.Conn itself.
		}
		if s.ddns != nil {
			if r.TLS != nil && s.ddns.IsActiveHostname(r.TLS.ServerName) && !s.ddns.IsActiveHostname(r.Host) {
				http.Error(w, "Unknown hostname", http.StatusMisdirectedRequest)
				return
			}
			if r.TLS == nil {
				status := s.ddns.Snapshot()
				if status.URL != "" && (s.ddns.IsActiveHostname(r.Host) || !localAdminRequest(r)) {
					http.Redirect(w, r, status.URL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func localAdminRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return false
	}
	address = address.Unmap()
	return address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast()
}

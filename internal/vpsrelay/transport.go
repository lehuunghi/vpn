package vpsrelay

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	v1 "github.com/fatedier/frp/pkg/config/v1"
	yamux "github.com/fatedier/yamux"
)

// Verify TLS before starting multiplexed streams, so certificate failures are
// returned directly instead of becoming a generic stream/login EOF. Trust is
// always explicit and kept in memory with the imported profile.
type tcpConnector struct {
	ctx     context.Context
	cfg     *v1.ClientCommonConfig
	ca      string
	dial    func(context.Context, string, string) (net.Conn, error)
	onClose context.CancelFunc
	mu      sync.Mutex
	closed  bool
	session *yamux.Session
}

func (c *tcpConnector) Open() error {
	var pool *x509.CertPool
	if c.ca != "" {
		pool = x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(c.ca)) {
			return errors.New("invalid TLS certificate authority")
		}
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	dial := c.dial
	if dial == nil {
		d := &net.Dialer{KeepAlive: 30 * time.Second}
		dial = d.DialContext
	}
	raw, err := dial(ctx, "tcp", net.JoinHostPort(c.cfg.ServerAddr, strconv.Itoa(c.cfg.ServerPort)))
	if err != nil {
		return err
	}
	conn := tls.Client(raw, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: c.cfg.Transport.TLS.ServerName, RootCAs: pool})
	if err := conn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return err
	}
	cfg := yamux.DefaultConfig()
	cfg.KeepAliveInterval = 30 * time.Second
	cfg.MaxStreamWindowSize = 6 * 1024 * 1024
	cfg.LogOutput = io.Discard
	session, err := yamux.Client(conn, cfg)
	if err != nil {
		conn.Close()
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		session.Close()
		return net.ErrClosed
	}
	c.session = session
	return nil
}

func (c *tcpConnector) Connect() (net.Conn, error) {
	c.mu.Lock()
	s := c.session
	c.mu.Unlock()
	if s == nil {
		return nil, net.ErrClosed
	}
	return s.OpenStream()
}

func (c *tcpConnector) Close() error {
	// End this service before frp can reuse its mutable logger/session context.
	// Manager reconnects with a fresh service and the same saved profile.
	if c.onClose != nil {
		c.onClose()
	}
	c.mu.Lock()
	c.closed = true
	s := c.session
	c.mu.Unlock()
	if s != nil {
		return s.Close()
	}
	return nil
}

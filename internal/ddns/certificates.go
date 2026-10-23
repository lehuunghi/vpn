package ddns

import (
	"context"
	"crypto/tls"
	"errors"
	"io/fs"
	"path/filepath"
	"sync"
	"time"

	"github.com/caddyserver/certmagic"
	"go.uber.org/zap"
)

type certificates interface {
	Manage(context.Context, string) error
	GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error)
	Expires(string) time.Time
	Forget(string)
	Close()
}
type automaticCertificates struct {
	config   *certmagic.Config
	cache    *certmagic.Cache
	attempts sync.Map // hostname -> context.CancelCauseFunc for one outer attempt
}

func newCertificates(m *Manager) (certificates, error) {
	logger := zap.NewNop()
	automatic := &automaticCertificates{}
	var magic *certmagic.Config
	cache := certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) { return magic, nil },
		Logger:           logger, Capacity: 8,
	})
	magic = certmagic.New(cache, certmagic.Config{
		Storage: &certmagic.FileStorage{Path: filepath.Join(m.opts.DataDir, "ddns", "certificates")},
		Logger:  logger,
		OnEvent: func(_ context.Context, event string, data map[string]any) error {
			if event == "cert_failed" {
				name, _ := data["identifier"].(string)
				if cancel, ok := automatic.attempts.Load(name); ok {
					if err, ok := data["error"].(error); ok {
						cancel.(context.CancelCauseFunc)(err)
					}
				}
			}
			return nil
		},
	})
	ca := m.opts.ACMECA
	if ca == "" {
		ca = certmagic.LetsEncryptProductionCA
	}
	issuer := certmagic.NewACMEIssuer(magic, certmagic.ACMEIssuer{
		CA: ca, Agreed: true, DisableHTTPChallenge: true, DisableTLSALPNChallenge: true, Logger: logger,
		DNS01Solver: newBrokerSolver(m),
	})
	magic.Issuers = []certmagic.Issuer{issuer}
	automatic.config, automatic.cache = magic, cache
	return automatic, nil
}
func (a *automaticCertificates) Manage(ctx context.Context, hostname string) error {
	// ManageSync/ObtainCertSync can prompt on stdin for the first account.
	// Services have no interactive stdin. The Async obtain/renew APIs execute
	// non-interactively; this caller waits for their bounded operation before
	// caching the certificate and reporting HTTPS ready.
	for _, cert := range a.cache.AllMatchingCertificates(hostname) {
		if !cert.Expired() {
			return nil
		} // Cache maintenance handles renewal.
	}
	// Let the manager own retry/backoff. Stop CertMagic's internal retry loop
	// after the first failed attempt and preserve that cause for the UI.
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	a.attempts.Store(hostname, cancel)
	defer a.attempts.Delete(hostname)
	operationError := func(err error) error {
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		return err
	}
	cert, err := a.config.CacheManagedCertificate(ctx, hostname)
	if errors.Is(err, fs.ErrNotExist) {
		if err = a.config.ObtainCertAsync(ctx, hostname); err != nil {
			return operationError(err)
		}
		cert, err = a.config.CacheManagedCertificate(ctx, hostname)
	}
	if err != nil {
		return operationError(err)
	}
	if cert.NeedsRenewal(a.config) {
		if err := a.config.RenewCertAsync(ctx, hostname, false); err != nil {
			return operationError(err)
		}
		_, err = a.config.CacheManagedCertificate(ctx, hostname)
	}
	return err
}
func (a *automaticCertificates) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return a.config.GetCertificate(hello)
}
func (a *automaticCertificates) Expires(hostname string) time.Time {
	for _, c := range a.cache.AllMatchingCertificates(hostname) {
		if c.Leaf != nil {
			return c.Leaf.NotAfter
		}
	}
	return time.Time{}
}
func (a *automaticCertificates) Forget(hostname string) {
	for _, c := range a.cache.AllMatchingCertificates(hostname) {
		a.cache.Remove([]string{c.Hash()})
	}
}
func (a *automaticCertificates) Close() { a.cache.Stop() }

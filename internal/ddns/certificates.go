package ddns

import (
	"context"
	"crypto/tls"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/libdns"
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
	config *certmagic.Config
	cache  *certmagic.Cache
}

func newCertificates(m *Manager) (certificates, error) {
	logger := zap.NewNop()
	var magic *certmagic.Config
	cache := certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) { return magic, nil },
		Logger:           logger, Capacity: 8,
	})
	magic = certmagic.New(cache, certmagic.Config{
		Storage: &certmagic.FileStorage{Path: filepath.Join(m.opts.DataDir, "ddns", "certificates")},
		Logger:  logger,
	})
	ca := m.opts.ACMECA
	if ca == "" {
		ca = certmagic.LetsEncryptProductionCA
	}
	issuer := certmagic.NewACMEIssuer(magic, certmagic.ACMEIssuer{
		CA: ca, Agreed: true, DisableHTTPChallenge: true, DisableTLSALPNChallenge: true, Logger: logger,
		DNS01Solver: &certmagic.DNS01Solver{DNSManager: certmagic.DNSManager{
			DNSProvider: &dnsProvider{manager: m}, PropagationTimeout: 2 * time.Minute, Resolvers: []string{"1.1.1.1:53", "8.8.8.8:53"},
		}},
	})
	magic.Issuers = []certmagic.Issuer{issuer}
	return &automaticCertificates{magic, cache}, nil
}
func (a *automaticCertificates) Manage(ctx context.Context, hostname string) error {
	return a.config.ManageSync(ctx, []string{hostname})
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

// The provider accepts only DNS-01 TXT records for names owned by this install.
// The Worker repeats these checks against the token's binding.
type dnsProvider struct{ manager *Manager }

func (p *dnsProvider) change(ctx context.Context, zone string, records []libdns.Record, remove bool) ([]libdns.Record, error) {
	completed := make([]libdns.Record, 0, len(records))
	for _, record := range records {
		rr := record.RR()
		name := strings.TrimSuffix(strings.ToLower(libdns.AbsoluteName(rr.Name, zone)), ".")
		if rr.Type != "TXT" || !strings.HasPrefix(name, "_acme-challenge.") {
			return completed, errors.New("Chỉ cho phép bản ghi xác thực SSL")
		}
		hostname := strings.TrimPrefix(name, "_acme-challenge.")
		token := p.manager.key(hostname)
		if token == "" {
			return completed, errors.New("Hostname nằm ngoài quyền của máy")
		}
		if err := p.manager.broker.challenge(ctx, token, rr.Data, remove); err != nil {
			return completed, err
		}
		completed = append(completed, record)
	}
	return completed, nil
}
func (p *dnsProvider) AppendRecords(ctx context.Context, zone string, records []libdns.Record) ([]libdns.Record, error) {
	return p.change(ctx, zone, records, false)
}
func (p *dnsProvider) DeleteRecords(ctx context.Context, zone string, records []libdns.Record) ([]libdns.Record, error) {
	return p.change(ctx, zone, records, true)
}

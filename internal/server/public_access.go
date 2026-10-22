package server

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/lehuunghi/vpn/internal/publicaccess"
)

func (s *Server) handlePublicAccess(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(s.cfg.HTTP)
	a, _ := netip.ParseAddr(host)
	var urls []string
	scheme := "http"
	if s.cfg.TLSEnabled() {
		scheme = "https"
	}
	_, port, _ := net.SplitHostPort(s.cfg.HTTP)
	if host == "" || a.IsUnspecified() {
		addresses, _ := net.InterfaceAddrs()
		for _, address := range addresses {
			p, err := netip.ParsePrefix(address.String())
			if err == nil && p.Addr().Is4() && !p.Addr().IsLoopback() && !p.Addr().IsLinkLocalUnicast() {
				urls = append(urls, scheme+"://"+net.JoinHostPort(p.Addr().String(), port))
			}
		}
	} else if host != "localhost" && !a.IsLoopback() {
		urls = append(urls, scheme+"://"+net.JoinHostPort(host, port))
	}
	writeJSON(w, http.StatusOK, struct {
		publicaccess.Status
		Listen       string   `json:"listen"`
		LoopbackOnly bool     `json:"loopbackOnly"`
		LANURLs      []string `json:"lanURLs"`
	}{s.publicDNS.Status(), s.cfg.HTTP, a.IsLoopback() || strings.EqualFold(host, "localhost"), urls})
}

func (s *Server) handleSavePublicAccess(w http.ResponseWriter, r *http.Request) {
	var in struct {
		publicaccess.Input
		UseEndpoint bool `json:"useEndpoint"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	status, err := s.publicDNS.Save(r.Context(), in.Input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "public_access.saved", status.Hostname, "")
	if in.UseEndpoint {
		settings := s.eng.Settings()
		settings.EndpointHost = status.Hostname
		if err = s.eng.UpdateSettings(r.Context(), settings); err != nil {
			writeError(w, http.StatusInternalServerError, "DNS đã lưu nhưng chưa đổi được điểm kết nối VPN: "+err.Error())
			return
		}
		s.audit(r, "settings.updated", status.Hostname, "Cloudflare endpoint")
	}
	s.handlePublicAccess(w, r)
}

func (s *Server) handleCheckPublicAccess(w http.ResponseWriter, r *http.Request) {
	if err := s.publicDNS.Sync(r.Context()); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.handlePublicAccess(w, r)
}

func (s *Server) handleDeletePublicAccess(w http.ResponseWriter, r *http.Request) {
	host := s.publicDNS.Status().Hostname
	if _, err := s.publicDNS.Delete(r.Context()); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "public_access.deleted", host, "")
	s.handlePublicAccess(w, r)
}

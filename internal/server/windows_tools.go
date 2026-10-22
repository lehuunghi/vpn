package server

import (
	_ "embed"
	"net/http"
	"strings"
)

//go:embed peer_access.ps1
var peerAccessScript string

func (s *Server) handleWindowsPeerAccess(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="Enable-VPN20-PeerAccess.ps1"`)
	// BOM lets Windows PowerShell 5.1 read UTF-8 downloads consistently.
	_, _ = w.Write([]byte("\xef\xbb\xbf" + strings.ReplaceAll(peerAccessScript, "__VPN20_SUBNET__", s.cfg.Subnet4.Masked().String())))
}

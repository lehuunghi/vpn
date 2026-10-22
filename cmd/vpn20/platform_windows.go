//go:build windows

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lehuunghi/vpn/internal/config"
	"github.com/lehuunghi/vpn/internal/engine"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "VPN20"

func configPath() string {
	if path := os.Getenv("VPN20_CONFIG"); path != "" {
		return path
	}
	return filepath.Join(os.Getenv("ProgramData"), "VPN20", "config.json")
}

func platformInit() (err error) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "stop-service", "uninstall-service", "version", "--version", "-v", "help", "--help", "-h":
			return nil
		}
	}
	defer func() {
		if err != nil && len(os.Args) > 1 && os.Args[1] == "install-service" {
			writeInstallResult(err)
		}
	}()
	data, err := os.ReadFile(configPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var settings map[string]string
	if err = json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("%s: %w", configPath(), err)
	}
	for k, v := range settings {
		if !strings.HasPrefix(k, "VPN20_") || k == "VPN20_CONFIG" {
			return fmt.Errorf("invalid configuration key %q", k)
		}
		if _, exists := os.LookupEnv(k); !exists {
			if err := os.Setenv(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func platformCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "service":
		return true, svc.Run(serviceName, &windowsService{})
	case "install-service", "uninstall-service", "stop-service":
		err := manageService(args[0])
		if args[0] == "install-service" {
			writeInstallResult(err)
		}
		return true, err
	}
	return false, nil
}

// A hidden installer process has no visible stderr. Persist its actual error
// and a bounded log tail so Setup can show actionable diagnostics.
func writeInstallResult(err error) {
	dir := filepath.Dir(configPath())
	path := filepath.Join(dir, "install-error.txt")
	if err == nil {
		_ = os.Remove(path)
		return
	}
	_ = os.MkdirAll(dir, 0700)
	message := err.Error()
	logPath := filepath.Join(dir, "vpn20.log")
	if f, e := os.Open(logPath); e == nil {
		defer f.Close()
		if st, e := f.Stat(); e == nil {
			offset := st.Size() - 3500
			if offset < 0 {
				offset = 0
			}
			_, _ = f.Seek(offset, io.SeekStart)
			if tail, e := io.ReadAll(io.LimitReader(f, 3500)); e == nil {
				message += "\r\n\r\nRecent service log:\r\n" + string(tail)
			}
		}
	}
	_ = os.WriteFile(path, []byte("\xef\xbb\xbf"+message), 0600)
}

type windowsService struct{}

func (*windowsService) Execute(args []string, requests <-chan svc.ChangeRequest, statuses chan<- svc.Status) (bool, uint32) {
	statuses <- svc.Status{State: svc.StartPending, WaitHint: 30000}
	dir := os.Getenv("VPN20_DATA_DIR")
	if dir == "" {
		dir = filepath.Join(os.Getenv("ProgramData"), "VPN20")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return true, 1
	}
	path := filepath.Join(dir, "vpn20.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 10<<20 {
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return true, 1
	}
	defer f.Close()
	os.Stdout = f
	os.Stderr = f
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runContext(ctx) }()
	current := svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	statuses <- current
	for {
		select {
		case err := <-done:
			if err != nil {
				fmt.Fprintln(f, err)
				return true, 1
			}
			return false, 0
		case req := <-requests:
			switch req.Cmd {
			case svc.Interrogate:
				statuses <- current
			case svc.Stop, svc.Shutdown:
				statuses <- svc.Status{State: svc.StopPending, WaitHint: 60000}
				cancel()
				select {
				case err := <-done:
					if err != nil {
						fmt.Fprintln(f, err)
						return true, 1
					}
				case <-time.After(55 * time.Second):
					fmt.Fprintln(f, "timed out stopping VPN20")
					return true, 1
				}
				return false, 0
			}
		}
	}
}

func stopWindowsService(s *mgr.Service) error {
	status, err := s.Query()
	if err != nil {
		return err
	}
	if status.State == svc.Stopped {
		return nil
	}
	if status.State != svc.StopPending {
		if _, err = s.Control(svc.Stop); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		status, err = s.Query()
		if err != nil {
			return err
		}
		if status.State == svc.Stopped {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("VPN20 service did not stop in time")
}

func manageService(action string) error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return errors.New("run this command as Administrator")
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, openErr := m.OpenService(serviceName)
	if action != "install-service" {
		if errors.Is(openErr, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil
		}
		if openErr != nil {
			return openErr
		}
		defer s.Close()
		if err = stopWindowsService(s); err != nil {
			return err
		}
		if action == "uninstall-service" {
			return s.Delete()
		}
		return nil
	}
	dir := filepath.Dir(configPath())
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	// SQLite contains private keys; inherited ProgramData permissions are too
	// broad. Only SYSTEM and local administrators may read or change this tree.
	icacls := filepath.Join(os.Getenv("SystemRoot"), "System32", "icacls.exe")
	out, aclErr := exec.Command(icacls, dir, "/inheritance:r", "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F").CombinedOutput()
	if aclErr != nil {
		return fmt.Errorf("protect VPN20 data directory: %w: %s", aclErr, out)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cfg := mgr.Config{ServiceType: windows.SERVICE_WIN32_OWN_PROCESS, ErrorControl: mgr.ErrorNormal, ServiceStartName: "LocalSystem", DisplayName: "VPN20 WireGuard Server", Description: "VPN20 VPN server and local administration interface", StartType: mgr.StartAutomatic, DelayedAutoStart: true, Dependencies: []string{"Tcpip", "BFE"}}
	if s != nil {
		defer s.Close()
		if err = stopWindowsService(s); err != nil {
			return err
		}
		cfg.BinaryPathName = windows.EscapeArg(exe) + " service"
		if err = s.UpdateConfig(cfg); err != nil {
			return err
		}
	} else {
		if !errors.Is(openErr, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return openErr
		}
		s, err = m.CreateService(serviceName, exe, cfg, "service")
		if err != nil {
			return err
		}
		defer s.Close()
	}
	if err = s.SetRecoveryActions([]mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: 5 * time.Second}, {Type: mgr.ServiceRestart, Delay: 15 * time.Second}, {Type: mgr.ServiceRestart, Delay: 60 * time.Second}}, 86400); err != nil {
		return err
	}
	if err = s.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
		return err
	}
	if err = s.Start(); err != nil {
		return err
	}
	// The installer must not report success while the API or data plane failed.
	appConfig, err := config.FromEnv()
	if err != nil {
		_ = stopWindowsService(s)
		return err
	}
	// First-time driver/network provisioning on Windows ARM64 can exceed 40s.
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if windowsHealthReady(appConfig) {
			return nil
		}
		status, e := s.Query()
		if e != nil {
			return e
		}
		if status.State == svc.Stopped {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	_ = stopWindowsService(s)
	return fmt.Errorf("VPN20 did not become ready; see %s", filepath.Join(dir, "vpn20.log"))
}

// Probe the configured listener, including custom ports and local TLS. Trust
// the installed certificate explicitly rather than disabling TLS validation.
func windowsHealthReady(cfg *config.Config) bool {
	host, port, err := net.SplitHostPort(cfg.HTTP)
	if err != nil {
		return false
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	scheme := "http"
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	if cfg.TLSEnabled() {
		scheme = "https"
		path := cfg.TLSCert
		if path == "" {
			path = filepath.Join(cfg.DataDir, "tls.crt")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		block, _ := pem.Decode(data)
		if block == nil {
			return false
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return false
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(data) {
			return false
		}
		name := host
		if len(cert.DNSNames) > 0 {
			name = strings.Replace(cert.DNSNames[0], "*.", "vpn20.", 1)
		} else if len(cert.IPAddresses) > 0 {
			name = cert.IPAddresses[0].String()
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: name, MinVersion: tls.VersionTLS12}
	}
	client := &http.Client{Timeout: time.Second, Transport: transport}
	resp, err := client.Get(scheme + "://" + net.JoinHostPort(host, port) + "/api/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var health struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	return resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&health) == nil && health.OK && health.Version == engine.Version
}

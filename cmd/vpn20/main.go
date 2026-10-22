// Command vpn20 runs the WireGuard server and its admin UI.
//
//	vpn20                    run the server (the container's default)
//	vpn20 reset-password U   set a new password for admin user U and drop
//	                       their sessions and second factor; for lockouts
//	vpn20 version            print the version
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/lehuunghi/vpn/internal/auth"
	"github.com/lehuunghi/vpn/internal/config"
	"github.com/lehuunghi/vpn/internal/engine"
	"github.com/lehuunghi/vpn/internal/server"
	"github.com/lehuunghi/vpn/internal/store"
	"github.com/lehuunghi/vpn/internal/update"
	"github.com/lehuunghi/vpn/internal/wg"
)

func main() {
	// Update helpers must run before service/config initialization: they can
	// restore a version whose normal service startup is currently failing.
	if len(os.Args) > 1 && (os.Args[1] == "apply-update" || os.Args[1] == "docker-apply-update") {
		var err error
		if len(os.Args) != 3 {
			err = errors.New("update helper requires a private plan file")
		} else if os.Args[1] == "apply-update" {
			err = update.Apply(os.Args[2])
		} else {
			err = update.DockerApply(os.Args[2])
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "vpn20 update:", err)
			os.Exit(1)
		}
		return
	}
	if err := platformInit(); err != nil {
		fmt.Fprintln(os.Stderr, "vpn20:", err)
		os.Exit(1)
	}
	if handled, err := platformCommand(os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, "vpn20:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			cfg, err := config.FromEnv()
			if err == nil {
				cert := cfg.TLSCert
				if cert == "" && cfg.TLSSelfSigned {
					cert = filepath.Join(cfg.DataDir, "tls.crt")
				}
				err = update.Health(context.Background(), update.LocalURL(cfg.HTTP, cfg.TLSEnabled()), cert, engine.Version)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "version", "--version", "-v":
			fmt.Println("VPN20", engine.Version)
			return
		case "reset-password":
			if err := resetPassword(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		case "serve", "run":
		case "help", "--help", "-h":
			fmt.Print(usage)
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q\n%s", os.Args[1], usage)
			os.Exit(2)
		}
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "vpn20:", err)
		os.Exit(1)
	}
}

const usage = `usage: vpn20 [serve | reset-password <user> | version]

Configuration is read from VPN20_* environment variables; see the README.
Windows: vpn20.exe enable-lan enables LAN administration and restarts the service.
`

func newLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogJSON {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

func openBackend(cfg *config.Config, log *slog.Logger) (wg.Backend, error) {
	if runtime.GOOS == "windows" && cfg.Backend == "auto" {
		if cfg.WindowsNetworkMode == "auto" || cfg.WindowsNetworkMode == "relay" {
			return wg.NewUserspace(cfg.Iface, log)
		}
		return wg.NewKernel(cfg.Iface, log)
	}
	switch cfg.Backend {
	case "mock":
		log.Warn("using the mock data plane: no real tunnel will be created")
		return wg.NewMock(cfg.Iface, true), nil
	case "kernel":
		return wg.NewKernel(cfg.Iface, log)
	case "userspace":
		return wg.NewUserspace(cfg.Iface, log)
	}
	if wg.KernelAvailable() {
		return wg.NewKernel(cfg.Iface, log)
	}
	log.Warn("the kernel has no WireGuard support; falling back to wireguard-go, which is slower. Load the wireguard module on the host for full speed.")
	return wg.NewUserspace(cfg.Iface, log)
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return runContext(ctx)
}

func runContext(ctx context.Context) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	log := newLogger(cfg)
	log.Info("starting VPN20", "version", engine.Version)

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	be, err := openBackend(cfg, log)
	if err != nil {
		return err
	}
	eng := engine.New(cfg, st, be, log)
	if err := eng.Start(ctx); err != nil {
		_ = eng.Stop(context.Background())
		return err
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := eng.Stop(shutdown); err != nil {
			log.Warn("shutdown", "error", err)
		}
	}()
	srv := server.New(cfg, eng, log)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe(ctx) }()

	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err := <-serveErr:
		if err != nil {
			log.Error("admin server failed", "error", err)
			return err
		}
	}
	return nil
}

// resetPassword is the way back in when every administrator is locked out.
// It runs inside the container against the same database.
func resetPassword(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: vpn20 reset-password <username>")
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()
	u, err := st.UserByName(ctx, args[0])
	if err != nil {
		return fmt.Errorf("no user named %q", args[0])
	}
	var pw string
	if v := os.Getenv("VPN20_NEW_PASSWORD"); v != "" {
		pw = v
	} else {
		fmt.Fprint(os.Stderr, "New password: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		pw = strings.TrimSpace(string(b))
	}
	if err := auth.ValidatePassword(pw); err != nil {
		return err
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	if err := st.SetPassword(ctx, u.ID, hash); err != nil {
		return err
	}
	if err := st.SetTOTP(ctx, u.ID, "", false); err != nil {
		return err
	}
	_ = st.ReplaceRecoveryCodes(ctx, u.ID, nil)
	_ = st.DeleteUserSessions(ctx, u.ID)
	_ = st.Audit(ctx, store.AuditEntry{Actor: "cli", Action: "password.reset", Target: u.Username, Detail: "two-factor cleared, sessions dropped"})
	fmt.Fprintf(os.Stderr, "password for %s reset; two-factor cleared and sessions dropped\n", u.Username)
	return nil
}

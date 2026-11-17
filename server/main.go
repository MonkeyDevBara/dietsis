// Command dietsis-server is the Dietsis API: a single static binary serving /api/v1 behind Nginx.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MonkeyDevBara/dietsis/server/internal/auth"
	"github.com/MonkeyDevBara/dietsis/server/internal/config"
	"github.com/MonkeyDevBara/dietsis/server/internal/httpx"
	"github.com/MonkeyDevBara/dietsis/server/internal/store"
)

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "dietsis-server:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	slog.SetDefault(newLogger(cfg))

	switch cmd {
	case "serve":
		return serve(cfg)
	case "migrate":
		return migrate(cfg)
	case "version":
		fmt.Println(version)
		return nil
	default:
		return fmt.Errorf("unknown command %q (available: serve, migrate, version)", cmd)
	}
}

func newLogger(cfg config.Config) *slog.Logger {
	if cfg.Production() {
		return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func openAndMigrate(ctx context.Context, cfg config.Config) (*store.DB, error) {
	db, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return db, nil
}

func migrate(cfg config.Config) error {
	db, err := openAndMigrate(context.Background(), cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	slog.Info("database is up to date", "path", cfg.DBPath)
	return nil
}

func serve(cfg config.Config) error {
	db, err := openAndMigrate(context.Background(), cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// A typed-nil verifier would defeat the "not configured" check, so only assign when configured.
	var verifier auth.IDTokenVerifier
	if cfg.GoogleClientID != "" {
		verifier = &auth.Verifier{ClientID: cfg.GoogleClientID, Keys: auth.NewJWKSCache(auth.GoogleJWKSURL, nil), Now: time.Now}
	} else {
		slog.Warn("DIETSIS_GOOGLE_CLIENT_ID is empty: Google login is disabled (POST /auth/google answers 503)")
	}
	sessions := &auth.Sessions{Store: db, TTL: cfg.SessionTTL, Secure: cfg.CookieSecure, Now: time.Now}
	go purgeSessionsLoop(ctx, db)

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: httpx.NewHandler(httpx.Deps{
			Version:        version,
			Ping:           db.Ping,
			TrustedProxies: cfg.TrustedProxies,
			Auth: &httpx.AuthDeps{
				Sessions:     sessions,
				Verifier:     verifier,
				Users:        db,
				PublicOrigin: cfg.PublicOrigin,
				Limits:       httpx.DefaultLimits(),
			},
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr, "env", cfg.Env, "version", version)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// purgeSessionsLoop deletes expired sessions at start and then hourly until ctx ends.
func purgeSessionsLoop(ctx context.Context, db *store.DB) {
	purge := func() {
		if n, err := db.PurgeExpiredSessions(ctx, time.Now()); err != nil {
			if ctx.Err() == nil {
				slog.Error("purge expired sessions", "err", err)
			}
		} else if n > 0 {
			slog.Info("purged expired sessions", "count", n)
		}
	}
	purge()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			purge()
		}
	}
}

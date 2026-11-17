// Package config loads and validates server configuration from environment variables.
// Secrets (Google client ID is public, but kept here for symmetry) never live in source control.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Env string

const (
	EnvDevelopment Env = "development"
	EnvProduction  Env = "production"
)

type Config struct {
	Env            Env
	Addr           string
	DBPath         string
	GoogleClientID string
	// PublicOrigin is the exact browser origin allowed to call the API (scheme://host[:port]).
	PublicOrigin   string
	CookieSecure   bool
	SessionTTL     time.Duration
	TrustedProxies []netip.Prefix
	BackupDir      string
	BackupKeep     int
}

func (c Config) Production() bool { return c.Env == EnvProduction }

// Load reads configuration through getenv (os.Getenv in production, a map in tests).
// All problems are reported together so a misconfigured deploy fails with one complete message.
func Load(getenv func(string) string) (Config, error) {
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}

	var errs []error
	cfg := Config{
		Env:            Env(get("DIETSIS_ENV", string(EnvDevelopment))),
		Addr:           get("DIETSIS_ADDR", "127.0.0.1:8787"),
		DBPath:         get("DIETSIS_DB_PATH", "./data/dietsis.db"),
		GoogleClientID: get("DIETSIS_GOOGLE_CLIENT_ID", ""),
		PublicOrigin:   get("DIETSIS_PUBLIC_ORIGIN", "http://localhost:5178"),
		BackupDir:      get("DIETSIS_BACKUP_DIR", "./data/backups"),
	}

	switch cfg.Env {
	case EnvDevelopment, EnvProduction:
	default:
		errs = append(errs, fmt.Errorf("DIETSIS_ENV must be %q or %q, got %q", EnvDevelopment, EnvProduction, cfg.Env))
	}

	ttl, err := time.ParseDuration(get("DIETSIS_SESSION_TTL", "720h"))
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("DIETSIS_SESSION_TTL: %w", err))
	case ttl < time.Hour || ttl > 90*24*time.Hour:
		errs = append(errs, fmt.Errorf("DIETSIS_SESSION_TTL must be between 1h and 2160h, got %s", ttl))
	}
	cfg.SessionTTL = ttl

	keep, err := strconv.Atoi(get("DIETSIS_BACKUP_KEEP", "14"))
	if err != nil || keep < 1 || keep > 365 {
		errs = append(errs, fmt.Errorf("DIETSIS_BACKUP_KEEP must be an integer between 1 and 365"))
	}
	cfg.BackupKeep = keep

	for _, raw := range strings.Split(get("DIETSIS_TRUSTED_PROXIES", "127.0.0.1/32,::1/128"), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		p, err := netip.ParsePrefix(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("DIETSIS_TRUSTED_PROXIES: %w", err))
			continue
		}
		cfg.TrustedProxies = append(cfg.TrustedProxies, p.Masked())
	}

	origin, err := url.Parse(cfg.PublicOrigin)
	if err != nil || origin.Scheme == "" || origin.Host == "" || origin.Path != "" || origin.RawQuery != "" {
		errs = append(errs, fmt.Errorf("DIETSIS_PUBLIC_ORIGIN must be an origin like https://example.com, got %q", cfg.PublicOrigin))
	}

	if cfg.Env == EnvProduction {
		cfg.CookieSecure = true
		if origin != nil && origin.Scheme != "https" {
			errs = append(errs, errors.New("DIETSIS_PUBLIC_ORIGIN must use https in production"))
		}
		if cfg.GoogleClientID == "" {
			errs = append(errs, errors.New("DIETSIS_GOOGLE_CLIENT_ID is required in production"))
		}
		if !filepath.IsAbs(cfg.DBPath) {
			errs = append(errs, errors.New("DIETSIS_DB_PATH must be absolute in production"))
		}
		if !filepath.IsAbs(cfg.BackupDir) {
			errs = append(errs, errors.New("DIETSIS_BACKUP_DIR must be absolute in production"))
		}
	} else {
		secure, err := strconv.ParseBool(get("DIETSIS_COOKIE_SECURE", "false"))
		if err != nil {
			errs = append(errs, fmt.Errorf("DIETSIS_COOKIE_SECURE: %w", err))
		}
		cfg.CookieSecure = secure
	}

	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

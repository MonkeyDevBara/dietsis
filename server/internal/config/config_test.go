package config

import (
	"strings"
	"testing"
	"time"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDevelopmentDefaults(t *testing.T) {
	cfg, err := Load(envMap(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Production() || cfg.CookieSecure {
		t.Fatalf("development defaults must not be production or force secure cookies: %+v", cfg)
	}
	if cfg.Addr != "127.0.0.1:8787" || cfg.SessionTTL != 720*time.Hour || cfg.BackupKeep != 14 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if len(cfg.TrustedProxies) != 2 {
		t.Fatalf("want loopback proxies trusted by default, got %v", cfg.TrustedProxies)
	}
}

func TestLoadProductionForcesSecureCookie(t *testing.T) {
	cfg, err := Load(envMap(map[string]string{
		"DIETSIS_ENV":              "production",
		"DIETSIS_DB_PATH":          "/var/lib/dietsis/dietsis.db",
		"DIETSIS_BACKUP_DIR":       "/var/backups/dietsis",
		"DIETSIS_GOOGLE_CLIENT_ID": "123.apps.googleusercontent.com",
		"DIETSIS_PUBLIC_ORIGIN":    "https://dietsis.transbara.my.id",
		"DIETSIS_COOKIE_SECURE":    "false",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.CookieSecure {
		t.Fatal("production must always use Secure cookies, even if DIETSIS_COOKIE_SECURE=false")
	}
}

func TestLoadProductionReportsAllProblems(t *testing.T) {
	_, err := Load(envMap(map[string]string{
		"DIETSIS_ENV":           "production",
		"DIETSIS_PUBLIC_ORIGIN": "http://dietsis.transbara.my.id",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"https", "DIETSIS_GOOGLE_CLIENT_ID", "DIETSIS_DB_PATH", "DIETSIS_BACKUP_DIR"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := map[string]map[string]string{
		"env":     {"DIETSIS_ENV": "staging"},
		"ttl":     {"DIETSIS_SESSION_TTL": "5m"},
		"keep":    {"DIETSIS_BACKUP_KEEP": "0"},
		"proxies": {"DIETSIS_TRUSTED_PROXIES": "not-a-cidr"},
		"origin":  {"DIETSIS_PUBLIC_ORIGIN": "https://example.com/app"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(envMap(env)); err == nil {
				t.Fatalf("expected error for %v", env)
			}
		})
	}
}

// Package auth verifies Google ID tokens and manages login sessions.
package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// GoogleJWKSURL is where Google publishes the public keys that sign ID tokens.
const GoogleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

const (
	minRSABits       = 2048
	maxJWKSBytes     = 1 << 20
	minRefreshGap    = time.Minute // at most one fetch per minute, even for unknown kids
	defaultKeyMaxAge = time.Hour
)

// KeySource resolves a JWT `kid` to a public key.
type KeySource interface {
	Key(ctx context.Context, kid string) (*rsa.PublicKey, error)
}

// JWKSCache fetches and caches a JWKS document. It honors Cache-Control max-age, refetches when a
// token names an unknown kid (key rotation) but never more than once per minute, and keeps serving
// the previous keys if a refresh fails, so a Google outage does not lock out existing logins.
type JWKSCache struct {
	url    string
	client *http.Client
	now    func() time.Time

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	expires   time.Time
	lastFetch time.Time
}

func NewJWKSCache(url string, client *http.Client) *JWKSCache {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &JWKSCache{url: url, client: client, now: time.Now}
}

var errUnknownKey = errors.New("unknown signing key")

func (c *JWKSCache) Key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if k, ok := c.keys[kid]; ok && now.Before(c.expires) {
		return k, nil
	}
	// Stale cache or unknown kid: refresh, but rate limited.
	if c.lastFetch.IsZero() || now.Sub(c.lastFetch) >= minRefreshGap {
		if err := c.refreshLocked(ctx, now); err != nil {
			// Keep serving old keys while they are still known, even past max-age.
			if k, ok := c.keys[kid]; ok {
				return k, nil
			}
			return nil, err
		}
	}
	if k, ok := c.keys[kid]; ok {
		return k, nil
	}
	return nil, errUnknownKey
}

func (c *JWKSCache) refreshLocked(ctx context.Context, now time.Time) error {
	c.lastFetch = now
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch jwks: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBytes+1))
	if err != nil {
		return fmt.Errorf("read jwks: %w", err)
	}
	if len(body) > maxJWKSBytes {
		return errors.New("jwks response too large")
	}
	keys, err := parseJWKS(body)
	if err != nil {
		return err
	}
	c.keys = keys
	c.expires = now.Add(maxAge(resp.Header.Get("Cache-Control")))
	return nil
}

var maxAgeRe = regexp.MustCompile(`(?i)max-age=(\d+)`)

func maxAge(cacheControl string) time.Duration {
	if m := maxAgeRe.FindStringSubmatch(cacheControl); m != nil {
		if secs, err := strconv.Atoi(m[1]); err == nil && secs > 0 {
			return min(time.Duration(secs)*time.Second, 24*time.Hour)
		}
	}
	return defaultKeyMaxAge
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// parseJWKS keeps only usable RSA signature keys of at least 2048 bits; anything else is skipped.
func parseJWKS(body []byte) (map[string]*rsa.PublicKey, error) {
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse jwks: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Kid == "" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		n, err1 := base64.RawURLEncoding.DecodeString(k.N)
		e, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil || len(e) == 0 || len(e) > 4 {
			continue
		}
		mod := new(big.Int).SetBytes(n)
		if mod.BitLen() < minRSABits {
			continue
		}
		exp := int(new(big.Int).SetBytes(e).Int64())
		if exp < 3 {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: mod, E: exp}
	}
	if len(keys) == 0 {
		return nil, errors.New("jwks contains no usable RSA keys")
	}
	return keys, nil
}

package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidToken wraps every verification failure. Callers answer 401 with a generic message and
// log the wrapped reason server-side only; the reason never reaches the client.
var ErrInvalidToken = errors.New("invalid id token")

// Claims are the verified identity fields from a Google ID token.
type Claims struct {
	Sub           string
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
}

// IDTokenVerifier is what the HTTP layer needs; tests substitute a fake.
type IDTokenVerifier interface {
	Verify(ctx context.Context, rawToken string) (Claims, error)
}

type Verifier struct {
	ClientID string
	Keys     KeySource
	Now      func() time.Time
}

// clockSkew tolerates small clock differences when checking iat/nbf.
const clockSkew = 60 * time.Second

const maxTokenLen = 8 << 10

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidToken, fmt.Sprintf(format, a...))
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type jwtPayload struct {
	Iss           string          `json:"iss"`
	Aud           string          `json:"aud"`
	Azp           string          `json:"azp"`
	Sub           string          `json:"sub"`
	Email         string          `json:"email"`
	EmailVerified json.RawMessage `json:"email_verified"`
	Name          string          `json:"name"`
	Picture       string          `json:"picture"`
	Exp           json.Number     `json:"exp"`
	Iat           json.Number     `json:"iat"`
	Nbf           json.Number     `json:"nbf"`
}

// Verify checks signature (RS256 only), issuer, audience, expiry, issued-at and a verified email.
// See docs/SECURITY.md → Autentikasi.
func (v *Verifier) Verify(ctx context.Context, raw string) (Claims, error) {
	if raw == "" || len(raw) > maxTokenLen {
		return Claims{}, invalid("token empty or too large")
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return Claims{}, invalid("expected 3 segments, got %d", len(parts))
	}

	var h jwtHeader
	if err := decodeSegment(parts[0], &h); err != nil {
		return Claims{}, invalid("header: %v", err)
	}
	// Pin the algorithm: never trust the header to choose it (blocks "none" and HS256 key confusion).
	if h.Alg != "RS256" {
		return Claims{}, invalid("unsupported alg %q", h.Alg)
	}
	if h.Kid == "" {
		return Claims{}, invalid("missing kid")
	}

	key, err := v.Keys.Key(ctx, h.Kid)
	if errors.Is(err, errUnknownKey) {
		return Claims{}, invalid("unknown signing key")
	}
	if err != nil {
		// Not the token's fault (Google unreachable, bad JWKS): callers answer 503, not 401.
		return Claims{}, fmt.Errorf("load signing keys: %w", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, invalid("signature encoding: %v", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return Claims{}, invalid("bad signature")
	}

	// Claims are read only after the signature is verified.
	var p jwtPayload
	if err := decodeSegment(parts[1], &p); err != nil {
		return Claims{}, invalid("payload: %v", err)
	}
	now := v.Now()

	if p.Iss != "accounts.google.com" && p.Iss != "https://accounts.google.com" {
		return Claims{}, invalid("issuer %q", p.Iss)
	}
	if v.ClientID == "" || p.Aud != v.ClientID {
		return Claims{}, invalid("audience mismatch")
	}
	if p.Azp != "" && p.Azp != v.ClientID {
		return Claims{}, invalid("authorized party mismatch")
	}
	exp, err := unixTime(p.Exp)
	if err != nil {
		return Claims{}, invalid("exp: %v", err)
	}
	if !now.Before(exp) {
		return Claims{}, invalid("token expired")
	}
	iat, err := unixTime(p.Iat)
	if err != nil {
		return Claims{}, invalid("iat: %v", err)
	}
	if iat.After(now.Add(clockSkew)) {
		return Claims{}, invalid("issued in the future")
	}
	if p.Nbf != "" {
		nbf, err := unixTime(p.Nbf)
		if err != nil {
			return Claims{}, invalid("nbf: %v", err)
		}
		if nbf.After(now.Add(clockSkew)) {
			return Claims{}, invalid("not yet valid")
		}
	}
	if p.Sub == "" || len(p.Sub) > 255 {
		return Claims{}, invalid("missing or oversized sub")
	}
	if p.Email == "" || len(p.Email) > 320 {
		return Claims{}, invalid("missing or oversized email")
	}
	if !truthy(p.EmailVerified) {
		return Claims{}, invalid("email not verified")
	}

	return Claims{Sub: p.Sub, Email: p.Email, EmailVerified: true, Name: clip(p.Name, 200), Picture: clip(p.Picture, 500)}, nil
}

func decodeSegment(seg string, dst any) error {
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	return dec.Decode(dst)
}

func unixTime(n json.Number) (time.Time, error) {
	if n == "" {
		return time.Time{}, errors.New("missing")
	}
	secs, err := n.Int64()
	if err != nil {
		f, ferr := n.Float64()
		if ferr != nil {
			return time.Time{}, errors.New("not a number")
		}
		secs = int64(f)
	}
	return time.Unix(secs, 0), nil
}

// truthy accepts a JSON true or the string "true" (Google has used both for email_verified).
func truthy(raw json.RawMessage) bool {
	s := strings.Trim(string(raw), `"`)
	return s == "true"
}

func clip(s string, maxRunes int) string {
	if r := []rune(s); len(r) > maxRunes {
		return string(r[:maxRunes])
	}
	return s
}

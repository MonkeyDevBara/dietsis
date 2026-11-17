package auth

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testClientID = "1234-abc.apps.googleusercontent.com"
	testKid      = "key-1"
)

var (
	keyOnce sync.Once
	keyA    *rsa.PrivateKey
	keyB    *rsa.PrivateKey
)

func testKeys(t testing.TB) (*rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	keyOnce.Do(func() {
		var err error
		if keyA, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
		if keyB, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
	})
	return keyA, keyB
}

var fixedNow = time.Unix(1_800_000_000, 0)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func jsonSeg(t testing.TB, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b64(b)
}

func validPayload() map[string]any {
	return map[string]any{
		"iss": "https://accounts.google.com", "aud": testClientID, "azp": testClientID,
		"sub": "110169484474386276334", "email": "rina@example.com", "email_verified": true,
		"name": "Rina S", "picture": "https://lh3.googleusercontent.com/a/x",
		"iat": fixedNow.Add(-time.Minute).Unix(), "exp": fixedNow.Add(59 * time.Minute).Unix(),
	}
}

// signRS256 builds a compact JWT signed with key.
func signRS256(t testing.TB, key *rsa.PrivateKey, header map[string]any, payload map[string]any) string {
	t.Helper()
	signing := jsonSeg(t, header) + "." + jsonSeg(t, payload)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + b64(sig)
}

type staticKeys map[string]*rsa.PublicKey

func (s staticKeys) Key(_ context.Context, kid string) (*rsa.PublicKey, error) {
	if k, ok := s[kid]; ok {
		return k, nil
	}
	return nil, errUnknownKey
}

func newVerifier(t testing.TB) (*Verifier, *rsa.PrivateKey, *rsa.PrivateKey) {
	a, b := testKeys(t)
	return &Verifier{ClientID: testClientID, Keys: staticKeys{testKid: &a.PublicKey}, Now: func() time.Time { return fixedNow }}, a, b
}

func goodHeader() map[string]any { return map[string]any{"alg": "RS256", "kid": testKid, "typ": "JWT"} }

func TestVerifyAcceptsValidToken(t *testing.T) {
	v, key, _ := newVerifier(t)
	claims, err := v.Verify(context.Background(), signRS256(t, key, goodHeader(), validPayload()))
	if err != nil {
		t.Fatal(err)
	}
	want := Claims{Sub: "110169484474386276334", Email: "rina@example.com", EmailVerified: true, Name: "Rina S", Picture: "https://lh3.googleusercontent.com/a/x"}
	if claims != want {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestVerifyAcceptsLegacyIssuerAndStringEmailVerified(t *testing.T) {
	v, key, _ := newVerifier(t)
	p := validPayload()
	p["iss"] = "accounts.google.com"
	p["email_verified"] = "true"
	delete(p, "azp")
	if _, err := v.Verify(context.Background(), signRS256(t, key, goodHeader(), p)); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejects(t *testing.T) {
	v, key, otherKey := newVerifier(t)
	mut := func(f func(p map[string]any)) string {
		p := validPayload()
		f(p)
		return signRS256(t, key, goodHeader(), p)
	}

	// HS256 "key confusion": MAC the token with the *public* key bytes as the HMAC secret.
	pubDER := keyA.N.Bytes()
	hsSigning := jsonSeg(t, map[string]any{"alg": "HS256", "kid": testKid}) + "." + jsonSeg(t, validPayload())
	mac := hmac.New(sha256.New, pubDER)
	mac.Write([]byte(hsSigning))
	hs256 := hsSigning + "." + b64(mac.Sum(nil))

	noneToken := jsonSeg(t, map[string]any{"alg": "none", "kid": testKid}) + "." + jsonSeg(t, validPayload()) + "."

	validTok := signRS256(t, key, goodHeader(), validPayload())
	segs := strings.Split(validTok, ".")
	tamperedPayload := validPayload()
	tamperedPayload["email"] = "attacker@example.com"
	tamperedBody := segs[0] + "." + jsonSeg(t, tamperedPayload) + "." + segs[2]

	cases := map[string]string{
		"empty":                 "",
		"two segments":          segs[0] + "." + segs[1],
		"four segments":         validTok + ".x",
		"garbage":               "not.a.jwt",
		"alg none":              noneToken,
		"alg HS256 confusion":   hs256,
		"alg RS384":             signRS256(t, key, map[string]any{"alg": "RS384", "kid": testKid}, validPayload()),
		"missing kid":           signRS256(t, key, map[string]any{"alg": "RS256"}, validPayload()),
		"unknown kid":           signRS256(t, key, map[string]any{"alg": "RS256", "kid": "rotated-away"}, validPayload()),
		"signed by another key": signRS256(t, otherKey, goodHeader(), validPayload()),
		"payload tampered":      tamperedBody,
		"wrong issuer":          mut(func(p map[string]any) { p["iss"] = "https://evil.example.com" }),
		"issuer lookalike":      mut(func(p map[string]any) { p["iss"] = "accounts.google.com.evil.com" }),
		"wrong audience":        mut(func(p map[string]any) { p["aud"] = "someone-elses-client" }),
		"audience missing":      mut(func(p map[string]any) { delete(p, "aud") }),
		"wrong azp":             mut(func(p map[string]any) { p["azp"] = "other-client" }),
		"expired":               mut(func(p map[string]any) { p["exp"] = fixedNow.Add(-time.Second).Unix() }),
		"expires exactly now":   mut(func(p map[string]any) { p["exp"] = fixedNow.Unix() }),
		"exp missing":           mut(func(p map[string]any) { delete(p, "exp") }),
		"exp not a number":      mut(func(p map[string]any) { p["exp"] = "tomorrow" }),
		"issued in future":      mut(func(p map[string]any) { p["iat"] = fixedNow.Add(5 * time.Minute).Unix() }),
		"not yet valid":         mut(func(p map[string]any) { p["nbf"] = fixedNow.Add(5 * time.Minute).Unix() }),
		"email unverified":      mut(func(p map[string]any) { p["email_verified"] = false }),
		"email_verified absent": mut(func(p map[string]any) { delete(p, "email_verified") }),
		"email missing":         mut(func(p map[string]any) { delete(p, "email") }),
		"sub missing":           mut(func(p map[string]any) { p["sub"] = "" }),
		"oversized token":       strings.Repeat("a", maxTokenLen+1),
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := v.Verify(context.Background(), tok)
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("want ErrInvalidToken, got %v", err)
			}
		})
	}
}

func TestVerifyToleratesSmallClockSkewOnIat(t *testing.T) {
	v, key, _ := newVerifier(t)
	p := validPayload()
	p["iat"] = fixedNow.Add(30 * time.Second).Unix()
	if _, err := v.Verify(context.Background(), signRS256(t, key, goodHeader(), p)); err != nil {
		t.Fatalf("30s skew should be tolerated: %v", err)
	}
}

func TestVerifyRefusesWhenClientIDNotConfigured(t *testing.T) {
	v, key, _ := newVerifier(t)
	v.ClientID = ""
	p := validPayload()
	p["aud"] = ""
	delete(p, "azp")
	if _, err := v.Verify(context.Background(), signRS256(t, key, goodHeader(), p)); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("empty client id must never match an empty aud, got %v", err)
	}
}

// ---- JWKS cache ----

func jwksJSON(kid string, pub *rsa.PublicKey) string {
	return fmt.Sprintf(`{"keys":[{"kty":"RSA","alg":"RS256","use":"sig","kid":%q,"n":%q,"e":%q}]}`,
		kid, b64(pub.N.Bytes()), b64(big.NewInt(int64(pub.E)).Bytes()))
}

type jwksServer struct {
	*httptest.Server
	hits   atomic.Int32
	body   atomic.Value // string
	status atomic.Int32
	cc     atomic.Value // string
}

func newJWKSServer(t *testing.T, body string) *jwksServer {
	s := &jwksServer{}
	s.body.Store(body)
	s.cc.Store("public, max-age=600")
	s.status.Store(200)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.hits.Add(1)
		w.Header().Set("Cache-Control", s.cc.Load().(string))
		w.WriteHeader(int(s.status.Load()))
		_, _ = w.Write([]byte(s.body.Load().(string)))
	}))
	t.Cleanup(s.Close)
	return s
}

func TestJWKSCacheCachesAndHonorsMaxAge(t *testing.T) {
	a, _ := testKeys(t)
	srv := newJWKSServer(t, jwksJSON("k1", &a.PublicKey))
	now := fixedNow
	c := NewJWKSCache(srv.URL, nil)
	c.now = func() time.Time { return now }

	for range 5 {
		if _, err := c.Key(context.Background(), "k1"); err != nil {
			t.Fatal(err)
		}
	}
	if srv.hits.Load() != 1 {
		t.Fatalf("fetches = %d, want 1 (cached)", srv.hits.Load())
	}
	now = now.Add(11 * time.Minute) // past max-age=600
	if _, err := c.Key(context.Background(), "k1"); err != nil {
		t.Fatal(err)
	}
	if srv.hits.Load() != 2 {
		t.Fatalf("fetches = %d, want 2 after expiry", srv.hits.Load())
	}
}

func TestJWKSCacheRefetchesOnUnknownKidButRateLimited(t *testing.T) {
	a, b := testKeys(t)
	srv := newJWKSServer(t, jwksJSON("old", &a.PublicKey))
	now := fixedNow
	c := NewJWKSCache(srv.URL, nil)
	c.now = func() time.Time { return now }

	if _, err := c.Key(context.Background(), "old"); err != nil {
		t.Fatal(err)
	}
	// Google rotates: a token arrives with a new kid.
	srv.body.Store(jwksJSON("new", &b.PublicKey))
	now = now.Add(2 * time.Minute)
	if _, err := c.Key(context.Background(), "new"); err != nil {
		t.Fatalf("rotated key should be found after refetch: %v", err)
	}
	// An attacker spraying random kids must not turn into a fetch per request.
	before := srv.hits.Load()
	for i := range 20 {
		if _, err := c.Key(context.Background(), fmt.Sprintf("bogus-%d", i)); !errors.Is(err, errUnknownKey) {
			t.Fatalf("want errUnknownKey, got %v", err)
		}
	}
	if srv.hits.Load() != before {
		t.Fatalf("unknown kids triggered %d extra fetches within a minute", srv.hits.Load()-before)
	}
}

func TestJWKSCacheKeepsOldKeysWhenRefreshFails(t *testing.T) {
	a, _ := testKeys(t)
	srv := newJWKSServer(t, jwksJSON("k1", &a.PublicKey))
	now := fixedNow
	c := NewJWKSCache(srv.URL, nil)
	c.now = func() time.Time { return now }
	if _, err := c.Key(context.Background(), "k1"); err != nil {
		t.Fatal(err)
	}
	srv.status.Store(500)
	srv.body.Store("boom")
	now = now.Add(2 * time.Hour)
	if _, err := c.Key(context.Background(), "k1"); err != nil {
		t.Fatalf("a Google outage must not break verification of a known key: %v", err)
	}
	if _, err := c.Key(context.Background(), "never-seen"); err == nil {
		t.Fatal("unknown key during outage must fail")
	}
}

func TestJWKSFirstFetchFailureIsAnError(t *testing.T) {
	srv := newJWKSServer(t, "nope")
	srv.status.Store(503)
	c := NewJWKSCache(srv.URL, nil)
	if _, err := c.Key(context.Background(), "k"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseJWKSSkipsWeakAndNonRSAKeys(t *testing.T) {
	a, _ := testKeys(t)
	weak, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	doc := fmt.Sprintf(`{"keys":[
		{"kty":"RSA","kid":"good","n":%q,"e":"AQAB"},
		{"kty":"RSA","kid":"weak","n":%q,"e":"AQAB"},
		{"kty":"EC","kid":"ec","crv":"P-256","x":"AA","y":"AA"},
		{"kty":"RSA","kid":"enc","use":"enc","n":%q,"e":"AQAB"},
		{"kty":"RSA","n":%q,"e":"AQAB"},
		{"kty":"RSA","kid":"e1","n":%q,"e":"AQ"}
	]}`, b64(a.N.Bytes()), b64(weak.N.Bytes()), b64(a.N.Bytes()), b64(a.N.Bytes()), b64(a.N.Bytes()))
	keys, err := parseJWKS([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys["good"] == nil {
		t.Fatalf("only the 2048-bit RSA signature key should survive, got %v", keys)
	}
	if _, err := parseJWKS([]byte(`{"keys":[]}`)); err == nil {
		t.Fatal("empty key set must be an error")
	}
	if _, err := parseJWKS([]byte(`<html>`)); err == nil {
		t.Fatal("non-JSON must be an error")
	}
}

func TestMaxAgeParsing(t *testing.T) {
	cases := map[string]time.Duration{
		"public, max-age=19234, must-revalidate": 19234 * time.Second,
		"max-age=0":                              defaultKeyMaxAge,
		"no-cache":                               defaultKeyMaxAge,
		"max-age=999999999":                      24 * time.Hour,
		"":                                       defaultKeyMaxAge,
	}
	for in, want := range cases {
		if got := maxAge(in); got != want {
			t.Errorf("maxAge(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestVerifierWithRealJWKSEndToEnd(t *testing.T) {
	a, _ := testKeys(t)
	srv := newJWKSServer(t, jwksJSON(testKid, &a.PublicKey))
	cache := NewJWKSCache(srv.URL, nil)
	v := &Verifier{ClientID: testClientID, Keys: cache, Now: func() time.Time { return fixedNow }}
	if _, err := v.Verify(context.Background(), signRS256(t, a, goodHeader(), validPayload())); err != nil {
		t.Fatal(err)
	}
}

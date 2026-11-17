package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	h := NewHandler(Deps{Version: "test"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	var body struct {
		Data healthResponse `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Status != "ok" || body.Data.Version != "test" {
		t.Fatalf("body = %+v", body.Data)
	}
}

func TestUnknownAPIRouteReturnsJSONError(t *testing.T) {
	h := NewHandler(Deps{})
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/nope"},
		{http.MethodPost, "/api/v1/health"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: status = %d", tc.method, tc.path, rec.Code)
		}
		if rec.Code == http.StatusNotFound {
			var body errorEnvelope
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Error.Code != "not_found" {
				t.Errorf("%s %s: want JSON not_found error, got %q (%v)", tc.method, tc.path, rec.Body.String(), err)
			}
		}
	}
}

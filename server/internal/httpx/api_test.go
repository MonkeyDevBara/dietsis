package httpx

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MonkeyDevBara/dietsis/server/internal/limit"
)

const (
	idA = "11111111-1111-4111-8111-111111111111"
	idB = "22222222-2222-4222-8222-222222222222"
	idC = "33333333-3333-4333-8333-333333333333"
)

type client struct {
	e      *env
	cookie *http.Cookie
}

func (e *env) user(token string) *client {
	e.t.Helper()
	rec, c := e.login(token)
	if rec.Code != http.StatusOK || c == nil {
		e.t.Fatalf("login %s failed: %d %s", token, rec.Code, rec.Body)
	}
	return &client{e: e, cookie: c}
}

func (c *client) do(method, path, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	return c.e.do(method, path, body, append([]reqOpt{withCookie(c.cookie)}, opts...)...)
}

func (c *client) ok(method, path, body string, want int, opts ...reqOpt) *httptest.ResponseRecorder {
	c.e.t.Helper()
	rec := c.do(method, path, body, opts...)
	if rec.Code != want {
		c.e.t.Fatalf("%s %s = %d, want %d: %s", method, path, rec.Code, want, rec.Body)
	}
	return rec
}

func data[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var env struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return env.Data
}

func idem(k string) reqOpt { return withHeader("Idempotency-Key", k) }

func logBody(id string, extra ...string) string {
	b := fmt.Sprintf(`{"id":%q,"date":"2026-11-17","meal":"lunch","food_id":"b:nasi-putih","name":"Nasi putih","serving_label":"1 porsi","serving_grams":150,"quantity":1.5,"kcal":195,"protein_g":4,"carbs_g":42,"fat_g":0.5`, id)
	for _, e := range extra {
		b += "," + e
	}
	return b + "}"
}

type logJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Meal      string `json:"meal"`
	Date      string `json:"date"`
	Version   int64  `json:"version"`
	DeletedAt *int64 `json:"deleted_at"`
	Quantity  float64
	Totals    struct {
		Kcal float64 `json:"kcal"`
	} `json:"totals"`
}

// ---- access control ----

func TestEveryBusinessRouteRequiresASession(t *testing.T) {
	l := DefaultLimits()
	l.authFail = limit.New(6000, 1000) // this test deliberately sends many unauthenticated requests
	e := newEnv(t, envOpts{limits: &l})
	routes := []struct{ method, path, body string }{
		{"GET", "/api/v1/profile", ""}, {"PUT", "/api/v1/profile", "{}"},
		{"GET", "/api/v1/favorites", ""}, {"PUT", "/api/v1/favorites", `{"food_ids":[]}`},
		{"GET", "/api/v1/days?from=2026-11-01&to=2026-11-02", ""}, {"GET", "/api/v1/days/2026-11-17", ""},
		{"PUT", "/api/v1/days/2026-11-17", "{}"}, {"POST", "/api/v1/days/2026-11-17/water", `{"delta":1}`},
		{"GET", "/api/v1/food-logs?from=2026-11-01&to=2026-11-02", ""}, {"POST", "/api/v1/food-logs", "{}"},
		{"PATCH", "/api/v1/food-logs/" + idA, "{}"}, {"DELETE", "/api/v1/food-logs/" + idA, ""},
		{"POST", "/api/v1/food-logs/" + idA + "/restore", ""},
		{"GET", "/api/v1/custom-foods", ""}, {"POST", "/api/v1/custom-foods", "{}"},
		{"GET", "/api/v1/weights", ""}, {"PUT", "/api/v1/weights/2026-11-17", "{}"}, {"DELETE", "/api/v1/weights/2026-11-17", ""},
		{"POST", "/api/v1/sync/push", `{"data_epoch":1,"ops":[]}`}, {"GET", "/api/v1/sync/pull", ""},
		{"GET", "/api/v1/export", ""}, {"DELETE", "/api/v1/account/data", `{"confirm":"HAPUS"}`},
	}
	for _, r := range routes {
		rec := e.do(r.method, r.path, r.body, withHeader("X-Real-IP", "x"))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session = %d, want 401", r.method, r.path, rec.Code)
		}
		// A user id supplied by the client must never substitute for a session.
		rec = e.do(r.method, r.path, r.body, withHeader("X-User-Id", "u1"), withCookie(&http.Cookie{Name: "user_id", Value: "u1"}))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with a fake user id = %d, want 401", r.method, r.path, rec.Code)
		}
	}
}

func TestMutationsRequireSameOrigin(t *testing.T) {
	e := newEnv(t, envOpts{})
	u := e.user("tok-rina")
	rec := u.do("POST", "/api/v1/food-logs", logBody(idA), withHeader("Origin", "https://evil.example"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
	rec = u.do("DELETE", "/api/v1/account/data", `{"confirm":"HAPUS"}`, withHeader("Origin", "https://evil.example"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("delete-all via CSRF: %d", rec.Code)
	}
	rec = u.do("POST", "/api/v1/food-logs", logBody(idA), withHeader("Content-Type", "text/plain"))
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("non-JSON body: %d", rec.Code)
	}
	if got := u.ok("GET", "/api/v1/food-logs?from=2026-11-01&to=2026-11-30", "", 200); len(data[[]logJSON](t, got)) != 0 {
		t.Fatal("blocked requests created data")
	}
}

// ---- profile ----

func TestProfileFlow(t *testing.T) {
	e := newEnv(t, envOpts{})
	u := e.user("tok-rina")
	if rec := u.ok("GET", "/api/v1/profile", "", 404); errCode(t, rec) != "not_found" {
		t.Fatal("want not_found")
	}
	body := `{"display_name":"Rina","gender":"female","age":30,"height_cm":160,"weight_kg":70,"goal_weight_kg":60,"activity_level":"moderate","calorie_target":0,"protein_target_g":0,"water_target_glasses":0,"calorie_target_manual":false,"protein_target_manual":false,"water_target_manual":false}`
	rec := u.ok("PUT", "/api/v1/profile", body, 200)
	p := data[struct {
		CalorieTarget int   `json:"calorie_target"`
		Version       int64 `json:"version"`
	}](t, rec)
	if p.CalorieTarget != 1650 || p.Version != 1 {
		t.Fatalf("%+v", p)
	}
	u.ok("GET", "/api/v1/profile", "", 200)
	if rec := u.ok("GET", "/api/v1/auth/me", "", 200); !strings.Contains(rec.Body.String(), `"has_profile":true`) {
		t.Fatal("has_profile should now be true")
	}

	stale := strings.Replace(body, `"calorie_target":0`, `"calorie_target":0,"base_version":9`, 1)
	rec = u.ok("PUT", "/api/v1/profile", stale, 409)
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Current struct {
				Version int64 `json:"version"`
			} `json:"current"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "conflict" || env.Error.Current.Version != 1 {
		t.Fatalf("409 must carry error.current: %s", rec.Body)
	}

	bad := strings.Replace(body, `"age":30`, `"age":10`, 1)
	rec = u.ok("PUT", "/api/v1/profile", bad, 422)
	if !strings.Contains(rec.Body.String(), `"age"`) || errCode(t, rec) != "validation_failed" {
		t.Fatalf("422 needs field details: %s", rec.Body)
	}
	u.ok("PUT", "/api/v1/profile", strings.Replace(body, `"gender"`, `"user_id":"x","gender"`, 1), 400)
}

// ---- food logs ----

func TestFoodLogHTTPLifecycle(t *testing.T) {
	e := newEnv(t, envOpts{})
	u := e.user("tok-rina")

	rec := u.ok("POST", "/api/v1/food-logs", logBody(idA), 201)
	l := data[logJSON](t, rec)
	if l.ID != idA || l.Version != 1 || l.Totals.Kcal != 293 {
		t.Fatalf("%+v", l)
	}
	if l2 := data[logJSON](t, u.ok("POST", "/api/v1/food-logs", logBody(idA, `"name":"lain"`), 200)); l2.Name != "Nasi putih" || l2.Version != 1 {
		t.Fatalf("duplicate id must return the existing entry with 200: %+v", l2)
	}
	// Idempotency-Key retry of a create answers 200 (replay), not a second 201.
	u.ok("POST", "/api/v1/food-logs", logBody(idB), 201, idem("create-b"))
	u.ok("POST", "/api/v1/food-logs", logBody(idB), 200, idem("create-b"))

	// Server-assigned id when the client sends none.
	noID := strings.Replace(logBody(idC), `"id":"`+idC+`",`, "", 1)
	if got := data[logJSON](t, u.ok("POST", "/api/v1/food-logs", noID, 201)); len(got.ID) != 36 {
		t.Fatalf("server id = %q", got.ID)
	}

	rec = u.ok("POST", "/api/v1/food-logs", logBody("33333333-3333-4333-8333-333333333334", `"meal":"brunch"`), 422)
	if !strings.Contains(rec.Body.String(), `"meal"`) {
		t.Fatalf("details: %s", rec.Body)
	}
	u.ok("POST", "/api/v1/food-logs", `{"id":"`+idC+`"`, 400)
	u.ok("POST", "/api/v1/food-logs", "", 400)

	// PATCH
	if got := data[logJSON](t, u.ok("PATCH", "/api/v1/food-logs/"+idA, `{"quantity":2,"base_version":1}`, 200)); got.Version != 2 || got.Totals.Kcal != 390 {
		t.Fatalf("%+v", got)
	}
	rec = u.ok("PATCH", "/api/v1/food-logs/"+idA, `{"meal":"dinner","base_version":1}`, 409)
	if !strings.Contains(rec.Body.String(), `"current"`) || !strings.Contains(rec.Body.String(), `"version":2`) {
		t.Fatalf("conflict body: %s", rec.Body)
	}
	u.ok("PATCH", "/api/v1/food-logs/"+idA, `{"id":"x","quantity":2}`, 400)
	u.ok("PATCH", "/api/v1/food-logs/"+idA, `{"food_id":"b:x"}`, 400)
	u.ok("PATCH", "/api/v1/food-logs/"+idA, `{"quantity":999}`, 422)
	u.ok("PATCH", "/api/v1/food-logs/"+idA, `{}`, 400)
	u.ok("PATCH", "/api/v1/food-logs/44444444-4444-4444-8444-444444444444", `{"quantity":2}`, 404)
	u.ok("PATCH", "/api/v1/food-logs/short", `{"quantity":2}`, 400)

	// DELETE / restore
	if got := data[logJSON](t, u.ok("DELETE", "/api/v1/food-logs/"+idA, "", 200)); got.DeletedAt == nil {
		t.Fatal("deleted_at must be set")
	}
	u.ok("DELETE", "/api/v1/food-logs/"+idA, "", 200)
	u.ok("DELETE", "/api/v1/food-logs/44444444-4444-4444-8444-444444444444", "", 404)
	u.ok("PATCH", "/api/v1/food-logs/"+idA, `{"quantity":3}`, 409)
	if got := data[logJSON](t, u.ok("POST", "/api/v1/food-logs/"+idA+"/restore", "", 200)); got.DeletedAt != nil {
		t.Fatal("restore must clear deleted_at")
	}
	u.ok("POST", "/api/v1/food-logs/44444444-4444-4444-8444-444444444444/restore", "", 404)
	u.ok("DELETE", "/api/v1/food-logs/"+idA, `{"x":1}`, 400)
}

func TestFoodLogListing(t *testing.T) {
	e := newEnv(t, envOpts{})
	u := e.user("tok-rina")
	for i := range 5 {
		u.ok("POST", "/api/v1/food-logs", logBody(fmt.Sprintf("%08d-0000-4000-8000-000000000000", i+1), fmt.Sprintf(`"name":"Item %d"`, i)), 201)
	}
	u.ok("DELETE", "/api/v1/food-logs/00000000-0000-4000-8000-000000000000", "", 404)
	u.ok("DELETE", "/api/v1/food-logs/00000002-0000-4000-8000-000000000000", "", 200)

	type page struct {
		Data       []logJSON `json:"data"`
		NextCursor *string   `json:"next_cursor"`
	}
	var all []string
	url := "/api/v1/food-logs?from=2026-11-01&to=2026-11-30&limit=2"
	for range 5 {
		var p page
		_ = json.Unmarshal(u.ok("GET", url, "", 200).Body.Bytes(), &p)
		for _, l := range p.Data {
			all = append(all, l.Name)
		}
		if p.NextCursor == nil {
			break
		}
		url = "/api/v1/food-logs?from=2026-11-01&to=2026-11-30&limit=2&cursor=" + *p.NextCursor
	}
	if len(all) != 4 {
		t.Fatalf("listing (deleted hidden) = %v", all)
	}
	var withDeleted page
	_ = json.Unmarshal(u.ok("GET", "/api/v1/food-logs?from=2026-11-01&to=2026-11-30&include_deleted=true", "", 200).Body.Bytes(), &withDeleted)
	if len(withDeleted.Data) != 5 || withDeleted.NextCursor != nil {
		t.Fatalf("include_deleted: %d, cursor %v", len(withDeleted.Data), withDeleted.NextCursor)
	}

	for name, q := range map[string]string{
		"missing from":   "to=2026-11-30",
		"missing to":     "from=2026-11-01",
		"bad date":       "from=2026-13-01&to=2026-11-30",
		"reversed":       "from=2026-11-30&to=2026-11-01",
		"range too long": "from=2025-01-01&to=2026-11-30",
		"limit too big":  "from=2026-11-01&to=2026-11-30&limit=501",
		"limit zero":     "from=2026-11-01&to=2026-11-30&limit=0",
		"limit text":     "from=2026-11-01&to=2026-11-30&limit=abc",
		"bad cursor":     "from=2026-11-01&to=2026-11-30&cursor=%21%21",
	} {
		if rec := u.do("GET", "/api/v1/food-logs?"+q, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", name, rec.Code)
		}
	}
}

// ---- water ----

func TestWaterEndpointRequiresIdempotencyKeyAndReplaysRetries(t *testing.T) {
	e := newEnv(t, envOpts{})
	u := e.user("tok-rina")
	water := func(rec *httptest.ResponseRecorder) int {
		return data[struct {
			Water int `json:"water_glasses"`
		}](t, rec).Water
	}
	if rec := u.ok("POST", "/api/v1/days/2026-11-17/water", `{"delta":1}`, 428); errCode(t, rec) != "idempotency_key_required" {
		t.Fatalf("%s", rec.Body)
	}
	u.ok("POST", "/api/v1/days/2026-11-17/water", `{"delta":1}`, 400, idem("bad key with spaces"))

	if got := water(u.ok("POST", "/api/v1/days/2026-11-17/water", `{"delta":1}`, 200, idem("tap-1"))); got != 1 {
		t.Fatalf("water = %d", got)
	}
	// Response lost → the client retries with the same key: still 1.
	for range 3 {
		if got := water(u.ok("POST", "/api/v1/days/2026-11-17/water", `{"delta":1}`, 200, idem("tap-1"))); got != 1 {
			t.Fatalf("retry changed the total to %d", got)
		}
	}
	if got := water(u.ok("POST", "/api/v1/days/2026-11-17/water", `{"delta":2}`, 200, idem("tap-2"))); got != 3 {
		t.Fatalf("water = %d, want 3", got)
	}
	if got := water(u.ok("POST", "/api/v1/days/2026-11-17/water", `{"delta":-1}`, 200, idem("tap-3"))); got != 2 {
		t.Fatalf("water = %d, want 2", got)
	}
	u.ok("POST", "/api/v1/days/2026-11-17/water", `{"delta":0}`, 422, idem("z"))
	u.ok("POST", "/api/v1/days/2026-11-17/water", `{"delta":99}`, 422, idem("z2"))
	u.ok("POST", "/api/v1/days/2026-11-17/water", `{"delta":1,"date":"2026-11-16"}`, 400, idem("z3")) // URL decides the date
	u.ok("POST", "/api/v1/days/2030-01-01/water", `{"delta":1}`, 400, idem("z4"))
	// The same key reused for a different operation is refused, not answered with the old result.
	u.ok("PUT", "/api/v1/weights/2026-11-17", `{"weight_kg":70}`, 400, idem("tap-1"))
}

func TestDayEndpoints(t *testing.T) {
	e := newEnv(t, envOpts{})
	u := e.user("tok-rina")
	type day struct {
		Water   int    `json:"water_glasses"`
		Steps   *int   `json:"steps"`
		Note    string `json:"note"`
		Version int64  `json:"version"`
	}
	if d := data[day](t, u.ok("GET", "/api/v1/days/2026-11-17", "", 200)); d.Water != 0 || d.Steps != nil || d.Version != 0 {
		t.Fatalf("never-written day must return defaults: %+v", d)
	}
	d := data[day](t, u.ok("PUT", "/api/v1/days/2026-11-17", `{"steps":5200,"note":"Jalan pagi","base_version":0}`, 200))
	if *d.Steps != 5200 || d.Note != "Jalan pagi" || d.Version != 1 {
		t.Fatalf("%+v", d)
	}
	u.ok("PUT", "/api/v1/days/2026-11-17", `{"steps":1,"base_version":0}`, 409)
	if d := data[day](t, u.ok("PUT", "/api/v1/days/2026-11-17", `{"steps":null}`, 200)); d.Steps != nil || d.Note != "Jalan pagi" {
		t.Fatalf("explicit null clears only steps: %+v", d)
	}
	u.ok("PUT", "/api/v1/days/2026-11-17", `{"water_glasses":50}`, 400)
	u.ok("PUT", "/api/v1/days/2026-11-17", `{"steps":-5}`, 422)
	u.ok("PUT", "/api/v1/days/2026-11-17", `{"date":"2026-11-16","steps":5}`, 400)
	days := data[[]day](t, u.ok("GET", "/api/v1/days?from=2026-11-01&to=2026-11-30", "", 200))
	if len(days) != 1 {
		t.Fatalf("days = %d", len(days))
	}
	u.ok("GET", "/api/v1/days/2026-02-30", "", 400)
	u.ok("GET", "/api/v1/days?from=2026-11-01", "", 400)
}

// ---- weights / custom foods / favorites ----

func TestWeightEndpoints(t *testing.T) {
	e := newEnv(t, envOpts{})
	u := e.user("tok-rina")
	type w struct {
		Date    string   `json:"date"`
		Weight  float64  `json:"weight_kg"`
		Waist   *float64 `json:"waist_cm"`
		Version int64    `json:"version"`
	}
	if got := data[w](t, u.ok("PUT", "/api/v1/weights/2026-11-17", `{"weight_kg":72.4,"waist_cm":80,"base_version":0}`, 200)); got.Version != 1 || *got.Waist != 80 {
		t.Fatalf("%+v", got)
	}
	if got := data[w](t, u.ok("PUT", "/api/v1/weights/2026-11-17", `{"weight_kg":72.0}`, 200)); got.Version != 2 || got.Waist != nil {
		t.Fatalf("PUT is a full upsert: %+v", got)
	}
	u.ok("PUT", "/api/v1/weights/2026-11-17", `{"weight_kg":71,"base_version":1}`, 409)
	u.ok("PUT", "/api/v1/weights/2026-11-17", `{"weight_kg":10}`, 422)
	u.ok("PUT", "/api/v1/weights/2026-11-17", `{"weight_kg":70,"date":"2026-11-01"}`, 400)
	u.ok("PUT", "/api/v1/weights/2026-11-16", `{"weight_kg":73}`, 200)

	var list struct {
		Data []w `json:"data"`
	}
	_ = json.Unmarshal(u.ok("GET", "/api/v1/weights", "", 200).Body.Bytes(), &list)
	if len(list.Data) != 2 || list.Data[0].Date != "2026-11-16" {
		t.Fatalf("weights ascending by date: %+v", list.Data)
	}
	u.ok("DELETE", "/api/v1/weights/2026-11-17", `{"base_version":99}`, 409)
	u.ok("DELETE", "/api/v1/weights/2026-11-17", "", 204)
	u.ok("DELETE", "/api/v1/weights/2026-11-17", "", 204) // idempotent
	_ = json.Unmarshal(u.ok("GET", "/api/v1/weights", "", 200).Body.Bytes(), &list)
	if len(list.Data) != 1 {
		t.Fatalf("after delete: %+v", list.Data)
	}
}

func TestCustomFoodEndpoints(t *testing.T) {
	e := newEnv(t, envOpts{})
	u := e.user("tok-rina")
	body := func(id string) string {
		idf := ""
		if id != "" {
			idf = fmt.Sprintf(`"id":%q,`, id)
		}
		return `{` + idf + `"name":"Smoothie bayam","serving_label":"1 gelas","serving_grams":300,"kcal":180,"protein_g":6,"carbs_g":30,"fat_g":3}`
	}
	u.ok("POST", "/api/v1/custom-foods", body(idA), 201)
	u.ok("POST", "/api/v1/custom-foods", body(idA), 200)
	u.ok("POST", "/api/v1/custom-foods", body(""), 201)
	u.ok("POST", "/api/v1/custom-foods", strings.Replace(body(idB), `"kcal":180`, `"kcal":-1`, 1), 422)
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(u.ok("GET", "/api/v1/custom-foods", "", 200).Body.Bytes(), &list)
	if len(list.Data) != 2 {
		t.Fatalf("custom foods = %d", len(list.Data))
	}
}

func TestFavoritesEndpoints(t *testing.T) {
	e := newEnv(t, envOpts{})
	u := e.user("tok-rina")
	type fav struct {
		FoodID string `json:"food_id"`
	}
	got := data[[]fav](t, u.ok("PUT", "/api/v1/favorites", `{"food_ids":["b:pisang","b:apel","b:pisang"]}`, 200))
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	got = data[[]fav](t, u.ok("PUT", "/api/v1/favorites", `{"food_ids":["b:apel"]}`, 200))
	if len(got) != 1 || got[0].FoodID != "b:apel" {
		t.Fatalf("%+v", got)
	}
	u.ok("PUT", "/api/v1/favorites", `{"food_ids":["nope"]}`, 422)
	u.ok("PUT", "/api/v1/favorites", `{"food_ids":null,"extra":1}`, 400)
	if got := data[[]fav](t, u.ok("GET", "/api/v1/favorites", "", 200)); len(got) != 1 {
		t.Fatalf("%+v", got)
	}
}

// ---- isolation between users, over HTTP ----

func TestUsersCannotReachEachOthersData(t *testing.T) {
	e := newEnv(t, envOpts{})
	rina := e.user("tok-rina")
	budi := e.user("tok-budi")
	rina.ok("POST", "/api/v1/food-logs", logBody(idA), 201)
	rina.ok("PUT", "/api/v1/weights/2026-11-17", `{"weight_kg":70}`, 200)
	rina.ok("POST", "/api/v1/days/2026-11-17/water", `{"delta":4}`, 200, idem("r1"))

	// Budi targets Rina's ids on every route.
	budi.ok("PATCH", "/api/v1/food-logs/"+idA, `{"quantity":50}`, 404)
	budi.ok("DELETE", "/api/v1/food-logs/"+idA, "", 404)
	budi.ok("POST", "/api/v1/food-logs/"+idA+"/restore", "", 404)
	budi.ok("DELETE", "/api/v1/weights/2026-11-17", "", 204) // idempotent "not there" for Budi

	// Rina's data is untouched.
	rinaLogs := data[[]logJSON](t, rina.ok("GET", "/api/v1/food-logs?from=2026-11-01&to=2026-11-30", "", 200))
	if len(rinaLogs) != 1 || rinaLogs[0].Version != 1 || rinaLogs[0].DeletedAt != nil {
		t.Fatalf("Rina's log was modified: %+v", rinaLogs)
	}
	if rec := rina.ok("GET", "/api/v1/weights", "", 200); !strings.Contains(rec.Body.String(), "2026-11-17") {
		t.Fatal("Rina's weight was deleted by Budi")
	}

	// Budi sees none of it.
	if l := data[[]logJSON](t, budi.ok("GET", "/api/v1/food-logs?from=2026-11-01&to=2026-11-30", "", 200)); len(l) != 0 {
		t.Fatalf("Budi sees Rina's logs: %+v", l)
	}
	if rec := budi.ok("GET", "/api/v1/weights", "", 200); strings.Contains(rec.Body.String(), "2026-11-17") {
		t.Fatal("Budi sees Rina's weights")
	}
	if rec := budi.ok("GET", "/api/v1/days/2026-11-17", "", 200); !strings.Contains(rec.Body.String(), `"water_glasses":0`) {
		t.Fatalf("Budi sees Rina's water: %s", rec.Body)
	}
	if rec := budi.ok("GET", "/api/v1/sync/pull", "", 200); strings.Contains(rec.Body.String(), idA) {
		t.Fatal("Budi's pull contains Rina's entities")
	}
	if rec := budi.ok("GET", "/api/v1/export?dataset=food_logs", "", 200); strings.Contains(rec.Body.String(), "Nasi putih") {
		t.Fatal("Budi's export contains Rina's data")
	}

	// Budi can use Rina's id for his own, independent entry.
	budi.ok("POST", "/api/v1/food-logs", strings.Replace(logBody(idA), "Nasi putih", "Punya Budi", 1), 201)
	if l := data[[]logJSON](t, rina.ok("GET", "/api/v1/food-logs?from=2026-11-01&to=2026-11-30", "", 200)); l[0].Name != "Nasi putih" {
		t.Fatal("Rina's entry was overwritten")
	}

	// A session cannot be used with someone else's id in a body or query either.
	budi.ok("POST", "/api/v1/food-logs", strings.Replace(logBody(idB), `"id"`, `"user_id":"`+idA+`","id"`, 1), 400)
}

// ---- sync ----

func pushBody(epoch int, ops ...string) string {
	return fmt.Sprintf(`{"data_epoch":%d,"ops":[%s]}`, epoch, strings.Join(ops, ","))
}

func op(id, typ, payload string) string {
	return fmt.Sprintf(`{"op_id":%q,"type":%q,"payload":%s}`, id, typ, payload)
}

type pushResult struct {
	OpID   string          `json:"op_id"`
	Status string          `json:"status"`
	Entity string          `json:"entity"`
	Key    string          `json:"key"`
	State  json.RawMessage `json:"state"`
	Error  *struct {
		Code string `json:"code"`
	} `json:"error"`
}

type pushData struct {
	DataEpoch int          `json:"data_epoch"`
	Results   []pushResult `json:"results"`
}

func TestSyncPushStatusesAndReplay(t *testing.T) {
	e := newEnv(t, envOpts{})
	u := e.user("tok-rina")
	body := pushBody(1,
		op("o1", "food_log.create", `{"id":"`+idA+`","date":"2026-11-17","meal":"lunch","food_id":null,"name":"Nasi","serving_label":"1","serving_grams":null,"quantity":1,"kcal":100,"protein_g":1,"carbs_g":1,"fat_g":1}`),
		op("o2", "day.water", `{"date":"2026-11-17","delta":2}`),
		op("o3", "food_log.update", `{"id":"`+idA+`","base_version":1,"patch":{"quantity":2}}`),
		op("o4", "food_log.update", `{"id":"`+idA+`","base_version":1,"patch":{"quantity":3}}`), // stale after o3
		op("o5", "weight.put", `{"date":"2026-11-17","weight_kg":5}`),                           // invalid
		op("o6", "nope.nope", `{}`),
	)
	res := data[pushData](t, u.ok("POST", "/api/v1/sync/push", body, 200))
	want := []string{"applied", "applied", "applied", "conflict", "rejected", "rejected"}
	if len(res.Results) != len(want) || res.DataEpoch != 1 {
		t.Fatalf("%+v", res)
	}
	for i, w := range want {
		if res.Results[i].Status != w || res.Results[i].OpID != fmt.Sprintf("o%d", i+1) {
			t.Errorf("result %d = %+v, want %s", i+1, res.Results[i], w)
		}
	}
	if res.Results[4].Error == nil || res.Results[4].Error.Code != "validation_failed" {
		t.Fatalf("%+v", res.Results[4])
	}
	if !strings.Contains(string(res.Results[3].State), `"quantity":2`) {
		t.Fatalf("conflict carries the server state: %s", res.Results[3].State)
	}

	// The whole batch is re-sent (the response was lost). Nothing is applied twice.
	again := data[pushData](t, u.ok("POST", "/api/v1/sync/push", body, 200))
	for i := range want {
		if again.Results[i].Status != res.Results[i].Status {
			t.Errorf("replay %d: %s vs %s", i+1, again.Results[i].Status, res.Results[i].Status)
		}
	}
	if d := data[struct {
		Water int `json:"water_glasses"`
	}](t, u.ok("GET", "/api/v1/days/2026-11-17", "", 200)); d.Water != 2 {
		t.Fatalf("water = %d after replay, want 2", d.Water)
	}
	if l := data[[]logJSON](t, u.ok("GET", "/api/v1/food-logs?from=2026-11-01&to=2026-11-30", "", 200)); len(l) != 1 || l[0].Version != 2 {
		t.Fatalf("food log after replay: %+v", l)
	}
}

func TestSyncPushValidation(t *testing.T) {
	e := newEnv(t, envOpts{})
	u := e.user("tok-rina")
	water := op("w", "day.water", `{"date":"2026-11-17","delta":1}`)
	for name, body := range map[string]string{
		"no epoch":        `{"ops":[` + water + `]}`,
		"no ops":          pushBody(1),
		"op without id":   pushBody(1, `{"type":"day.water","payload":{"date":"2026-11-17","delta":1}}`),
		"op without type": pushBody(1, `{"op_id":"x","payload":{}}`),
		"unknown field":   `{"data_epoch":1,"ops":[` + water + `],"user_id":"x"}`,
		"bad op field":    pushBody(1, `{"op_id":"x","id":"y","type":"day.water","payload":{}}`),
		"not json":        `[`,
	} {
		if rec := u.do("POST", "/api/v1/sync/push", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", name, rec.Code)
		}
	}
	many := make([]string, 201)
	for i := range many {
		many[i] = op(fmt.Sprintf("m%d", i), "day.water", `{"date":"2026-11-17","delta":1}`)
	}
	if rec := u.do("POST", "/api/v1/sync/push", pushBody(1, many...)); rec.Code != http.StatusBadRequest {
		t.Errorf("201 ops: %d", rec.Code)
	}
	if rec := u.do("POST", "/api/v1/sync/push", pushBody(1, many[:200]...)); rec.Code != http.StatusOK {
		t.Errorf("200 ops must pass: %d %s", rec.Code, rec.Body)
	}
	huge := pushBody(1, op("big", "day.update", `{"date":"2026-11-17","patch":{"note":"`+strings.Repeat("a", 1<<20)+`"}}`))
	if rec := u.do("POST", "/api/v1/sync/push", huge); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("over 1 MiB: %d", rec.Code)
	}
	// Normal endpoints keep the small limit.
	if rec := u.do("PUT", "/api/v1/days/2026-11-17", `{"note":"`+strings.Repeat("a", 70<<10)+`"}`); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("64 KiB limit on normal routes: %d", rec.Code)
	}
}

func TestSyncPushRateLimit(t *testing.T) {
	l := DefaultLimits()
	l.push = limit.New(60, 2)
	e := newEnv(t, envOpts{limits: &l})
	u := e.user("tok-rina")
	body := pushBody(1, op("a", "day.water", `{"date":"2026-11-17","delta":1}`))
	u.ok("POST", "/api/v1/sync/push", body, 200)
	u.ok("POST", "/api/v1/sync/push", body, 200)
	rec := u.ok("POST", "/api/v1/sync/push", body, 429)
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 needs Retry-After")
	}
}

func TestSyncPull(t *testing.T) {
	e := newEnv(t, envOpts{})
	u := e.user("tok-rina")
	u.ok("POST", "/api/v1/food-logs", logBody(idA), 201)
	u.ok("PUT", "/api/v1/weights/2026-11-17", `{"weight_kg":70}`, 200)
	u.ok("DELETE", "/api/v1/weights/2026-11-17", "", 204)

	type pull struct {
		DataEpoch int  `json:"data_epoch"`
		Cursor    int  `json:"cursor"`
		HasMore   bool `json:"has_more"`
		Changes   []struct {
			Seq    int             `json:"seq"`
			Entity string          `json:"entity"`
			Key    string          `json:"key"`
			State  json.RawMessage `json:"state"`
		} `json:"changes"`
	}
	full := data[pull](t, u.ok("GET", "/api/v1/sync/pull", "", 200))
	if full.DataEpoch != 1 || len(full.Changes) != 2 || full.HasMore {
		t.Fatalf("%+v", full)
	}
	if full.Changes[1].Entity != "weight" || string(full.Changes[1].State) != "null" {
		t.Fatalf("deleted weight must be state null: %+v", full.Changes[1])
	}
	page := data[pull](t, u.ok("GET", "/api/v1/sync/pull?limit=1", "", 200))
	if len(page.Changes) != 1 || !page.HasMore || page.Cursor != page.Changes[0].Seq {
		t.Fatalf("%+v", page)
	}
	rest := data[pull](t, u.ok("GET", fmt.Sprintf("/api/v1/sync/pull?since=%d", page.Cursor), "", 200))
	if len(rest.Changes) != 1 || rest.HasMore {
		t.Fatalf("%+v", rest)
	}
	for _, q := range []string{"since=-1", "since=abc", "limit=0", "limit=1001"} {
		if rec := u.do("GET", "/api/v1/sync/pull?"+q, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", q, rec.Code)
		}
	}
}

// Two devices of one user work offline, then sync: the end states converge and nothing is lost.
func TestOfflineDevicesConverge(t *testing.T) {
	e := newEnv(t, envOpts{})
	phone := e.user("tok-rina")
	laptop := e.user("tok-rina")

	// Phone, offline: logs lunch, drinks water twice, weighs in. Then reconnects.
	phone.ok("POST", "/api/v1/sync/push", pushBody(1,
		op("p1", "food_log.create", `{"id":"`+idA+`","date":"2026-11-17","meal":"lunch","food_id":"b:nasi-putih","name":"Nasi","serving_label":"1","serving_grams":null,"quantity":1,"kcal":200,"protein_g":4,"carbs_g":40,"fat_g":1}`),
		op("p2", "day.water", `{"date":"2026-11-17","delta":1}`),
		op("p3", "day.water", `{"date":"2026-11-17","delta":1}`),
		op("p4", "weight.put", `{"date":"2026-11-17","weight_kg":72,"base_version":0}`),
	), 200)

	// Laptop, offline at the same time: water too, and weighs in on the same date.
	res := data[pushData](t, laptop.ok("POST", "/api/v1/sync/push", pushBody(1,
		op("l1", "day.water", `{"date":"2026-11-17","delta":3}`),
		op("l2", "weight.put", `{"date":"2026-11-17","weight_kg":71.5,"base_version":0}`),
	), 200))
	if res.Results[0].Status != "applied" || res.Results[1].Status != "conflict" {
		t.Fatalf("water adds up, the weight conflicts: %+v", res.Results)
	}

	// Both devices pull and end up with identical state.
	read := func(c *client) string {
		type snap struct {
			Water  int
			Weight string
			Logs   int
		}
		d := data[struct {
			Water int `json:"water_glasses"`
		}](t, c.ok("GET", "/api/v1/days/2026-11-17", "", 200))
		ws := c.ok("GET", "/api/v1/weights", "", 200).Body.String()
		logs := data[[]logJSON](t, c.ok("GET", "/api/v1/food-logs?from=2026-11-01&to=2026-11-30", "", 200))
		var wl struct {
			Data []struct {
				Weight float64 `json:"weight_kg"`
			} `json:"data"`
		}
		_ = json.Unmarshal([]byte(ws), &wl)
		return fmt.Sprintf("%+v", snap{d.Water, fmt.Sprint(wl.Data), len(logs)})
	}
	if a, b := read(phone), read(laptop); a != b || !strings.Contains(a, "Water:5") || !strings.Contains(a, "[{72}]") || !strings.Contains(a, "Logs:1") {
		t.Fatalf("devices diverged or lost data: phone=%s laptop=%s", a, b)
	}
}

// ---- export / delete all ----

func TestExportCSV(t *testing.T) {
	e := newEnv(t, envOpts{})
	u := e.user("tok-rina")
	u.ok("POST", "/api/v1/food-logs", strings.Replace(logBody(idA), "Nasi putih", "=cmd", 1), 201)
	rec := u.ok("GET", "/api/v1/export", "", 200)
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Fatalf("content-type %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="dietsis-food_logs-`) || !strings.HasSuffix(cd, `.csv"`) {
		t.Fatalf("content-disposition %q", cd)
	}
	if !strings.HasPrefix(rec.Body.String(), "\uFEFF") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("BOM and no-store expected")
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(rec.Body.String(), "\uFEFF"))).ReadAll()
	if err != nil || len(rows) != 2 || rows[1][2] != "'=cmd" || rows[1][9] != "293" {
		t.Fatalf("rows = %v (%v)", rows, err)
	}
	for _, ds := range []string{"weights", "days", "custom_foods", "profile", "food_logs"} {
		u.ok("GET", "/api/v1/export?dataset="+ds, "", 200)
	}
	u.ok("GET", "/api/v1/export?dataset=users", "", 400)
	u.ok("GET", "/api/v1/export?dataset=food_logs%27%3B--", "", 400)
}

func TestDeleteAccountData(t *testing.T) {
	e := newEnv(t, envOpts{})
	rina := e.user("tok-rina")
	budi := e.user("tok-budi")
	for _, c := range []*client{rina, budi} {
		c.ok("POST", "/api/v1/food-logs", logBody(idA), 201)
		c.ok("PUT", "/api/v1/weights/2026-11-17", `{"weight_kg":70}`, 200)
	}
	rina.ok("DELETE", "/api/v1/account/data", `{}`, 422)
	rina.ok("DELETE", "/api/v1/account/data", `{"confirm":"hapus"}`, 422)
	rina.ok("DELETE", "/api/v1/account/data", "", 400)
	if l := data[[]logJSON](t, rina.ok("GET", "/api/v1/food-logs?from=2026-11-01&to=2026-11-30", "", 200)); len(l) != 1 {
		t.Fatal("rejected confirmations must not delete anything")
	}

	out := data[struct {
		Epoch int `json:"data_epoch"`
	}](t, rina.ok("DELETE", "/api/v1/account/data", `{"confirm":"HAPUS"}`, 200))
	if out.Epoch != 2 {
		t.Fatalf("epoch = %d", out.Epoch)
	}
	if l := data[[]logJSON](t, rina.ok("GET", "/api/v1/food-logs?from=2026-11-01&to=2026-11-30", "", 200)); len(l) != 0 {
		t.Fatal("food logs survived")
	}
	rina.ok("GET", "/api/v1/profile", "", 404)
	if rec := rina.ok("GET", "/api/v1/auth/me", "", 200); !strings.Contains(rec.Body.String(), `"data_epoch":2`) || !strings.Contains(rec.Body.String(), `"has_profile":false`) {
		t.Fatalf("the account and session stay: %s", rec.Body)
	}
	// A device that was offline with epoch 1 must not bring the data back.
	rec := rina.ok("POST", "/api/v1/sync/push", pushBody(1, op("late", "food_log.create", `{"id":"`+idB+`","date":"2026-11-17","meal":"lunch","food_id":null,"name":"x","serving_label":"1","serving_grams":null,"quantity":1,"kcal":1,"protein_g":0,"carbs_g":0,"fat_g":0}`)), 409)
	if errCode(t, rec) != "epoch_mismatch" || !strings.Contains(rec.Body.String(), `"data_epoch":"2"`) {
		t.Fatalf("%s", rec.Body)
	}
	if l := data[[]logJSON](t, rina.ok("GET", "/api/v1/food-logs?from=2026-11-01&to=2026-11-30", "", 200)); len(l) != 0 {
		t.Fatal("stale push resurrected data")
	}
	// Budi is untouched.
	if l := data[[]logJSON](t, budi.ok("GET", "/api/v1/food-logs?from=2026-11-01&to=2026-11-30", "", 200)); len(l) != 1 {
		t.Fatal("another user's data was deleted")
	}
	budi.ok("POST", "/api/v1/sync/push", pushBody(1, op("ok", "day.water", `{"date":"2026-11-17","delta":1}`)), 200)
}

func TestErrorsNeverLeakInternals(t *testing.T) {
	e := newEnv(t, envOpts{})
	u := e.user("tok-rina")
	e.db.Close() // simulate a dead database
	rec := u.do("GET", "/api/v1/profile", "")
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	for _, leak := range []string{"sql", "database", ".db", "closed"} {
		if strings.Contains(strings.ToLower(rec.Body.String()), leak) {
			t.Fatalf("response leaks %q: %s", leak, rec.Body)
		}
	}
}

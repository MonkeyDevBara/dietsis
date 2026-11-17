package httpx

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/MonkeyDevBara/dietsis/server/internal/export"
	"github.com/MonkeyDevBara/dietsis/server/internal/store"
	dsync "github.com/MonkeyDevBara/dietsis/server/internal/sync"
	"github.com/MonkeyDevBara/dietsis/server/internal/validate"
)

const (
	pushBodyLimit   int64 = 1 << 20
	maxPushOps            = 200
	maxRangeDays          = 366
	maxPullLimit          = 1000
	defaultPull           = 500
	maxListLimit          = 500
	defaultFoodLogs       = 200
	defaultWeights        = 366
	maxWeightsLimit       = 1000
)

type api struct {
	kit    *authKit
	eng    *dsync.Engine
	readDB *sql.DB
}

// registerAPI mounts the authenticated business routes. Every handler runs behind authenticate:
// the user comes from the verified session only, and ids from paths/bodies are always looked up
// together with that user id (never on their own).
func registerAPI(mux *http.ServeMux, kit *authKit, eng *dsync.Engine, readDB *sql.DB) {
	a := &api{kit: kit, eng: eng, readDB: readDB}
	auth := func(max int64, h http.HandlerFunc) http.Handler {
		return limitBody(max, kit.authenticate(h))
	}
	def := func(h http.HandlerFunc) http.Handler { return auth(DefaultBodyLimit, h) }

	mux.Handle("GET /api/v1/profile", def(a.getProfile))
	mux.Handle("PUT /api/v1/profile", def(a.putProfile))

	mux.Handle("GET /api/v1/favorites", def(a.getFavorites))
	mux.Handle("PUT /api/v1/favorites", def(a.putFavorites))

	mux.Handle("GET /api/v1/days", def(a.listDays))
	mux.Handle("GET /api/v1/days/{date}", def(a.getDay))
	mux.Handle("PUT /api/v1/days/{date}", def(a.putDay))
	mux.Handle("POST /api/v1/days/{date}/water", def(a.addWater))

	mux.Handle("GET /api/v1/food-logs", def(a.listFoodLogs))
	mux.Handle("POST /api/v1/food-logs", def(a.createFoodLog))
	mux.Handle("PATCH /api/v1/food-logs/{id}", def(a.patchFoodLog))
	mux.Handle("DELETE /api/v1/food-logs/{id}", def(a.deleteFoodLog))
	mux.Handle("POST /api/v1/food-logs/{id}/restore", def(a.restoreFoodLog))

	mux.Handle("GET /api/v1/custom-foods", def(a.listCustomFoods))
	mux.Handle("POST /api/v1/custom-foods", def(a.createCustomFood))

	mux.Handle("GET /api/v1/weights", def(a.listWeights))
	mux.Handle("PUT /api/v1/weights/{date}", def(a.putWeight))
	mux.Handle("DELETE /api/v1/weights/{date}", def(a.deleteWeight))

	mux.Handle("POST /api/v1/sync/push", auth(pushBodyLimit, a.syncPush))
	mux.Handle("GET /api/v1/sync/pull", def(a.syncPull))

	mux.Handle("GET /api/v1/export", def(a.exportCSV))
	mux.Handle("DELETE /api/v1/account/data", def(a.deleteAccountData))
}

// ---- helpers ----

func userID(r *http.Request) string {
	u, _ := CurrentUser(r.Context())
	return u.ID
}

func badRequest(w http.ResponseWriter, msg string) {
	WriteError(w, http.StatusBadRequest, ErrorBody{Code: "bad_request", Message: msg})
}

func fail500(w http.ResponseWriter, r *http.Request, what string, err error) {
	slog.Error(what, "request_id", RequestID(r.Context()), "err", err)
	internalError(w)
}

// idemKey reads the optional Idempotency-Key header. ok=false means a response was written.
func idemKey(w http.ResponseWriter, r *http.Request, required bool) (key string, ok bool) {
	key = r.Header.Get("Idempotency-Key")
	if key == "" {
		if required {
			WriteError(w, http.StatusPreconditionRequired, ErrorBody{Code: "idempotency_key_required", Message: "Header Idempotency-Key wajib untuk operasi ini."})
			return "", false
		}
		return "", true
	}
	if !dsync.ValidOpID(key) {
		badRequest(w, "Idempotency-Key tidak valid.")
		return "", false
	}
	return key, true
}

// bodyMap reads the JSON body as an object whose values stay raw, so "field absent" and
// "field: null" remain distinguishable for patches.
func bodyMap(w http.ResponseWriter, r *http.Request, optional bool) (map[string]json.RawMessage, bool) {
	var m map[string]json.RawMessage
	if !decodeJSON(w, r, &m, optional) {
		return nil, false
	}
	if m == nil {
		if !optional {
			badRequest(w, "Isi permintaan tidak valid.")
			return nil, false
		}
		m = map[string]json.RawMessage{}
	}
	return m, true
}

func jsonStr(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

// pathParam puts a path value into the payload. A body that also carries the field is refused, so
// the URL and the body can never disagree.
func pathParam(w http.ResponseWriter, m map[string]json.RawMessage, key, val string) bool {
	if _, dup := m[key]; dup {
		badRequest(w, fmt.Sprintf("Field %q ditentukan oleh URL dan tidak boleh ada di body.", key))
		return false
	}
	m[key] = jsonStr(val)
	return true
}

// split moves the named keys out of m into a nested "patch" object; base_version stays top-level.
func split(w http.ResponseWriter, m map[string]json.RawMessage, patchKeys ...string) (map[string]json.RawMessage, bool) {
	patch := map[string]json.RawMessage{}
	allowed := map[string]bool{"base_version": true}
	for _, k := range patchKeys {
		allowed[k] = true
	}
	for k, v := range m {
		if !allowed[k] {
			badRequest(w, fmt.Sprintf("Field tidak dikenal: %q", k))
			return nil, false
		}
		if k != "base_version" {
			patch[k] = v
			delete(m, k)
		}
	}
	pb, _ := json.Marshal(patch)
	m["patch"] = pb
	return m, true
}

// run applies one op built from a payload map and writes the response.
func (a *api) run(w http.ResponseWriter, r *http.Request, typ string, payload map[string]json.RawMessage, idem string, okStatus int, wantEntity bool) {
	raw, err := json.Marshal(payload)
	if err != nil {
		fail500(w, r, "marshal payload", err)
		return
	}
	res, err := a.eng.Apply(r.Context(), userID(r), dsync.Op{ID: idem, Type: typ, Payload: raw}, nil)
	if err != nil {
		fail500(w, r, "apply op", err)
		return
	}
	a.respond(w, res, okStatus, wantEntity)
}

// respond maps an op result to HTTP (docs/API.md → Konvensi):
//
//	rejected → 422 (validation_failed) / 400
//	conflict, entity missing → 404;  conflict, entity exists → 409 with error.current
//	applied, entity missing and the route needs one → 404
//	applied → okStatus (201 only for a genuinely new entity; a replay or duplicate id is 200)
func (a *api) respond(w http.ResponseWriter, res dsync.Result, okStatus int, wantEntity bool) {
	missing := string(res.State) == "null"
	switch {
	case res.Status == dsync.StatusRejected:
		status := http.StatusBadRequest
		if res.Error.Code == dsync.CodeValidation {
			status = http.StatusUnprocessableEntity
		}
		WriteError(w, status, ErrorBody{Code: res.Error.Code, Message: res.Error.Message, Details: res.Error.Details})
	case res.Status == dsync.StatusConflict && missing, res.Status == dsync.StatusApplied && missing && wantEntity:
		WriteError(w, http.StatusNotFound, ErrorBody{Code: "not_found", Message: "Data tidak ditemukan."})
	case res.Status == dsync.StatusConflict:
		WriteError(w, http.StatusConflict, ErrorBody{Code: "conflict", Message: "Data sudah diubah dari perangkat lain.", Current: res.State})
	default:
		if okStatus == http.StatusCreated && (!res.Created || res.Replayed) {
			okStatus = http.StatusOK
		}
		WriteData(w, okStatus, res.State)
	}
}

func dateParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	d := r.PathValue("date")
	if !validate.ValidDate(d, time.Now()) {
		badRequest(w, "Tanggal tidak valid.")
		return "", false
	}
	return d, true
}

func idParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if len(id) != 36 { // exact format is validated by the engine; this only avoids absurd input
		badRequest(w, "ID tidak valid.")
		return "", false
	}
	return id, true
}

// queryDate parses an optional/required date query parameter (any real date from 2000-01-01).
func queryDate(w http.ResponseWriter, r *http.Request, name string, required bool) (string, bool) {
	v := r.URL.Query().Get(name)
	if v == "" {
		if required {
			badRequest(w, fmt.Sprintf("Parameter %q wajib diisi.", name))
			return "", false
		}
		return "", true
	}
	if _, err := time.Parse("2006-01-02", v); err != nil || len(v) != 10 || v < "2000-01-01" {
		badRequest(w, fmt.Sprintf("Parameter %q bukan tanggal yang valid.", name))
		return "", false
	}
	return v, true
}

// dateRange validates from/to (inclusive, max 366 days apart).
func dateRange(w http.ResponseWriter, r *http.Request, required bool) (from, to string, ok bool) {
	if from, ok = queryDate(w, r, "from", required); !ok {
		return
	}
	if to, ok = queryDate(w, r, "to", required); !ok {
		return
	}
	if from != "" && to != "" {
		f, _ := time.Parse("2006-01-02", from)
		t, _ := time.Parse("2006-01-02", to)
		if t.Before(f) {
			badRequest(w, "Parameter \"to\" tidak boleh sebelum \"from\".")
			return "", "", false
		}
		if t.Sub(f) > (maxRangeDays-1)*24*time.Hour {
			badRequest(w, fmt.Sprintf("Rentang maksimal %d hari.", maxRangeDays))
			return "", "", false
		}
	}
	return from, to, true
}

func intQuery(w http.ResponseWriter, r *http.Request, name string, def, min, max int) (int, bool) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		badRequest(w, fmt.Sprintf("Parameter %q harus bilangan bulat antara %d dan %d.", name, min, max))
		return 0, false
	}
	return n, true
}

func listErr(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, dsync.ErrBadCursor) {
		badRequest(w, "Cursor tidak valid.")
		return
	}
	fail500(w, r, "list", err)
}

// ---- profile ----

func (a *api) getProfile(w http.ResponseWriter, r *http.Request) {
	p, err := a.eng.GetProfile(r.Context(), userID(r))
	if err != nil {
		fail500(w, r, "get profile", err)
		return
	}
	if p == nil {
		WriteError(w, http.StatusNotFound, ErrorBody{Code: "not_found", Message: "Profil belum dibuat."})
		return
	}
	WriteData(w, http.StatusOK, p)
}

func (a *api) putProfile(w http.ResponseWriter, r *http.Request) {
	key, ok := idemKey(w, r, false)
	if !ok {
		return
	}
	m, ok := bodyMap(w, r, false)
	if !ok {
		return
	}
	a.run(w, r, dsync.OpProfilePut, m, key, http.StatusOK, false)
}

// ---- favorites ----

func (a *api) getFavorites(w http.ResponseWriter, r *http.Request) {
	favs, err := a.eng.ListFavorites(r.Context(), userID(r))
	if err != nil {
		fail500(w, r, "list favorites", err)
		return
	}
	WriteData(w, http.StatusOK, favs)
}

func (a *api) putFavorites(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FoodIDs []string `json:"food_ids"`
	}
	if !decodeJSON(w, r, &body, false) {
		return
	}
	if len(body.FoodIDs) > dsync.MaxFavorites {
		WriteError(w, http.StatusUnprocessableEntity, ErrorBody{Code: "validation_failed", Message: "Data tidak valid.",
			Details: map[string]string{"food_ids": fmt.Sprintf("Maksimal %d favorit", dsync.MaxFavorites)}})
		return
	}
	for _, id := range body.FoodIDs {
		if !dsync.ValidFoodID(id) {
			WriteError(w, http.StatusUnprocessableEntity, ErrorBody{Code: "validation_failed", Message: "Data tidak valid.",
				Details: map[string]string{"food_ids": "Format food_id tidak valid"}})
			return
		}
	}
	favs, err := a.eng.ReplaceFavorites(r.Context(), userID(r), body.FoodIDs)
	if err != nil {
		fail500(w, r, "replace favorites", err)
		return
	}
	WriteData(w, http.StatusOK, favs)
}

// ---- days ----

func (a *api) listDays(w http.ResponseWriter, r *http.Request) {
	from, to, ok := dateRange(w, r, true)
	if !ok {
		return
	}
	days, err := a.eng.ListDays(r.Context(), userID(r), from, to)
	if err != nil {
		fail500(w, r, "list days", err)
		return
	}
	WriteData(w, http.StatusOK, days)
}

func (a *api) getDay(w http.ResponseWriter, r *http.Request) {
	date, ok := dateParam(w, r)
	if !ok {
		return
	}
	d, err := a.eng.GetDay(r.Context(), userID(r), date)
	if err != nil {
		fail500(w, r, "get day", err)
		return
	}
	WriteData(w, http.StatusOK, d)
}

func (a *api) putDay(w http.ResponseWriter, r *http.Request) {
	date, ok := dateParam(w, r)
	if !ok {
		return
	}
	key, ok := idemKey(w, r, false)
	if !ok {
		return
	}
	m, ok := bodyMap(w, r, false)
	if !ok {
		return
	}
	if m, ok = split(w, m, "steps", "note"); !ok || !pathParam(w, m, "date", date) {
		return
	}
	a.run(w, r, dsync.OpDayUpdate, m, key, http.StatusOK, false)
}

func (a *api) addWater(w http.ResponseWriter, r *http.Request) {
	date, ok := dateParam(w, r)
	if !ok {
		return
	}
	key, ok := idemKey(w, r, true)
	if !ok {
		return
	}
	m, ok := bodyMap(w, r, false)
	if !ok || !pathParam(w, m, "date", date) {
		return
	}
	a.run(w, r, dsync.OpDayWater, m, key, http.StatusOK, false)
}

// ---- food logs ----

func (a *api) listFoodLogs(w http.ResponseWriter, r *http.Request) {
	from, to, ok := dateRange(w, r, true)
	if !ok {
		return
	}
	limit, ok := intQuery(w, r, "limit", defaultFoodLogs, 1, maxListLimit)
	if !ok {
		return
	}
	logs, next, err := a.eng.ListFoodLogs(r.Context(), userID(r), dsync.FoodLogQuery{
		From: from, To: to, Limit: limit, Cursor: r.URL.Query().Get("cursor"), IncludeDeleted: r.URL.Query().Get("include_deleted") == "true",
	})
	if err != nil {
		listErr(w, r, err)
		return
	}
	WriteList(w, logs, next)
}

func (a *api) createFoodLog(w http.ResponseWriter, r *http.Request) {
	key, ok := idemKey(w, r, false)
	if !ok {
		return
	}
	m, ok := bodyMap(w, r, false)
	if !ok {
		return
	}
	if _, has := m["id"]; !has {
		// Without a client id the request is not idempotent, as documented.
		id, err := store.NewID()
		if err != nil {
			fail500(w, r, "new id", err)
			return
		}
		m["id"] = jsonStr(id)
	}
	a.run(w, r, dsync.OpFoodLogCreate, m, key, http.StatusCreated, false)
}

func (a *api) patchFoodLog(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	key, ok := idemKey(w, r, false)
	if !ok {
		return
	}
	m, ok := bodyMap(w, r, false)
	if !ok {
		return
	}
	if m, ok = split(w, m, "date", "meal", "name", "serving_label", "serving_grams", "quantity", "kcal", "protein_g", "carbs_g", "fat_g"); !ok || !pathParam(w, m, "id", id) {
		return
	}
	a.run(w, r, dsync.OpFoodLogUpdate, m, key, http.StatusOK, true)
}

func (a *api) deleteFoodLog(w http.ResponseWriter, r *http.Request) {
	a.idOp(w, r, dsync.OpFoodLogDelete)
}

func (a *api) restoreFoodLog(w http.ResponseWriter, r *http.Request) {
	a.idOp(w, r, dsync.OpFoodLogRestore)
}

func (a *api) idOp(w http.ResponseWriter, r *http.Request, typ string) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	key, ok := idemKey(w, r, false)
	if !ok {
		return
	}
	m, ok := bodyMap(w, r, true)
	if !ok || len(m) > 0 {
		if ok {
			badRequest(w, "Body harus kosong.")
		}
		return
	}
	m["id"] = jsonStr(id)
	a.run(w, r, typ, m, key, http.StatusOK, true)
}

// ---- custom foods ----

func (a *api) listCustomFoods(w http.ResponseWriter, r *http.Request) {
	limit, ok := intQuery(w, r, "limit", defaultFoodLogs, 1, maxListLimit)
	if !ok {
		return
	}
	foods, next, err := a.eng.ListCustomFoods(r.Context(), userID(r), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		listErr(w, r, err)
		return
	}
	WriteList(w, foods, next)
}

func (a *api) createCustomFood(w http.ResponseWriter, r *http.Request) {
	key, ok := idemKey(w, r, false)
	if !ok {
		return
	}
	m, ok := bodyMap(w, r, false)
	if !ok {
		return
	}
	if _, has := m["id"]; !has {
		id, err := store.NewID()
		if err != nil {
			fail500(w, r, "new id", err)
			return
		}
		m["id"] = jsonStr(id)
	}
	a.run(w, r, dsync.OpCustomFoodCreate, m, key, http.StatusCreated, false)
}

// ---- weights ----

func (a *api) listWeights(w http.ResponseWriter, r *http.Request) {
	from, ok := queryDate(w, r, "from", false)
	if !ok {
		return
	}
	to, ok := queryDate(w, r, "to", false)
	if !ok {
		return
	}
	limit, ok := intQuery(w, r, "limit", defaultWeights, 1, maxWeightsLimit)
	if !ok {
		return
	}
	ws, next, err := a.eng.ListWeights(r.Context(), userID(r), from, to, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		listErr(w, r, err)
		return
	}
	WriteList(w, ws, next)
}

func (a *api) putWeight(w http.ResponseWriter, r *http.Request) {
	date, ok := dateParam(w, r)
	if !ok {
		return
	}
	key, ok := idemKey(w, r, false)
	if !ok {
		return
	}
	m, ok := bodyMap(w, r, false)
	if !ok || !pathParam(w, m, "date", date) {
		return
	}
	a.run(w, r, dsync.OpWeightPut, m, key, http.StatusOK, false)
}

// deleteWeight is idempotent: a missing entry is still 204.
func (a *api) deleteWeight(w http.ResponseWriter, r *http.Request) {
	date, ok := dateParam(w, r)
	if !ok {
		return
	}
	key, ok := idemKey(w, r, false)
	if !ok {
		return
	}
	m, ok := bodyMap(w, r, true)
	if !ok || !pathParam(w, m, "date", date) {
		return
	}
	raw, _ := json.Marshal(m)
	res, err := a.eng.Apply(r.Context(), userID(r), dsync.Op{ID: key, Type: dsync.OpWeightDelete, Payload: raw}, nil)
	if err != nil {
		fail500(w, r, "delete weight", err)
		return
	}
	if res.Status == dsync.StatusApplied {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// A conflict here always has an existing entry (a missing one is "applied"), so it is a real 409.
	a.respond(w, res, http.StatusNoContent, false)
}

// ---- sync ----

type pushRequest struct {
	DataEpoch *int64     `json:"data_epoch"`
	Ops       []dsync.Op `json:"ops"`
}

func (a *api) syncPush(w http.ResponseWriter, r *http.Request) {
	uid := userID(r)
	if ok, retry := a.kit.limits.push.Allow(uid); !ok {
		tooManyRequests(w, retry)
		return
	}
	var req pushRequest
	if !decodeJSON(w, r, &req, false) {
		return
	}
	if req.DataEpoch == nil {
		badRequest(w, "data_epoch wajib diisi.")
		return
	}
	if len(req.Ops) == 0 || len(req.Ops) > maxPushOps {
		badRequest(w, fmt.Sprintf("Jumlah operasi harus 1 sampai %d.", maxPushOps))
		return
	}
	for _, op := range req.Ops {
		if op.ID == "" || op.Type == "" {
			badRequest(w, "Setiap operasi wajib punya op_id dan type.")
			return
		}
	}

	results := make([]dsync.Result, 0, len(req.Ops))
	for _, op := range req.Ops {
		res, err := a.eng.Apply(r.Context(), uid, op, req.DataEpoch)
		var em *dsync.EpochMismatchError
		if errors.As(err, &em) {
			WriteError(w, http.StatusConflict, ErrorBody{Code: "epoch_mismatch", Message: "Data akun telah dihapus dari perangkat lain.",
				Details: map[string]string{"data_epoch": strconv.FormatInt(em.Current, 10)}})
			return
		}
		if err != nil {
			fail500(w, r, "sync push", err)
			return
		}
		results = append(results, res)
	}
	WriteData(w, http.StatusOK, map[string]any{"data_epoch": *req.DataEpoch, "results": results})
}

func (a *api) syncPull(w http.ResponseWriter, r *http.Request) {
	since := int64(0)
	if v := r.URL.Query().Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			badRequest(w, "Parameter \"since\" harus bilangan bulat tidak negatif.")
			return
		}
		since = n
	}
	limit, ok := intQuery(w, r, "limit", defaultPull, 1, maxPullLimit)
	if !ok {
		return
	}
	res, err := a.eng.Pull(r.Context(), userID(r), since, limit)
	if err != nil {
		fail500(w, r, "sync pull", err)
		return
	}
	WriteData(w, http.StatusOK, res)
}

// ---- data ----

func (a *api) exportCSV(w http.ResponseWriter, r *http.Request) {
	dataset := r.URL.Query().Get("dataset")
	if dataset == "" {
		dataset = "food_logs"
	}
	if !export.Valid(dataset) {
		badRequest(w, "Dataset tidak dikenal.")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="dietsis-%s-%s.csv"`, dataset, time.Now().Format("2006-01-02")))
	if err := export.Write(r.Context(), w, a.readDB, userID(r), dataset); err != nil {
		// Headers are already sent; all we can do is log and cut the stream.
		slog.Error("export failed", "request_id", RequestID(r.Context()), "dataset", dataset, "err", err)
	}
}

func (a *api) deleteAccountData(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Confirm string `json:"confirm"`
	}
	if !decodeJSON(w, r, &body, false) {
		return
	}
	if body.Confirm != "HAPUS" {
		WriteError(w, http.StatusUnprocessableEntity, ErrorBody{Code: "validation_failed", Message: "Data tidak valid.",
			Details: map[string]string{"confirm": "Ketik HAPUS untuk mengonfirmasi"}})
		return
	}
	epoch, err := a.eng.DeleteAllData(r.Context(), userID(r))
	if err != nil {
		fail500(w, r, "delete account data", err)
		return
	}
	slog.Info("user data deleted", "request_id", RequestID(r.Context()), "user_id", userID(r))
	WriteData(w, http.StatusOK, map[string]any{"data_epoch": epoch})
}

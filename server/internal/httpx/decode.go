package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

// decodeJSON reads exactly one JSON object into dst with unknown fields rejected. On failure it
// writes the error response and returns false. `optional` allows an empty body (dst stays zero).
//
//	413 payload_too_large  body over the route's limit
//	415 unsupported_media_type  body present but not application/json
//	400 bad_request  malformed JSON, unknown field, trailing data, or missing required body
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, optional bool) bool {
	if r.ContentLength != 0 && !isJSON(r) {
		WriteError(w, http.StatusUnsupportedMediaType, ErrorBody{Code: "unsupported_media_type", Message: "Gunakan Content-Type: application/json."})
		return false
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if errors.Is(err, io.EOF) && optional {
		return true
	}
	if err == nil {
		// Reject trailing garbage / a second JSON value.
		if _, extra := dec.Token(); !errors.Is(extra, io.EOF) {
			err = errors.New("trailing data")
		}
	}
	if err == nil {
		return true
	}
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		WriteError(w, http.StatusRequestEntityTooLarge, ErrorBody{Code: "payload_too_large", Message: "Data terlalu besar."})
		return false
	}
	WriteError(w, http.StatusBadRequest, ErrorBody{Code: "bad_request", Message: "Isi permintaan tidak valid."})
	return false
}

func isJSON(r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mt == "application/json"
}

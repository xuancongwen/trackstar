package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"trackstar/internal/apperr"
)

const maxBodyBytes = 1 << 20

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// fail maps a service error to a response. Unknown errors are logged and
// reported without detail.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	status := map[apperr.Kind]int{
		apperr.KindInvalid:      http.StatusUnprocessableEntity,
		apperr.KindUnauthorized: http.StatusUnauthorized,
		apperr.KindForbidden:    http.StatusForbidden,
		apperr.KindNotFound:     http.StatusNotFound,
		apperr.KindConflict:     http.StatusConflict,
		apperr.KindRateLimited:  http.StatusTooManyRequests,
	}[apperr.KindOf(err)]
	if status == 0 {
		s.Logger.Error("internal error", "error", err, "request_id", requestID(r.Context()), "path", r.URL.Path)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeError(w, status, err.Error())
}

// decode reads a JSON body strictly: unknown fields and trailing data are errors.
func decode(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return apperr.Invalid("invalid JSON body: %v", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return apperr.Invalid("invalid JSON body: unexpected trailing data")
	}
	return nil
}

func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.NotFound("resource")
	}
	return id, nil
}

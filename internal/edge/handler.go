package edge

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const maxTapRequestBytes = 8 << 10

func NewHandler(service *Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "method not allowed",
			})
			return
		}

		var req TapRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTapRequestBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": decodeErrorMessage(err),
			})
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "request body must contain a single JSON object",
			})
			return
		}

		result, err := service.AcceptTap(r.Context(), r.Header.Get("X-Ashton-Edge-Token"), req)
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, ErrMissingToken):
				status = http.StatusUnauthorized
			case errors.Is(err, ErrForbiddenToken):
				status = http.StatusForbidden
			case errors.Is(err, ErrPublishUnavailable):
				status = http.StatusServiceUnavailable
			case IsValidationError(err):
				status = http.StatusBadRequest
			}

			writeJSON(w, status, map[string]string{
				"error": err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusAccepted, result)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func decodeErrorMessage(err error) string {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return "request body is too large"
	}
	if errors.Is(err, io.EOF) {
		return "request body is required"
	}
	return err.Error()
}

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/ixxet/athena/internal/domain"
	"github.com/ixxet/athena/internal/edgehistory"
	"github.com/ixxet/athena/internal/facility"
	"github.com/ixxet/athena/internal/metrics"
	"github.com/ixxet/athena/internal/presence"
)

type countResponse struct {
	FacilityID   string `json:"facility_id"`
	ZoneID       string `json:"zone_id,omitempty"`
	CurrentCount int    `json:"current_count"`
	ObservedAt   string `json:"observed_at"`
}

type healthResponse struct {
	Service string `json:"service"`
	Status  string `json:"status"`
	Adapter string `json:"adapter"`
}

type historyObservationResponse struct {
	Direction  string `json:"direction"`
	Result     string `json:"result"`
	ObservedAt string `json:"observed_at"`
	Committed  bool   `json:"committed"`
}

type historyResponse struct {
	FacilityID   string                       `json:"facility_id"`
	Since        string                       `json:"since"`
	Until        string                       `json:"until"`
	Observations []historyObservationResponse `json:"observations"`
}

type facilityListResponse struct {
	Facilities []facility.Summary `json:"facilities"`
}

type Option func(*handlerOptions)

type handlerOptions struct {
	edgeTapHandler http.Handler
	historyPath    string
	facilityStore  *facility.Store
}

func WithEdgeTapHandler(handler http.Handler) Option {
	return func(options *handlerOptions) {
		options.edgeTapHandler = handler
	}
}

func WithHistoryPath(path string) Option {
	return func(options *handlerOptions) {
		options.historyPath = strings.TrimSpace(path)
	}
}

func WithFacilityStore(store *facility.Store) Option {
	return func(options *handlerOptions) {
		options.facilityStore = store
	}
}

func NewHandler(readPath *presence.ReadPath, collector *metrics.Metrics, adapterName string, opts ...Option) http.Handler {
	options := handlerOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	router := chi.NewRouter()

	router.Get("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{
			Service: "athena",
			Status:  "ok",
			Adapter: adapterName,
		})
	})

	router.Get("/api/v1/presence/count", func(w http.ResponseWriter, r *http.Request) {
		filter := domain.OccupancyFilter{
			FacilityID: r.URL.Query().Get("facility"),
			ZoneID:     r.URL.Query().Get("zone"),
		}

		snapshot, err := readPath.CurrentOccupancy(r.Context(), filter)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, countResponse{
			FacilityID:   snapshot.FacilityID,
			ZoneID:       snapshot.ZoneID,
			CurrentCount: snapshot.CurrentCount,
			ObservedAt:   snapshot.ObservedAt.Format(time.RFC3339),
		})
	})

	router.Get("/api/v1/presence/history", func(w http.ResponseWriter, r *http.Request) {
		if options.historyPath == "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "edge observation history is not configured",
			})
			return
		}

		facilityID := strings.TrimSpace(r.URL.Query().Get("facility"))
		if facilityID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "facility query parameter is required",
			})
			return
		}

		since, err := parseHistoryBoundary(r.URL.Query().Get("since"), "since")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": err.Error(),
			})
			return
		}
		until, err := parseHistoryBoundary(r.URL.Query().Get("until"), "until")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": err.Error(),
			})
			return
		}
		if until.Before(since) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "until query parameter must be greater than or equal to since",
			})
			return
		}

		observations, err := edgehistory.ReadPublicObservations(options.historyPath, edgehistory.PublicFilter{
			FacilityID: facilityID,
			Since:      since,
			Until:      until,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": err.Error(),
			})
			return
		}

		payload := make([]historyObservationResponse, 0, len(observations))
		for _, observation := range observations {
			payload = append(payload, historyObservationResponse{
				Direction:  string(observation.Direction),
				Result:     observation.Result,
				ObservedAt: observation.ObservedAt.UTC().Format(time.RFC3339),
				Committed:  observation.Committed,
			})
		}

		writeJSON(w, http.StatusOK, historyResponse{
			FacilityID:   facilityID,
			Since:        since.UTC().Format(time.RFC3339),
			Until:        until.UTC().Format(time.RFC3339),
			Observations: payload,
		})
	})

	router.Get("/api/v1/facilities", func(w http.ResponseWriter, r *http.Request) {
		if options.facilityStore == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "facility catalog is not configured",
			})
			return
		}

		writeJSON(w, http.StatusOK, facilityListResponse{
			Facilities: options.facilityStore.List(),
		})
	})

	router.Get("/api/v1/facilities/{facilityID}", func(w http.ResponseWriter, r *http.Request) {
		if options.facilityStore == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "facility catalog is not configured",
			})
			return
		}

		facilityID := strings.TrimSpace(chi.URLParam(r, "facilityID"))
		if facilityID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "facility path parameter is required",
			})
			return
		}

		record, ok := options.facilityStore.Facility(facilityID)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "facility not found",
			})
			return
		}

		writeJSON(w, http.StatusOK, record)
	})

	router.Handle("/metrics", promhttp.HandlerFor(collector.Registry(), promhttp.HandlerOpts{}))
	if options.edgeTapHandler != nil {
		router.Handle("/api/v1/edge/tap", options.edgeTapHandler)
	}

	return router
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func parseHistoryBoundary(value, field string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, &historyQueryError{message: field + " query parameter is required"}
	}

	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, &historyQueryError{message: field + " query parameter must be RFC3339"}
	}

	return parsed.UTC(), nil
}

type historyQueryError struct {
	message string
}

func (e *historyQueryError) Error() string {
	return e.message
}

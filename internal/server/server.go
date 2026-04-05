package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/ixxet/athena/internal/domain"
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

type Option func(*handlerOptions)

type handlerOptions struct {
	edgeTapHandler http.Handler
}

func WithEdgeTapHandler(handler http.Handler) Option {
	return func(options *handlerOptions) {
		options.edgeTapHandler = handler
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

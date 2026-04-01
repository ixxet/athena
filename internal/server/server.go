package server

import (
	"encoding/json"
	"net/http"

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

func NewHandler(service *presence.Service, collector *metrics.Metrics) http.Handler {
	router := chi.NewRouter()

	router.Get("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"service": "athena",
			"status":  "ok",
		})
	})

	router.Get("/api/v1/presence/count", func(w http.ResponseWriter, r *http.Request) {
		filter := domain.OccupancyFilter{
			FacilityID: r.URL.Query().Get("facility"),
			ZoneID:     r.URL.Query().Get("zone"),
		}

		snapshot, err := service.CurrentOccupancy(r.Context(), filter)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": err.Error(),
			})
			return
		}

		collector.SetCurrentOccupancy(snapshot.CurrentCount)

		writeJSON(w, http.StatusOK, countResponse{
			FacilityID:   snapshot.FacilityID,
			ZoneID:       snapshot.ZoneID,
			CurrentCount: snapshot.CurrentCount,
			ObservedAt:   snapshot.ObservedAt.Format(http.TimeFormat),
		})
	})

	router.Handle("/metrics", promhttp.HandlerFor(collector.Registry(), promhttp.HandlerOpts{}))

	return router
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

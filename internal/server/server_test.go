package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ixxet/athena/internal/domain"
	"github.com/ixxet/athena/internal/metrics"
	"github.com/ixxet/athena/internal/presence"
)

type stubAdapter struct {
	events []domain.PresenceEvent
}

func (s stubAdapter) Name() string {
	return "stub"
}

func (s stubAdapter) ListEvents(context.Context) ([]domain.PresenceEvent, error) {
	return s.events, nil
}

func TestPresenceCountEndpoint(t *testing.T) {
	service := presence.NewService(stubAdapter{
		events: []domain.PresenceEvent{
			{
				ID:         "1",
				FacilityID: "ashtonbee",
				Direction:  domain.DirectionIn,
				RecordedAt: time.Now().UTC().Add(-2 * time.Minute),
			},
			{
				ID:         "2",
				FacilityID: "ashtonbee",
				Direction:  domain.DirectionOut,
				RecordedAt: time.Now().UTC().Add(-time.Minute),
			},
			{
				ID:         "3",
				FacilityID: "ashtonbee",
				Direction:  domain.DirectionIn,
				RecordedAt: time.Now().UTC(),
			},
		},
	})

	collector := metrics.New()
	handler := NewHandler(service, collector)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/presence/count?facility=ashtonbee", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "\"current_count\":1") {
		t.Fatalf("body = %q, want current_count 1", body)
	}
}

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ixxet/athena/internal/adapter"
	"github.com/ixxet/athena/internal/domain"
	"github.com/ixxet/athena/internal/metrics"
	"github.com/ixxet/athena/internal/presence"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()

	baseTime := time.Date(2026, 4, 1, 8, 30, 0, 0, time.UTC)
	service := presence.NewService(
		adapter.NewMockAdapter(adapter.MockConfig{
			FacilityID: "ashtonbee",
			Entries:    3,
			Exits:      1,
			BaseTime:   baseTime,
		}),
		presence.WithClock(func() time.Time { return baseTime }),
	)
	readPath := presence.NewReadPath(service, domain.OccupancyFilter{
		FacilityID: "ashtonbee",
	})

	return NewHandler(readPath, metrics.New(readPath), "mock")
}

func TestHealthEndpoint(t *testing.T) {
	handler := testHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "\"service\":\"athena\"") {
		t.Fatalf("body = %q, want service field", body)
	}
	if !strings.Contains(body, "\"adapter\":\"mock\"") {
		t.Fatalf("body = %q, want adapter field", body)
	}
}

func TestPresenceCountEndpointUsesDefaultFacility(t *testing.T) {
	handler := testHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/presence/count", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "\"facility_id\":\"ashtonbee\"") {
		t.Fatalf("body = %q, want facility_id ashtonbee", body)
	}
	if !strings.Contains(body, "\"current_count\":2") {
		t.Fatalf("body = %q, want current_count 2", body)
	}
}

func TestPresenceCountEndpointReturnsZeroForUnknownFacility(t *testing.T) {
	handler := testHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/presence/count?facility=missing", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "\"facility_id\":\"missing\"") {
		t.Fatalf("body = %q, want facility_id missing", body)
	}
	if !strings.Contains(body, "\"current_count\":0") {
		t.Fatalf("body = %q, want current_count 0", body)
	}
}

func TestMetricsEndpointScrapesCanonicalReadPath(t *testing.T) {
	handler := testHandler(t)

	countRequest := httptest.NewRequest(http.MethodGet, "/api/v1/presence/count?facility=missing", nil)
	countRecorder := httptest.NewRecorder()
	handler.ServeHTTP(countRecorder, countRequest)

	metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(metricsRecorder, metricsRequest)

	if metricsRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", metricsRecorder.Code, http.StatusOK)
	}

	body := metricsRecorder.Body.String()
	if !strings.Contains(body, "athena_current_occupancy 2") {
		t.Fatalf("metrics body = %q, want athena_current_occupancy 2", body)
	}
}

func TestEdgeTapRouteMountsWhenConfigured(t *testing.T) {
	handler := NewHandler(
		presence.NewReadPath(
			presence.NewService(adapter.NewMockAdapter(adapter.MockConfig{
				FacilityID: "ashtonbee",
				Entries:    1,
				BaseTime:   time.Date(2026, 4, 1, 8, 30, 0, 0, time.UTC),
			})),
			domain.OccupancyFilter{FacilityID: "ashtonbee"},
		),
		metrics.New(
			presence.NewReadPath(
				presence.NewService(adapter.NewMockAdapter(adapter.MockConfig{
					FacilityID: "ashtonbee",
					Entries:    1,
					BaseTime:   time.Date(2026, 4, 1, 8, 30, 0, 0, time.UTC),
				})),
				domain.OccupancyFilter{FacilityID: "ashtonbee"},
			),
		),
		"mock",
		WithEdgeTapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		})),
	)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/edge/tap", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
}

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ixxet/athena/internal/config"
	"github.com/ixxet/athena/internal/metrics"
	"github.com/ixxet/athena/internal/server"
)

func TestBuildAppWithCSVAdapterFeedsOccupancyReadPath(t *testing.T) {
	path := writeCSVFixture(t, strings.Join([]string{
		"event_id,facility_id,zone_id,external_identity_hash,direction,recorded_at",
		"csv-in-001,ashtonbee,lobby,tag-001,in,2026-04-01T08:00:00Z",
		"csv-in-002,ashtonbee,lobby,,in,2026-04-01T08:05:00Z",
		"csv-out-001,ashtonbee,lobby,,out,2026-04-01T08:10:00Z",
		"csv-other-001,other,,tag-002,in,2026-04-01T08:15:00Z",
	}, "\n"))

	application, err := buildApp(config.Config{
		Adapter:           "csv",
		CSVPath:           path,
		DefaultFacilityID: "ashtonbee",
	})
	if err != nil {
		t.Fatalf("buildApp() error = %v", err)
	}

	handler := server.NewHandler(application.readPath, metrics.New(application.readPath), application.adapterName)

	healthRequest := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	healthRecorder := httptest.NewRecorder()
	handler.ServeHTTP(healthRecorder, healthRequest)
	if healthRecorder.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", healthRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(healthRecorder.Body.String(), "\"adapter\":\"csv\"") {
		t.Fatalf("health body = %q, want adapter csv", healthRecorder.Body.String())
	}

	countRequest := httptest.NewRequest(http.MethodGet, "/api/v1/presence/count", nil)
	countRecorder := httptest.NewRecorder()
	handler.ServeHTTP(countRecorder, countRequest)
	if countRecorder.Code != http.StatusOK {
		t.Fatalf("count status = %d, want %d", countRecorder.Code, http.StatusOK)
	}
	body := countRecorder.Body.String()
	if !strings.Contains(body, "\"facility_id\":\"ashtonbee\"") {
		t.Fatalf("count body = %q, want facility ashtonbee", body)
	}
	if !strings.Contains(body, "\"current_count\":1") {
		t.Fatalf("count body = %q, want current_count 1", body)
	}
	if !strings.Contains(body, "\"observed_at\":\"2026-04-01T08:10:00Z\"") {
		t.Fatalf("count body = %q, want observed_at from csv source", body)
	}
}

func TestBuildAppWithCSVAdapterRejectsBrokenSource(t *testing.T) {
	_, err := buildApp(config.Config{
		Adapter:           "csv",
		CSVPath:           filepath.Join(t.TempDir(), "missing.csv"),
		DefaultFacilityID: "ashtonbee",
	})
	if err == nil {
		t.Fatal("buildApp() error = nil, want missing csv source error")
	}
	if !strings.Contains(err.Error(), "read csv source") {
		t.Fatalf("buildApp() error = %q, want read csv source context", err)
	}
}

func writeCSVFixture(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "presence.csv")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	return path
}

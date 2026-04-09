package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ixxet/athena/internal/config"
	"github.com/ixxet/athena/internal/domain"
	"github.com/ixxet/athena/internal/edge"
	"github.com/ixxet/athena/internal/edgehistory"
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

func TestEdgeHistoryCommandPrintsRecentDurableObservations(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "edge-history.jsonl")
	store, err := edgehistory.NewFileStore(historyPath)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	record := edge.ObservationRecord{
		EventID:              "edge-001",
		FacilityID:           "ashtonbee",
		ZoneID:               "gym-floor",
		NodeID:               "entry-node",
		Direction:            domain.DirectionIn,
		Result:               "pass",
		Source:               domain.SourceRFID,
		ExternalIdentityHash: "hashed-account",
		ObservedAt:           time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC),
		StoredAt:             time.Date(2026, 4, 4, 12, 0, 1, 0, time.UTC),
		AccountType:          "Standard",
		NamePresent:          true,
	}
	if err := store.RecordObservation(context.Background(), record); err != nil {
		t.Fatalf("RecordObservation() error = %v", err)
	}

	cmd := newRootCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{
		"edge",
		"history",
		"--history-path", historyPath,
		"--limit", "1",
		"--format", "json",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "\"external_identity_hash\": \"hashed-account\"") {
		t.Fatalf("output = %q, want external_identity_hash", output)
	}
	if strings.Contains(output, "1000123456") {
		t.Fatalf("output leaked raw account number: %s", output)
	}
}

func TestFacilityListCommandPrintsCatalogSummaries(t *testing.T) {
	catalogPath := writeFacilityCatalogFixture(t)

	cmd := newRootCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{
		"facility",
		"list",
		"--catalog-path", catalogPath,
		"--format", "json",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "\"facility_id\": \"ashtonbee\"") {
		t.Fatalf("output = %q, want ashtonbee facility", output)
	}
	if !strings.Contains(output, "\"facility_id\": \"morningside\"") {
		t.Fatalf("output = %q, want morningside facility", output)
	}
}

func TestFacilityShowCommandPrintsFacilityDetail(t *testing.T) {
	catalogPath := writeFacilityCatalogFixture(t)

	cmd := newRootCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{
		"facility",
		"show",
		"--catalog-path", catalogPath,
		"--facility", "ashtonbee",
		"--format", "json",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "\"zone_id\": \"gym-floor\"") {
		t.Fatalf("output = %q, want gym-floor zone", output)
	}
	if !strings.Contains(output, "\"metadata\": {") {
		t.Fatalf("output = %q, want metadata payload", output)
	}
	if !strings.Contains(output, "\"starts_at\": \"2026-07-01T12:00:00Z\"") {
		t.Fatalf("output = %q, want UTC-normalized closure start", output)
	}
}

func TestFacilityCommandsRequireCatalogPath(t *testing.T) {
	cmd := newRootCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{
		"facility",
		"list",
		"--format", "json",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want missing catalog path error")
	}
	if !strings.Contains(err.Error(), "ATHENA_FACILITY_CATALOG_PATH") {
		t.Fatalf("Execute() error = %q, want ATHENA_FACILITY_CATALOG_PATH context", err)
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

func writeFacilityCatalogFixture(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "facilities.json")
	contents := `{
  "facilities": [
    {
      "facility_id": "morningside",
      "name": "Morningside",
      "timezone": "America/Toronto",
      "hours": [
        {"day": "tuesday", "opens_at": "06:00", "closes_at": "22:00"},
        {"day": "monday", "opens_at": "06:00", "closes_at": "22:00"}
      ],
      "zones": [
        {"zone_id": "weight-room", "name": "Weight Room"}
      ],
      "metadata": {
        "ingress_mode": "touchnet",
        "surface": "internal-only"
      }
    },
    {
      "facility_id": "ashtonbee",
      "name": "Ashtonbee",
      "timezone": "America/Toronto",
      "hours": [
        {"day": "monday", "opens_at": "06:00", "closes_at": "22:00"}
      ],
      "zones": [
        {"zone_id": "gym-floor", "name": "Gym Floor"},
        {"zone_id": "lobby", "name": "Lobby"}
      ],
      "closure_windows": [
        {
          "starts_at": "2026-07-01T08:00:00-04:00",
          "ends_at": "2026-07-01T12:00:00-04:00",
          "code": "maintenance",
          "reason": "Morning maintenance",
          "zone_ids": ["gym-floor"]
        }
      ],
      "metadata": {
        "ingress_mode": "touchnet",
        "surface": "internal-only"
      }
    }
  ]
}`

	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	return path
}

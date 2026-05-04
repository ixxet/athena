package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
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
	"github.com/ixxet/athena/internal/publish"
	"github.com/ixxet/athena/internal/server"
)

type serveStubPublisher struct{}

func (serveStubPublisher) Publish(context.Context, string, []byte) error {
	return nil
}

type stubIngressBridgeStore struct {
	report edgehistory.IngressBridgeReport
	filter edgehistory.IngressBridgeFilter
	closed int
}

func (s *stubIngressBridgeStore) ReadIngressBridge(_ context.Context, filter edgehistory.IngressBridgeFilter) (edgehistory.IngressBridgeReport, error) {
	s.filter = filter
	return s.report, nil
}

func (s *stubIngressBridgeStore) Close() {
	s.closed++
}

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

func TestEdgeIngressBridgeCommandPrintsRedactedJSON(t *testing.T) {
	base := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	stub := &stubIngressBridgeStore{
		report: edgehistory.IngressBridgeReport{
			FacilityID: "ashtonbee",
			ZoneID:     "gym-floor",
			Since:      base.Add(-time.Hour),
			Until:      base.Add(time.Hour),
			Contract: edgehistory.IngressBridgeContract{
				Scope:          "repo_local_runtime_proof",
				IdentityOutput: "raw account ids, names, and external identity hashes are redacted",
			},
			Summary: edgehistory.IngressBridgeSummary{
				TotalEvidence:                   1,
				EligibleCoPresence:              1,
				EligibleDailyPresence:           1,
				EligibleReliabilityVerification: 0,
				ReasonCounts: []edgehistory.ReasonCount{
					{Code: edgehistory.ReasonAcceptedPresenceWithoutSourcePassSession, Count: 1},
				},
			},
			Evidence: []edgehistory.IngressBridgeEvidence{
				{
					EvidenceID:       "evidence-001",
					EventID:          "edge-policy-fail-001",
					IdentityPresent:  true,
					IdentityRef:      "identity-001",
					FacilityID:       "ashtonbee",
					ZoneID:           "gym-floor",
					NodeID:           "entry-node",
					Direction:        domain.DirectionIn,
					SourceResult:     "fail",
					ObservedAt:       testTimePtr(base),
					AcceptedPresence: true,
					AcceptancePath:   edge.AcceptancePathFacility,
					Eligibility: edgehistory.IngressBridgeEligibility{
						CoPresenceProof: edgehistory.EligibilitySignal{
							Eligible: true,
						},
						PrivateDailyPresence: edgehistory.EligibilitySignal{
							Eligible: true,
						},
						ReliabilityVerification: edgehistory.EligibilitySignal{
							ReasonCodes: []string{edgehistory.ReasonAcceptedPresenceWithoutSourcePassSession},
						},
					},
					ReasonCodes: []string{edgehistory.ReasonAcceptedPresenceWithoutSourcePassSession},
				},
			},
		},
	}

	previous := newIngressBridgeStore
	newIngressBridgeStore = func(_ context.Context, postgresDSN string) (ingressBridgeStore, error) {
		if postgresDSN != "postgres://fixture" {
			t.Fatalf("postgresDSN = %q, want fixture dsn", postgresDSN)
		}
		return stub, nil
	}
	t.Cleanup(func() {
		newIngressBridgeStore = previous
	})

	cmd := newRootCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{
		"edge",
		"ingress-bridge",
		"--postgres-dsn", "postgres://fixture",
		"--facility", "ashtonbee",
		"--zone", "gym-floor",
		"--since", base.Add(-time.Hour).Format(time.RFC3339),
		"--until", base.Add(time.Hour).Format(time.RFC3339),
		"--format", "json",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if stub.closed != 1 {
		t.Fatalf("Close() calls = %d, want 1", stub.closed)
	}
	if stub.filter.FacilityID != "ashtonbee" || stub.filter.ZoneID != "gym-floor" {
		t.Fatalf("filter = %#v, want scoped facility/zone", stub.filter)
	}

	output := stdout.String()
	for _, want := range []string{
		"\"scope\": \"repo_local_runtime_proof\"",
		"\"identity_ref\": \"identity-001\"",
		"\"accepted_presence\": true",
		edgehistory.ReasonAcceptedPresenceWithoutSourcePassSession,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want %q", output, want)
		}
	}
	for _, unsafe := range []string{"external_identity_hash", "1000123456", "Fixture Student"} {
		if strings.Contains(output, unsafe) {
			t.Fatalf("ingress bridge JSON leaked %q: %s", unsafe, output)
		}
	}
}

func TestEdgeIngressBridgeCommandPrintsReadableText(t *testing.T) {
	base := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	stub := &stubIngressBridgeStore{
		report: edgehistory.IngressBridgeReport{
			FacilityID: "ashtonbee",
			Since:      base.Add(-time.Hour),
			Until:      base.Add(time.Hour),
			Summary: edgehistory.IngressBridgeSummary{
				TotalEvidence:         1,
				NoEligibleSignals:     1,
				EligibleCoPresence:    0,
				EligibleDailyPresence: 0,
				ReasonCounts: []edgehistory.ReasonCount{
					{Code: edgehistory.ReasonSourceFailWithoutAcceptedPresence, Count: 1},
				},
			},
			Evidence: []edgehistory.IngressBridgeEvidence{
				{
					EvidenceID:       "evidence-001",
					EventID:          "edge-fail-001",
					IdentityPresent:  true,
					IdentityRef:      "identity-001",
					FacilityID:       "ashtonbee",
					NodeID:           "entry-node",
					Direction:        domain.DirectionIn,
					SourceResult:     "fail",
					ObservedAt:       testTimePtr(base),
					AcceptedPresence: false,
					ReasonCodes:      []string{edgehistory.ReasonSourceFailWithoutAcceptedPresence},
				},
			},
		},
	}

	previous := newIngressBridgeStore
	newIngressBridgeStore = func(context.Context, string) (ingressBridgeStore, error) {
		return stub, nil
	}
	t.Cleanup(func() {
		newIngressBridgeStore = previous
	})

	cmd := newRootCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{
		"edge",
		"ingress-bridge",
		"--postgres-dsn", "postgres://fixture",
		"--facility", "ashtonbee",
		"--since", base.Add(-time.Hour).Format(time.RFC3339),
		"--until", base.Add(time.Hour).Format(time.RFC3339),
		"--format", "text",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := stdout.String()
	for _, want := range []string{
		"facility=ashtonbee",
		"eligible_co_presence=0",
		"reason code=source_fail_without_accepted_presence count=1",
		"evidence evidence_id=evidence-001 event_id=edge-fail-001",
		"identity_ref=identity-001",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want %q", output, want)
		}
	}
	if strings.Contains(output, "external_identity_hash") {
		t.Fatalf("text output leaked external identity hash field: %s", output)
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

func TestIdentityLinkAddRejectsUnsafeKeyBeforeOpeningPostgres(t *testing.T) {
	cmd := newRootCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{
		"identity",
		"link",
		"add",
		"--facility", "ashtonbee",
		"--subject-id", "550e8400-e29b-41d4-a716-446655440000",
		"--kind", "member_account",
		"--key", "1000123456",
		"--source", "owner_confirmed",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want unsafe link key validation error")
	}
	if !strings.Contains(err.Error(), "member_account link_key must be a canonical lowercase UUID") {
		t.Fatalf("Execute() error = %q, want member_account UUID validation", err)
	}
	if strings.Contains(err.Error(), "Postgres") || strings.Contains(err.Error(), "dsn") {
		t.Fatalf("Execute() error = %q, validation should happen before opening Postgres", err)
	}
}

func TestServeCommandShutsDownCleanlyWhenContextIsCanceled(t *testing.T) {
	addr := reserveListenAddress(t)
	t.Setenv("ATHENA_HTTP_ADDR", addr)
	t.Setenv("ATHENA_ADAPTER", "mock")

	serveCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	command := newServeCmd()
	command.SetContext(serveCtx)

	done := make(chan error, 1)
	go func() {
		done <- command.Execute()
	}()

	waitForHealthyHTTP(t, fmt.Sprintf("http://%s/api/v1/health", addr))
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve command did not shut down within 10s after context cancellation")
	}
}

func TestServeCommandStartsProjectionModeWithConfiguredProjectorBounds(t *testing.T) {
	addr := reserveListenAddress(t)
	historyPath := filepath.Join(t.TempDir(), "edge-history.jsonl")
	t.Setenv("ATHENA_HTTP_ADDR", addr)
	t.Setenv("ATHENA_ADAPTER", "mock")
	t.Setenv("ATHENA_EDGE_OCCUPANCY_PROJECTION", "true")
	t.Setenv("ATHENA_EDGE_HASH_SALT", "salt")
	t.Setenv("ATHENA_EDGE_TOKENS", "entry=node-token")
	t.Setenv("ATHENA_NATS_URL", "nats://example:4222")
	t.Setenv("ATHENA_EDGE_OBSERVATION_HISTORY_PATH", historyPath)
	t.Setenv("ATHENA_EDGE_PROJECTOR_ABSENT_RETENTION", "2h")
	t.Setenv("ATHENA_EDGE_PROJECTOR_MAX_ABSENT_IDENTITIES", "2")

	originalPublisherHandle := newPublisherHandle
	newPublisherHandle = func(cfg config.Config) (publish.Publisher, func() error, error) {
		return serveStubPublisher{}, func() error { return nil }, nil
	}
	defer func() {
		newPublisherHandle = originalPublisherHandle
	}()

	serveCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	command := newServeCmd()
	command.SetContext(serveCtx)

	done := make(chan error, 1)
	go func() {
		done <- command.Execute()
	}()

	waitForHealthyHTTP(t, fmt.Sprintf("http://%s/api/v1/health", addr))

	response, err := http.Get(fmt.Sprintf("http://%s/api/v1/health", addr)) //nolint:gosec // local integration probe
	if err != nil {
		t.Fatalf("GET /api/v1/health error = %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if !strings.Contains(string(body), "\"adapter\":\"edge-projection\"") {
		t.Fatalf("health body = %q, want adapter edge-projection", string(body))
	}

	countResponse, err := http.Get(fmt.Sprintf("http://%s/api/v1/presence/count", addr)) //nolint:gosec // local integration probe
	if err != nil {
		t.Fatalf("GET /api/v1/presence/count error = %v", err)
	}
	defer countResponse.Body.Close()

	countBody, err := io.ReadAll(countResponse.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if countResponse.StatusCode != http.StatusOK {
		t.Fatalf("count status = %d, want %d", countResponse.StatusCode, http.StatusOK)
	}
	if !strings.Contains(string(countBody), "\"current_count\":0") {
		t.Fatalf("count body = %q, want current_count 0", string(countBody))
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve command did not shut down within 10s after projection-mode context cancellation")
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

func reserveListenAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()

	return listener.Addr().String()
}

func waitForHealthyHTTP(t *testing.T, url string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(url) //nolint:gosec // local integration probe
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("health endpoint %s did not become ready", url)
}

func testTimePtr(value time.Time) *time.Time {
	copy := value.UTC()
	return &copy
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

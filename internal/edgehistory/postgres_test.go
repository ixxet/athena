package edgehistory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/ory/dockertest/v3"
	docker "github.com/ory/dockertest/v3/docker"

	"github.com/ixxet/athena/internal/domain"
	"github.com/ixxet/athena/internal/edge"
	"github.com/ixxet/athena/internal/presence"
)

func TestPostgresStoreReplayAndAnalytics(t *testing.T) {
	store := newPostgresStore(t)
	projector := presence.NewProjector()

	service, err := edge.NewService(
		&stubPublisher{},
		"salt",
		map[string]string{"entry-node": "entry-token"},
		edge.WithProjection(projector),
		edge.WithObservationRecorder(store),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	testCases := []struct {
		request       edge.TapRequest
		wantStatus    string
		wantPublished bool
	}{
		{
			request: edge.TapRequest{
				EventID:    "edge-in-accepted-001",
				AccountRaw: "1000123456",
				Direction:  "in",
				FacilityID: "ashtonbee",
				ZoneID:     "gym-floor",
				NodeID:     "entry-node",
				ObservedAt: "2026-04-04T12:00:00Z",
				Result:     "pass",
			},
			wantStatus:    "accepted",
			wantPublished: true,
		},
		{
			request: edge.TapRequest{
				EventID:    "edge-in-repeat-001",
				AccountRaw: "1000123456",
				Direction:  "in",
				FacilityID: "ashtonbee",
				ZoneID:     "gym-floor",
				NodeID:     "entry-node",
				ObservedAt: "2026-04-04T12:01:00Z",
				Result:     "pass",
			},
			wantStatus:    "observed",
			wantPublished: false,
		},
		{
			request: edge.TapRequest{
				EventID:    "edge-fail-001",
				AccountRaw: "1000123456",
				Direction:  "in",
				FacilityID: "ashtonbee",
				ZoneID:     "gym-floor",
				NodeID:     "entry-node",
				ObservedAt: "2026-04-04T12:02:00Z",
				Result:     "fail",
			},
			wantStatus:    "observed",
			wantPublished: false,
		},
		{
			request: edge.TapRequest{
				EventID:    "edge-out-accepted-001",
				AccountRaw: "1000123456",
				Direction:  "out",
				FacilityID: "ashtonbee",
				ZoneID:     "gym-floor",
				NodeID:     "entry-node",
				ObservedAt: "2026-04-04T12:05:00Z",
				Result:     "pass",
			},
			wantStatus:    "accepted",
			wantPublished: true,
		},
		{
			request: edge.TapRequest{
				EventID:    "edge-out-repeat-001",
				AccountRaw: "1000123456",
				Direction:  "out",
				FacilityID: "ashtonbee",
				ZoneID:     "gym-floor",
				NodeID:     "entry-node",
				ObservedAt: "2026-04-04T12:06:00Z",
				Result:     "pass",
			},
			wantStatus:    "observed",
			wantPublished: false,
		},
	}

	for _, tc := range testCases {
		result, err := service.AcceptTap(context.Background(), "entry-token", tc.request)
		if err != nil {
			t.Fatalf("AcceptTap(%s) error = %v", tc.request.EventID, err)
		}
		if result.Status != tc.wantStatus {
			t.Fatalf("AcceptTap(%s) status = %q, want %q", tc.request.EventID, result.Status, tc.wantStatus)
		}
		if result.Published != tc.wantPublished {
			t.Fatalf("AcceptTap(%s) published = %t, want %t", tc.request.EventID, result.Published, tc.wantPublished)
		}
	}

	records, err := store.ReadAll(context.Background())
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(records) != 5 {
		t.Fatalf("len(records) = %d, want 5", len(records))
	}

	var committed int
	for _, record := range records {
		if record.CommittedAt != nil {
			committed++
		}
	}
	if committed != 2 {
		t.Fatalf("committed rows = %d, want 2", committed)
	}

	marker, ok, err := store.ReadMarker(context.Background(), MarkerKey{
		FacilityID:           "ashtonbee",
		ZoneID:               "gym-floor",
		ExternalIdentityHash: edge.HashAccount("1000123456", "salt"),
	})
	if err != nil {
		t.Fatalf("ReadMarker() error = %v", err)
	}
	if !ok {
		t.Fatal("ReadMarker() ok = false, want true")
	}
	if marker.LastEventID != "edge-out-accepted-001" {
		t.Fatalf("marker.LastEventID = %q, want edge-out-accepted-001", marker.LastEventID)
	}
	if marker.Direction != "out" {
		t.Fatalf("marker.Direction = %q, want out", marker.Direction)
	}
	if marker.LastRecordedAt != time.Date(2026, 4, 4, 12, 5, 0, 0, time.UTC) {
		t.Fatalf("marker.LastRecordedAt = %s, want 2026-04-04T12:05:00Z", marker.LastRecordedAt)
	}
	if got := countRows(t, store, "athena.edge_identity_markers"); got != 1 {
		t.Fatalf("edge_identity_markers rows = %d, want 1", got)
	}

	publicObservations, err := store.ReadPublicObservations(context.Background(), PublicFilter{
		FacilityID: "ashtonbee",
		Since:      time.Date(2026, 4, 4, 11, 0, 0, 0, time.UTC),
		Until:      time.Date(2026, 4, 4, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ReadPublicObservations() error = %v", err)
	}
	if len(publicObservations) != 5 {
		t.Fatalf("len(publicObservations) = %d, want 5", len(publicObservations))
	}

	restartedProjector := presence.NewProjector()
	replay, err := ReplayProjector(restartedProjector, records)
	if err != nil {
		t.Fatalf("ReplayProjector() error = %v", err)
	}
	if replay.Total != 5 {
		t.Fatalf("replay.Total = %d, want 5", replay.Total)
	}
	if replay.Pass != 4 {
		t.Fatalf("replay.Pass = %d, want 4", replay.Pass)
	}
	if replay.Fail != 1 {
		t.Fatalf("replay.Fail = %d, want 1", replay.Fail)
	}
	if replay.Applied != 2 {
		t.Fatalf("replay.Applied = %d, want 2", replay.Applied)
	}
	if replay.Observed != 2 {
		t.Fatalf("replay.Observed = %d, want 2", replay.Observed)
	}

	liveSnapshot, err := projector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "ashtonbee",
		ZoneID:     "gym-floor",
	})
	if err != nil {
		t.Fatalf("live CurrentOccupancy() error = %v", err)
	}
	restartedSnapshot, err := restartedProjector.CurrentOccupancy(context.Background(), domain.OccupancyFilter{
		FacilityID: "ashtonbee",
		ZoneID:     "gym-floor",
	})
	if err != nil {
		t.Fatalf("restarted CurrentOccupancy() error = %v", err)
	}
	if liveSnapshot.CurrentCount != restartedSnapshot.CurrentCount {
		t.Fatalf("live current_count = %d, restarted current_count = %d, want equal", liveSnapshot.CurrentCount, restartedSnapshot.CurrentCount)
	}

	report, err := store.ReadAnalytics(context.Background(), AnalyticsFilter{
		FacilityID:   "ashtonbee",
		ZoneID:       "gym-floor",
		NodeID:       "entry-node",
		Since:        time.Date(2026, 4, 4, 11, 0, 0, 0, time.UTC),
		Until:        time.Date(2026, 4, 4, 13, 0, 0, 0, time.UTC),
		BucketSize:   15 * time.Minute,
		SessionLimit: 10,
	})
	if err != nil {
		t.Fatalf("ReadAnalytics() error = %v", err)
	}
	if report.ObservationSummary.Total != 5 {
		t.Fatalf("ObservationSummary.Total = %d, want 5", report.ObservationSummary.Total)
	}
	if report.ObservationSummary.CommittedPass != 2 {
		t.Fatalf("ObservationSummary.CommittedPass = %d, want 2", report.ObservationSummary.CommittedPass)
	}
	if report.SessionSummary.OpenCount != 0 {
		t.Fatalf("SessionSummary.OpenCount = %d, want 0", report.SessionSummary.OpenCount)
	}
	if report.SessionSummary.ClosedCount != 1 {
		t.Fatalf("SessionSummary.ClosedCount = %d, want 1", report.SessionSummary.ClosedCount)
	}
	if report.SessionSummary.UnmatchedExitCount != 0 {
		t.Fatalf("SessionSummary.UnmatchedExitCount = %d, want 0", report.SessionSummary.UnmatchedExitCount)
	}
	if report.SessionSummary.UniqueVisitors != 1 {
		t.Fatalf("SessionSummary.UniqueVisitors = %d, want 1", report.SessionSummary.UniqueVisitors)
	}
	if report.SessionSummary.AverageDurationSeconds != 300 {
		t.Fatalf("SessionSummary.AverageDurationSeconds = %d, want 300", report.SessionSummary.AverageDurationSeconds)
	}
	if report.SessionSummary.MedianDurationSeconds != 300 {
		t.Fatalf("SessionSummary.MedianDurationSeconds = %d, want 300", report.SessionSummary.MedianDurationSeconds)
	}
	if report.SessionSummary.OccupancyAtEnd != 0 {
		t.Fatalf("SessionSummary.OccupancyAtEnd = %d, want 0", report.SessionSummary.OccupancyAtEnd)
	}
	if len(report.Sessions) != 1 {
		t.Fatalf("len(report.Sessions) = %d, want 1", len(report.Sessions))
	}
	if report.Sessions[0].State != "closed" {
		t.Fatalf("report.Sessions[0].State = %q, want closed", report.Sessions[0].State)
	}
	if report.Sessions[0].DurationSeconds == nil || *report.Sessions[0].DurationSeconds != 300 {
		t.Fatalf("report.Sessions[0].DurationSeconds = %v, want 300", report.Sessions[0].DurationSeconds)
	}
	if len(report.FlowBuckets) == 0 {
		t.Fatal("len(report.FlowBuckets) = 0, want at least one bucket")
	}
	var (
		passInTotal  int
		passOutTotal int
		failInTotal  int
	)
	for _, bucket := range report.FlowBuckets {
		passInTotal += bucket.PassIn
		passOutTotal += bucket.PassOut
		failInTotal += bucket.FailIn
	}
	if passInTotal != 2 || passOutTotal != 2 || failInTotal != 1 {
		t.Fatalf("flow totals = pass_in:%d pass_out:%d fail_in:%d, want 2/2/1", passInTotal, passOutTotal, failInTotal)
	}
}

func TestPostgresStoreTracksUnmatchedExitSessions(t *testing.T) {
	store := newPostgresStore(t)

	service, err := edge.NewService(
		&stubPublisher{},
		"salt",
		map[string]string{"exit-node": "exit-token"},
		edge.WithObservationRecorder(store),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := service.AcceptTap(context.Background(), "exit-token", edge.TapRequest{
		EventID:    "edge-out-only-001",
		AccountRaw: "1000123456",
		Direction:  "out",
		FacilityID: "ashtonbee",
		ZoneID:     "gym-floor",
		NodeID:     "exit-node",
		ObservedAt: "2026-04-04T12:00:00Z",
		Result:     "pass",
	})
	if err != nil {
		t.Fatalf("AcceptTap() error = %v", err)
	}
	if result.Status != "accepted" {
		t.Fatalf("result.Status = %q, want accepted", result.Status)
	}

	report, err := store.ReadAnalytics(context.Background(), AnalyticsFilter{
		FacilityID:   "ashtonbee",
		ZoneID:       "gym-floor",
		Since:        time.Date(2026, 4, 4, 11, 0, 0, 0, time.UTC),
		Until:        time.Date(2026, 4, 4, 13, 0, 0, 0, time.UTC),
		BucketSize:   15 * time.Minute,
		SessionLimit: 10,
	})
	if err != nil {
		t.Fatalf("ReadAnalytics() error = %v", err)
	}
	if report.SessionSummary.UnmatchedExitCount != 1 {
		t.Fatalf("SessionSummary.UnmatchedExitCount = %d, want 1", report.SessionSummary.UnmatchedExitCount)
	}
	if len(report.Sessions) != 1 || report.Sessions[0].State != "unmatched_exit" {
		t.Fatalf("report.Sessions = %#v, want unmatched_exit session", report.Sessions)
	}
}

func TestPostgresStoreAcceptsRecognizedDeniedThroughFacilityPolicy(t *testing.T) {
	store := newPostgresStore(t)
	projector := presence.NewProjector()

	policyRecord, err := store.CreateFacilityWindowPolicy(context.Background(), CreateFacilityWindowPolicyInput{
		FacilityID:         "ashtonbee",
		StartsAt:           time.Date(2026, 4, 4, 11, 0, 0, 0, time.UTC),
		EndsAt:             time.Date(2026, 4, 4, 13, 0, 0, 0, time.UTC),
		ReasonCode:         "testing_rollout",
		CreatedByActorKind: "owner_user",
		CreatedByActorID:   "tester",
		CreatedBySurface:   "athena_cli",
	})
	if err != nil {
		t.Fatalf("CreateFacilityWindowPolicy() error = %v", err)
	}
	if policyRecord.PolicyMode != "facility_window" {
		t.Fatalf("policy mode = %q, want facility_window", policyRecord.PolicyMode)
	}

	service, err := edge.NewService(
		&stubPublisher{},
		"salt",
		map[string]string{"entry-node": "entry-token"},
		edge.WithProjection(projector),
		edge.WithObservationRecorder(store),
		edge.WithAcceptedPresenceRecorder(store),
		edge.WithPolicyEvaluator(store),
		edge.WithPolicyAcceptanceEnabled(true),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := service.AcceptTap(context.Background(), "entry-token", edge.TapRequest{
		EventID:       "edge-policy-fail-001",
		AccountRaw:    "1000123456",
		Direction:     "in",
		FacilityID:    "ashtonbee",
		ZoneID:        "gym-floor",
		NodeID:        "entry-node",
		ObservedAt:    "2026-04-04T12:00:00Z",
		Result:        "fail",
		AccountType:   "Standard",
		Name:          "Fixture Student",
		StatusMessage: "No active access rule for this account.",
	})
	if err != nil {
		t.Fatalf("AcceptTap() error = %v", err)
	}
	if result.Status != "accepted" {
		t.Fatalf("result.Status = %q, want accepted", result.Status)
	}
	if result.Result != "fail" {
		t.Fatalf("result.Result = %q, want fail", result.Result)
	}

	records, err := store.ReadAll(context.Background())
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].FailureReasonCode != edge.FailureReasonRecognizedDenied {
		t.Fatalf("FailureReasonCode = %q, want %q", records[0].FailureReasonCode, edge.FailureReasonRecognizedDenied)
	}
	if records[0].AcceptedAt == nil {
		t.Fatal("AcceptedAt = nil, want policy-backed accepted presence")
	}
	if records[0].AcceptancePath != edge.AcceptancePathFacility {
		t.Fatalf("AcceptancePath = %q, want %q", records[0].AcceptancePath, edge.AcceptancePathFacility)
	}
	if records[0].AcceptedReasonCode != "testing_rollout" {
		t.Fatalf("AcceptedReasonCode = %q, want testing_rollout", records[0].AcceptedReasonCode)
	}

	publicObservations, err := store.ReadPublicObservations(context.Background(), PublicFilter{
		FacilityID: "ashtonbee",
		Since:      time.Date(2026, 4, 4, 11, 0, 0, 0, time.UTC),
		Until:      time.Date(2026, 4, 4, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ReadPublicObservations() error = %v", err)
	}
	if len(publicObservations) != 1 {
		t.Fatalf("len(publicObservations) = %d, want 1", len(publicObservations))
	}
	if !publicObservations[0].Accepted {
		t.Fatal("Accepted = false, want true")
	}
	if publicObservations[0].AcceptancePath != edge.AcceptancePathFacility {
		t.Fatalf("AcceptancePath = %q, want %q", publicObservations[0].AcceptancePath, edge.AcceptancePathFacility)
	}

	restartedProjector := presence.NewProjector()
	replay, err := ReplayProjector(restartedProjector, records)
	if err != nil {
		t.Fatalf("ReplayProjector() error = %v", err)
	}
	if replay.Applied != 1 {
		t.Fatalf("replay.Applied = %d, want 1", replay.Applied)
	}

	report, err := store.ReadAnalytics(context.Background(), AnalyticsFilter{
		FacilityID:   "ashtonbee",
		ZoneID:       "gym-floor",
		NodeID:       "entry-node",
		Since:        time.Date(2026, 4, 4, 11, 0, 0, 0, time.UTC),
		Until:        time.Date(2026, 4, 4, 13, 0, 0, 0, time.UTC),
		BucketSize:   15 * time.Minute,
		SessionLimit: 10,
	})
	if err != nil {
		t.Fatalf("ReadAnalytics() error = %v", err)
	}
	if report.ObservationSummary.Accepted != 1 {
		t.Fatalf("ObservationSummary.Accepted = %d, want 1", report.ObservationSummary.Accepted)
	}
	if report.ObservationSummary.AcceptedTestingPolicy != 1 {
		t.Fatalf("ObservationSummary.AcceptedTestingPolicy = %d, want 1", report.ObservationSummary.AcceptedTestingPolicy)
	}
	if report.ObservationSummary.RecognizedDenied != 1 {
		t.Fatalf("ObservationSummary.RecognizedDenied = %d, want 1", report.ObservationSummary.RecognizedDenied)
	}
	if report.SessionSummary.OccupancyAtEnd != 1 {
		t.Fatalf("SessionSummary.OccupancyAtEnd = %d, want 1", report.SessionSummary.OccupancyAtEnd)
	}
	if report.SessionSummary.ClosedCount != 0 {
		t.Fatalf("SessionSummary.ClosedCount = %d, want 0", report.SessionSummary.ClosedCount)
	}
}

func TestPostgresStoreListPoliciesReturnsFacilityWindowPolicies(t *testing.T) {
	store := newPostgresStore(t)

	startsAt := time.Date(2026, 4, 4, 11, 0, 0, 0, time.UTC)
	endsAt := time.Date(2026, 4, 4, 13, 0, 0, 0, time.UTC)
	policyRecord, err := store.CreateFacilityWindowPolicy(context.Background(), CreateFacilityWindowPolicyInput{
		FacilityID:         "ashtonbee",
		StartsAt:           startsAt,
		EndsAt:             endsAt,
		ReasonCode:         "testing_rollout",
		CreatedByActorKind: "owner_user",
		CreatedByActorID:   "tester",
		CreatedBySurface:   "athena_cli",
	})
	if err != nil {
		t.Fatalf("CreateFacilityWindowPolicy() error = %v", err)
	}

	activeAt := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	records, err := store.ListPolicies(context.Background(), "ashtonbee", "", &activeAt)
	if err != nil {
		t.Fatalf("ListPolicies() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].PolicyID != policyRecord.PolicyID {
		t.Fatalf("records[0].PolicyID = %q, want %q", records[0].PolicyID, policyRecord.PolicyID)
	}
	if records[0].SubjectID != "" {
		t.Fatalf("records[0].SubjectID = %q, want empty", records[0].SubjectID)
	}
	if records[0].PolicyMode != "facility_window" {
		t.Fatalf("records[0].PolicyMode = %q, want facility_window", records[0].PolicyMode)
	}
	if records[0].TargetSelector != "recognized_denied_only" {
		t.Fatalf("records[0].TargetSelector = %q, want recognized_denied_only", records[0].TargetSelector)
	}
}

func TestPostgresStoreRejectsUnauthorizedAndMalformedWrites(t *testing.T) {
	store := newPostgresStore(t)

	service, err := edge.NewService(
		&stubPublisher{},
		"salt",
		map[string]string{"entry-node": "entry-token"},
		edge.WithProjection(presence.NewProjector()),
		edge.WithObservationRecorder(store),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if _, err := service.AcceptTap(context.Background(), "wrong-token", edge.TapRequest{
		EventID:    "edge-unauthorized-001",
		AccountRaw: "1000123456",
		Direction:  "in",
		FacilityID: "ashtonbee",
		NodeID:     "entry-node",
		ObservedAt: "2026-04-04T12:00:00Z",
	}); err == nil {
		t.Fatal("AcceptTap(unauthorized) error = nil, want forbidden token error")
	}

	if _, err := service.AcceptTap(context.Background(), "entry-token", edge.TapRequest{
		EventID:    "edge-malformed-001",
		AccountRaw: "1000123456",
		Direction:  "sideways",
		FacilityID: "ashtonbee",
		NodeID:     "entry-node",
		ObservedAt: "2026-04-04T12:00:00Z",
	}); err == nil {
		t.Fatal("AcceptTap(malformed) error = nil, want validation error")
	}

	if got := countRows(t, store, "athena.edge_observations"); got != 0 {
		t.Fatalf("edge_observations rows = %d, want 0", got)
	}
	if got := countRows(t, store, "athena.edge_observation_commits"); got != 0 {
		t.Fatalf("edge_observation_commits rows = %d, want 0", got)
	}
	if got := countRows(t, store, "athena.edge_sessions"); got != 0 {
		t.Fatalf("edge_sessions rows = %d, want 0", got)
	}
	if got := countRows(t, store, "athena.edge_identity_markers"); got != 0 {
		t.Fatalf("edge_identity_markers rows = %d, want 0", got)
	}
}

func newPostgresStore(t *testing.T) *PostgresStore {
	t.Helper()

	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Fatalf("dockertest.NewPool() error = %v", err)
	}

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "16-alpine",
		Env: []string{
			"POSTGRES_USER=athena",
			"POSTGRES_PASSWORD=secret",
			"POSTGRES_DB=athena",
		},
	}, func(hostConfig *docker.HostConfig) {
		hostConfig.AutoRemove = true
		hostConfig.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		t.Fatalf("RunWithOptions() error = %v", err)
	}
	resource.Expire(120)

	dsn := fmt.Sprintf("postgres://athena:secret@127.0.0.1:%s/athena?sslmode=disable", resource.GetPort("5432/tcp"))

	if err := pool.Retry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		store, err := NewPostgresStore(ctx, dsn)
		if err != nil {
			return err
		}
		store.Close()
		return nil
	}); err != nil {
		_ = pool.Purge(resource)
		t.Fatalf("Retry() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	store, err := NewPostgresStore(ctx, dsn)
	if err != nil {
		_ = pool.Purge(resource)
		t.Fatalf("NewPostgresStore() error = %v", err)
	}
	if err := applyTestMigrations(ctx, store); err != nil {
		store.Close()
		_ = pool.Purge(resource)
		t.Fatalf("applyTestMigrations() error = %v", err)
	}

	t.Cleanup(func() {
		store.Close()
		_ = pool.Purge(resource)
	})

	return store
}

func applyTestMigrations(ctx context.Context, store *PostgresStore) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}

	paths, err := filepath.Glob(filepath.Join(root, "db", "migrations", "*.up.sql"))
	if err != nil {
		return fmt.Errorf("glob migrations: %w", err)
	}
	sort.Strings(paths)

	for _, path := range paths {
		payload, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", path, err)
		}
		if _, err := store.pool.Exec(ctx, string(payload)); err != nil {
			return fmt.Errorf("apply migration %s: %w", filepath.Base(path), err)
		}
	}

	return nil
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolve caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")), nil
}

func countRows(t *testing.T, store *PostgresStore, table string) int {
	t.Helper()

	var count int
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	if err := store.pool.QueryRow(context.Background(), query).Scan(&count); err != nil {
		t.Fatalf("QueryRow(%s) error = %v", table, err)
	}
	return count
}

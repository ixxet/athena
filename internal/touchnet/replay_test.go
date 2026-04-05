package touchnet

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ixxet/athena/internal/domain"
	edgeingress "github.com/ixxet/athena/internal/edge"
	"github.com/ixxet/athena/internal/metrics"
	"github.com/ixxet/athena/internal/presence"
	"github.com/ixxet/athena/internal/server"
)

type replayStubPublisher struct {
	subjects []string
}

func (s *replayStubPublisher) Publish(_ context.Context, subject string, _ []byte) error {
	s.subjects = append(s.subjects, subject)
	return nil
}

func TestParseAccessReportSkipsMetadataAndParsesRows(t *testing.T) {
	records, err := ParseAccessReport(strings.NewReader(strings.Join([]string{
		"Report 10101",
		"Generated for Ashtonbee",
		"",
		"ACCOUNT, NAME , LOCATION , DATE TIME",
		"1000123456,Student One,Entry Reader,04/04/2026 10:00:00",
		"1000123457,Student Two,Exit Reader,04/04/2026 10:05:00",
	}, "\n")))
	if err != nil {
		t.Fatalf("ParseAccessReport() error = %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[0].Account != "1000123456" {
		t.Fatalf("records[0].Account = %q, want 1000123456", records[0].Account)
	}
	if records[1].Location != "Exit Reader" {
		t.Fatalf("records[1].Location = %q, want Exit Reader", records[1].Location)
	}
	if records[0].ObservedAt.Format(time.RFC3339) != "2026-04-04T10:00:00Z" {
		t.Fatalf("records[0].ObservedAt = %s, want 2026-04-04T10:00:00Z", records[0].ObservedAt.Format(time.RFC3339))
	}
}

func TestParseAccessReportRejectsMissingHeader(t *testing.T) {
	_, err := ParseAccessReport(strings.NewReader("ACCOUNT,LOCATION\n1000123456,Entry Reader\n"))
	if err == nil {
		t.Fatal("ParseAccessReport() error = nil, want missing header error")
	}
}

func TestReplayerRejectsUnmappedLocation(t *testing.T) {
	replayer, err := NewReplayer(ReplayConfig{
		FacilityID:    "ashtonbee",
		EntryLocation: "Entry Reader",
		ExitLocation:  "Exit Reader",
		BaseURL:       "http://127.0.0.1:8080",
		NodeID:        "entry-node",
		Token:         "entry-token",
		TimeScale:     0,
	})
	if err != nil {
		t.Fatalf("NewReplayer() error = %v", err)
	}

	_, err = replayer.Replay(context.Background(), []AccessRecord{{
		Account:    "1000123456",
		Location:   "Side Door",
		ObservedAt: time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC),
		RowNumber:  6,
	}})
	if err == nil {
		t.Fatal("Replay() error = nil, want unmapped location error")
	}
	if !strings.Contains(err.Error(), "row 6") {
		t.Fatalf("Replay() error = %q, want row context", err)
	}
}

func TestReplayerPostsAcceptedRowsAndRespectsBurstMode(t *testing.T) {
	var requests []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/edge/tap" {
			t.Fatalf("path = %q, want /api/v1/edge/tap", r.URL.Path)
		}
		if r.Header.Get("X-Ashton-Edge-Token") != "entry-token" {
			t.Fatalf("token = %q, want entry-token", r.Header.Get("X-Ashton-Edge-Token"))
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode(body) error = %v", err)
		}
		requests = append(requests, body)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	}))
	defer server.Close()

	replayer, err := NewReplayer(ReplayConfig{
		FacilityID:    "ashtonbee",
		ZoneID:        "gym-floor",
		EntryLocation: "Entry Reader",
		ExitLocation:  "Exit Reader",
		BaseURL:       server.URL,
		NodeID:        "entry-node",
		Token:         "entry-token",
		TimeScale:     0,
	})
	if err != nil {
		t.Fatalf("NewReplayer() error = %v", err)
	}

	sleeps := make([]time.Duration, 0)
	replayer.sleep = func(_ context.Context, delay time.Duration) error {
		sleeps = append(sleeps, delay)
		return nil
	}

	sent, err := replayer.Replay(context.Background(), []AccessRecord{
		{
			Account:    "1000123456",
			Location:   "Entry Reader",
			ObservedAt: time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC),
			RowNumber:  4,
		},
		{
			Account:    "1000123456",
			Location:   "Exit Reader",
			ObservedAt: time.Date(2026, 4, 4, 10, 5, 0, 0, time.UTC),
			RowNumber:  5,
		},
	})
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}

	if sent != 2 {
		t.Fatalf("sent = %d, want 2", sent)
	}
	if len(requests) != 2 {
		t.Fatalf("len(requests) = %d, want 2", len(requests))
	}
	if requests[0]["direction"] != "in" {
		t.Fatalf("direction[0] = %q, want in", requests[0]["direction"])
	}
	if requests[1]["direction"] != "out" {
		t.Fatalf("direction[1] = %q, want out", requests[1]["direction"])
	}
	if requests[0]["facility_id"] != "ashtonbee" {
		t.Fatalf("facility_id = %q, want ashtonbee", requests[0]["facility_id"])
	}
	if len(sleeps) != 0 {
		t.Fatalf("len(sleeps) = %d, want 0 in burst mode", len(sleeps))
	}
}

func TestReplayerScalesDelays(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	replayer, err := NewReplayer(ReplayConfig{
		FacilityID:    "ashtonbee",
		EntryLocation: "Entry Reader",
		ExitLocation:  "Exit Reader",
		BaseURL:       server.URL,
		NodeID:        "entry-node",
		Token:         "entry-token",
		TimeScale:     2.0,
	})
	if err != nil {
		t.Fatalf("NewReplayer() error = %v", err)
	}

	sleeps := make([]time.Duration, 0)
	replayer.sleep = func(_ context.Context, delay time.Duration) error {
		sleeps = append(sleeps, delay)
		return nil
	}

	_, err = replayer.Replay(context.Background(), []AccessRecord{
		{
			Account:    "1000123456",
			Location:   "Entry Reader",
			ObservedAt: time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC),
			RowNumber:  4,
		},
		{
			Account:    "1000123457",
			Location:   "Entry Reader",
			ObservedAt: time.Date(2026, 4, 4, 10, 4, 0, 0, time.UTC),
			RowNumber:  5,
		},
	})
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}

	if len(sleeps) != 1 {
		t.Fatalf("len(sleeps) = %d, want 1", len(sleeps))
	}
	if sleeps[0] != 2*time.Minute {
		t.Fatalf("sleeps[0] = %s, want 2m0s", sleeps[0])
	}
}

func TestReplayerDrivesLiveProjectionPath(t *testing.T) {
	projector := presence.NewProjectorWithClock(func() time.Time {
		return time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	})
	readPath := presence.NewReadPath(projector, domain.OccupancyFilter{FacilityID: "ashtonbee"})
	publisher := &replayStubPublisher{}
	edgeService, err := edgeingress.NewService(
		publisher,
		"salt",
		map[string]string{"entry-node": "entry-token"},
		edgeingress.WithProjection(projector),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	handler := server.NewHandler(
		readPath,
		metrics.New(readPath),
		"edge-projection",
		server.WithEdgeTapHandler(edgeingress.NewHandler(edgeService)),
	)

	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	replayer, err := NewReplayer(ReplayConfig{
		FacilityID:    "ashtonbee",
		ZoneID:        "gym-floor",
		EntryLocation: "Entry Reader",
		ExitLocation:  "Exit Reader",
		BaseURL:       testServer.URL,
		NodeID:        "entry-node",
		Token:         "entry-token",
		TimeScale:     0,
	})
	if err != nil {
		t.Fatalf("NewReplayer() error = %v", err)
	}

	sent, err := replayer.Replay(context.Background(), []AccessRecord{
		{
			Account:    "1000123456",
			Location:   "Entry Reader",
			ObservedAt: time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC),
			RowNumber:  4,
		},
		{
			Account:    "1000123456",
			Location:   "Exit Reader",
			ObservedAt: time.Date(2026, 4, 4, 10, 5, 0, 0, time.UTC),
			RowNumber:  5,
		},
	})
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if sent != 2 {
		t.Fatalf("sent = %d, want 2", sent)
	}

	response, err := http.Get(testServer.URL + "/api/v1/presence/count?facility=ashtonbee&zone=gym-floor")
	if err != nil {
		t.Fatalf("GET presence/count error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("count status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("Decode(count response) error = %v", err)
	}
	if currentCount := int(body["current_count"].(float64)); currentCount != 0 {
		t.Fatalf("current_count = %d, want 0 after replayed in/out", currentCount)
	}
	if len(publisher.subjects) != 2 {
		t.Fatalf("len(subjects) = %d, want 2", len(publisher.subjects))
	}
}

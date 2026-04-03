package adapter

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ixxet/athena/internal/domain"
)

func TestNewCSVAdapterRejectsMissingPath(t *testing.T) {
	_, err := NewCSVAdapter(CSVConfig{}, testLogger(nil))
	if err == nil {
		t.Fatal("NewCSVAdapter() error = nil, want missing path error")
	}
	if !strings.Contains(err.Error(), "ATHENA_CSV_PATH") {
		t.Fatalf("NewCSVAdapter() error = %q, want ATHENA_CSV_PATH context", err)
	}
}

func TestNewCSVAdapterRejectsMissingSourceFile(t *testing.T) {
	_, err := NewCSVAdapter(CSVConfig{Path: filepath.Join(t.TempDir(), "missing.csv")}, testLogger(nil))
	if err == nil {
		t.Fatal("NewCSVAdapter() error = nil, want missing source error")
	}
	if !strings.Contains(err.Error(), "read csv source") {
		t.Fatalf("NewCSVAdapter() error = %q, want read csv source context", err)
	}
}

func TestNewCSVAdapterRejectsUnreadableDirectorySource(t *testing.T) {
	dir := t.TempDir()

	_, err := NewCSVAdapter(CSVConfig{Path: dir}, testLogger(nil))
	if err == nil {
		t.Fatal("NewCSVAdapter() error = nil, want unreadable directory error")
	}
	if !strings.Contains(err.Error(), "parse csv source") && !strings.Contains(err.Error(), "read csv source") {
		t.Fatalf("NewCSVAdapter() error = %q, want source read or parse context", err)
	}
}

func TestNewCSVAdapterRejectsMissingRequiredColumns(t *testing.T) {
	path := writeCSVFile(t, "event_id,facility_id,direction\ncsv-001,ashtonbee,in\n")

	_, err := NewCSVAdapter(CSVConfig{Path: path}, testLogger(nil))
	if err == nil {
		t.Fatal("NewCSVAdapter() error = nil, want missing column error")
	}
	if !strings.Contains(err.Error(), "missing required column") {
		t.Fatalf("NewCSVAdapter() error = %q, want missing required column context", err)
	}
}

func TestCSVAdapterListEventsParsesValidFileAndSortsDeterministically(t *testing.T) {
	path := writeCSVFile(t, strings.Join([]string{
		"event_id,facility_id,zone_id,external_identity_hash,direction,recorded_at",
		"csv-out-001,ashtonbee,lobby,,out,2026-04-01T08:02:00Z",
		"csv-in-002,ashtonbee,lobby,tag-002,in,2026-04-01T08:01:00Z",
		"csv-in-001,ashtonbee,lobby,tag-001,in,2026-04-01T08:01:00Z",
		"",
	}, "\n"))

	var logs bytes.Buffer
	adapter, err := NewCSVAdapter(CSVConfig{Path: path}, testLogger(&logs))
	if err != nil {
		t.Fatalf("NewCSVAdapter() error = %v", err)
	}

	first, err := adapter.ListEvents(context.Background())
	if err != nil {
		t.Fatalf("ListEvents() first error = %v", err)
	}
	second, err := adapter.ListEvents(context.Background())
	if err != nil {
		t.Fatalf("ListEvents() second error = %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("ListEvents() repeated loads differ:\nfirst=%#v\nsecond=%#v", first, second)
	}

	gotIDs := []string{first[0].ID, first[1].ID, first[2].ID}
	wantIDs := []string{"csv-in-001", "csv-in-002", "csv-out-001"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("sorted event ids = %#v, want %#v", gotIDs, wantIDs)
	}
	if first[0].Source != domain.SourceCSV {
		t.Fatalf("first event source = %q, want %q", first[0].Source, domain.SourceCSV)
	}

	logText := logs.String()
	if !strings.Contains(logText, "csv adapter initialized") || !strings.Contains(logText, "csv adapter refreshed") {
		t.Fatalf("logs = %q, want initialization and refresh logs", logText)
	}
}

func TestCSVAdapterListEventsRejectsMalformedRows(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name: "invalid direction",
			payload: strings.Join([]string{
				"event_id,facility_id,direction,recorded_at",
				"csv-001,ashtonbee,sideways,2026-04-01T08:00:00Z",
			}, "\n"),
			want: "must be one of in,out",
		},
		{
			name: "invalid timestamp",
			payload: strings.Join([]string{
				"event_id,facility_id,direction,recorded_at",
				"csv-001,ashtonbee,in,not-a-time",
			}, "\n"),
			want: "recorded_at",
		},
		{
			name: "missing facility",
			payload: strings.Join([]string{
				"event_id,facility_id,direction,recorded_at",
				"csv-001,,in,2026-04-01T08:00:00Z",
			}, "\n"),
			want: "facility_id is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeCSVFile(t, tc.payload)

			_, err := NewCSVAdapter(CSVConfig{Path: path}, testLogger(nil))
			if err == nil {
				t.Fatal("NewCSVAdapter() error = nil, want malformed row error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("NewCSVAdapter() error = %q, want %q", err, tc.want)
			}
		})
	}
}

func TestCSVAdapterListEventsRejectsDuplicateEventIDs(t *testing.T) {
	path := writeCSVFile(t, strings.Join([]string{
		"event_id,facility_id,direction,recorded_at",
		"csv-001,ashtonbee,in,2026-04-01T08:00:00Z",
		"csv-001,ashtonbee,out,2026-04-01T08:01:00Z",
	}, "\n"))

	_, err := NewCSVAdapter(CSVConfig{Path: path}, testLogger(nil))
	if err == nil {
		t.Fatal("NewCSVAdapter() error = nil, want duplicate event error")
	}
	if !strings.Contains(err.Error(), "duplicate event_id") {
		t.Fatalf("NewCSVAdapter() error = %q, want duplicate event_id context", err)
	}
}

func TestCSVAdapterListEventsHandlesEmptyValidFile(t *testing.T) {
	path := writeCSVFile(t, "event_id,facility_id,direction,recorded_at\n")

	adapter, err := NewCSVAdapter(CSVConfig{Path: path}, testLogger(nil))
	if err != nil {
		t.Fatalf("NewCSVAdapter() error = %v", err)
	}

	events, err := adapter.ListEvents(context.Background())
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("len(events) = %d, want 0", len(events))
	}
}

func TestCSVAdapterLogsParseFailureOnRefresh(t *testing.T) {
	path := writeCSVFile(t, strings.Join([]string{
		"event_id,facility_id,direction,recorded_at",
		"csv-001,ashtonbee,in,2026-04-01T08:00:00Z",
	}, "\n"))

	var logs bytes.Buffer
	adapter, err := NewCSVAdapter(CSVConfig{Path: path}, testLogger(&logs))
	if err != nil {
		t.Fatalf("NewCSVAdapter() error = %v", err)
	}

	if err := os.WriteFile(path, []byte("event_id,facility_id,direction,recorded_at\ncsv-001,ashtonbee,bad,2026-04-01T08:00:00Z\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err = adapter.ListEvents(context.Background())
	if err == nil {
		t.Fatal("ListEvents() error = nil, want parse failure")
	}
	if !strings.Contains(logs.String(), "csv adapter refresh failed") {
		t.Fatalf("logs = %q, want refresh failure log", logs.String())
	}
}

func writeCSVFile(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "events.csv")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func testLogger(buffer *bytes.Buffer) *slog.Logger {
	if buffer == nil {
		buffer = &bytes.Buffer{}
	}
	return slog.New(slog.NewTextHandler(buffer, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

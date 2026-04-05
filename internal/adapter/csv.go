package adapter

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ixxet/athena/internal/domain"
)

type CSVConfig struct {
	Path string
}

type CSVAdapter struct {
	path   string
	logger *slog.Logger
}

func NewCSVAdapter(cfg CSVConfig, logger *slog.Logger) (*CSVAdapter, error) {
	if strings.TrimSpace(cfg.Path) == "" {
		return nil, fmt.Errorf("ATHENA_CSV_PATH is required when ATHENA_ADAPTER=csv")
	}
	if logger == nil {
		logger = slog.Default()
	}

	adapter := &CSVAdapter{
		path:   cfg.Path,
		logger: logger,
	}

	events, err := adapter.loadEvents()
	if err != nil {
		adapter.logger.Error("csv adapter initialization failed", "path", adapter.path, "error", err)
		return nil, err
	}

	adapter.logger.Info("csv adapter initialized", "path", adapter.path, "event_count", len(events))
	return adapter, nil
}

func (a *CSVAdapter) Name() string {
	return string(domain.SourceCSV)
}

func (a *CSVAdapter) ListEvents(_ context.Context) ([]domain.PresenceEvent, error) {
	return a.loadEvents()
}

func (a *CSVAdapter) loadEvents() ([]domain.PresenceEvent, error) {
	file, err := os.Open(a.path)
	if err != nil {
		err = fmt.Errorf("read csv source %q: %w", a.path, err)
		a.logger.Error("csv adapter refresh failed", "path", a.path, "error", err)
		return nil, err
	}
	defer file.Close()

	events, observedAt, err := ParseCSVEvents(file)
	if err != nil {
		err = fmt.Errorf("parse csv source %q: %w", a.path, err)
		a.logger.Error("csv adapter refresh failed", "path", a.path, "error", err)
		return nil, err
	}

	logFields := []any{
		"path", a.path,
		"event_count", len(events),
	}
	if !observedAt.IsZero() {
		logFields = append(logFields, "observed_at", observedAt.UTC().Format(time.RFC3339))
	}
	a.logger.Info("csv adapter refreshed", logFields...)

	return events, nil
}

func ParseCSVEvents(reader io.Reader) ([]domain.PresenceEvent, time.Time, error) {
	csvReader := csv.NewReader(reader)
	csvReader.TrimLeadingSpace = true
	csvReader.FieldsPerRecord = -1

	header, err := csvReader.Read()
	if err != nil {
		if err == io.EOF {
			return nil, time.Time{}, fmt.Errorf("csv header is required")
		}
		return nil, time.Time{}, err
	}

	columns := normalizeHeader(header)
	required := []string{"event_id", "facility_id", "direction", "recorded_at"}
	for _, column := range required {
		if _, ok := columns[column]; !ok {
			return nil, time.Time{}, fmt.Errorf("missing required column %q", column)
		}
	}

	var (
		events     []domain.PresenceEvent
		observedAt time.Time
		seenIDs    = make(map[string]int)
		rowNumber  = 1
	)

	for {
		rowNumber++
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("read row %d: %w", rowNumber, err)
		}
		if rowIsBlank(record) {
			continue
		}

		event, err := parseCSVRecord(columns, record, rowNumber)
		if err != nil {
			return nil, time.Time{}, err
		}
		if firstRow, exists := seenIDs[event.ID]; exists {
			return nil, time.Time{}, fmt.Errorf("row %d: duplicate event_id %q (first seen on row %d)", rowNumber, event.ID, firstRow)
		}
		seenIDs[event.ID] = rowNumber

		if event.RecordedAt.After(observedAt) {
			observedAt = event.RecordedAt
		}
		events = append(events, event)
	}

	slices.SortStableFunc(events, func(left, right domain.PresenceEvent) int {
		if cmp := left.RecordedAt.Compare(right.RecordedAt); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(left.ID, right.ID); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(left.FacilityID, right.FacilityID); cmp != 0 {
			return cmp
		}
		return strings.Compare(left.ZoneID, right.ZoneID)
	})

	return events, observedAt, nil
}

func parseCSVRecord(columns map[string]int, record []string, rowNumber int) (domain.PresenceEvent, error) {
	id := valueAt(record, columns["event_id"])
	if id == "" {
		return domain.PresenceEvent{}, fmt.Errorf("row %d: event_id is required", rowNumber)
	}

	facilityID := valueAt(record, columns["facility_id"])
	if facilityID == "" {
		return domain.PresenceEvent{}, fmt.Errorf("row %d: facility_id is required", rowNumber)
	}

	directionText := valueAt(record, columns["direction"])
	direction, err := domain.ParseDirection(directionText)
	if err != nil {
		return domain.PresenceEvent{}, fmt.Errorf("row %d: %w", rowNumber, err)
	}

	recordedAtText := valueAt(record, columns["recorded_at"])
	if recordedAtText == "" {
		return domain.PresenceEvent{}, fmt.Errorf("row %d: recorded_at is required", rowNumber)
	}
	recordedAt, err := time.Parse(time.RFC3339Nano, recordedAtText)
	if err != nil {
		return domain.PresenceEvent{}, fmt.Errorf("row %d: recorded_at %q: %w", rowNumber, recordedAtText, err)
	}

	event := domain.PresenceEvent{
		ID:                   id,
		FacilityID:           facilityID,
		ZoneID:               valueAtOptional(record, columns, "zone_id"),
		ExternalIdentityHash: valueAtOptional(record, columns, "external_identity_hash"),
		Direction:            direction,
		Source:               domain.SourceCSV,
		RecordedAt:           recordedAt.UTC(),
	}

	return event, nil
}

func normalizeHeader(header []string) map[string]int {
	columns := make(map[string]int, len(header))
	for index, value := range header {
		key := strings.ToLower(strings.TrimSpace(value))
		if key == "" {
			continue
		}
		if _, exists := columns[key]; !exists {
			columns[key] = index
		}
	}
	return columns
}

func valueAt(record []string, index int) string {
	if index < 0 || index >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[index])
}

func valueAtOptional(record []string, columns map[string]int, key string) string {
	index, ok := columns[key]
	if !ok {
		return ""
	}
	return valueAt(record, index)
}

func rowIsBlank(record []string) bool {
	for _, value := range record {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

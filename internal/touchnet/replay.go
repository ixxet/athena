package touchnet

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ixxet/athena/internal/domain"
	"github.com/ixxet/athena/internal/edge"
)

type AccessRecord struct {
	Account    string
	Name       string
	Location   string
	ObservedAt time.Time
	RowNumber  int
}

type ReplayConfig struct {
	FacilityID    string
	ZoneID        string
	EntryLocation string
	ExitLocation  string
	BaseURL       string
	NodeID        string
	Token         string
	TimeScale     float64
	HTTPClient    *http.Client
}

type Replayer struct {
	cfg    ReplayConfig
	client *http.Client
	sleep  func(context.Context, time.Duration) error
}

func ParseAccessReport(reader io.Reader) ([]AccessRecord, error) {
	csvReader := csv.NewReader(reader)
	csvReader.TrimLeadingSpace = true
	csvReader.FieldsPerRecord = -1

	columns := map[string]int(nil)
	rowNumber := 0
	records := make([]AccessRecord, 0)

	for {
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read TouchNet CSV row %d: %w", rowNumber+1, err)
		}

		rowNumber++
		if rowIsBlank(row) {
			continue
		}

		if columns == nil {
			normalized := normalizeHeader(row)
			if isAccessReportHeader(normalized) {
				columns = normalized
			}
			continue
		}

		record, err := parseAccessRecord(columns, row, rowNumber)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	if columns == nil {
		return nil, fmt.Errorf("TouchNet CSV header is required")
	}

	return records, nil
}

func NewReplayer(cfg ReplayConfig) (*Replayer, error) {
	if strings.TrimSpace(cfg.FacilityID) == "" {
		return nil, fmt.Errorf("facility id is required")
	}
	if strings.TrimSpace(cfg.EntryLocation) == "" {
		return nil, fmt.Errorf("entry location is required")
	}
	if strings.TrimSpace(cfg.ExitLocation) == "" {
		return nil, fmt.Errorf("exit location is required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	if strings.TrimSpace(cfg.NodeID) == "" {
		return nil, fmt.Errorf("node id is required")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("token is required")
	}
	if cfg.TimeScale < 0 {
		return nil, fmt.Errorf("time scale must be >= 0")
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	return &Replayer{
		cfg:    cfg,
		client: client,
		sleep:  sleepWithContext,
	}, nil
}

func (r *Replayer) Replay(ctx context.Context, records []AccessRecord) (int, error) {
	endpoint, err := url.JoinPath(strings.TrimRight(r.cfg.BaseURL, "/"), "/api/v1/edge/tap")
	if err != nil {
		return 0, fmt.Errorf("build edge tap URL: %w", err)
	}

	sent := 0
	for index, record := range records {
		direction, err := r.directionForLocation(record.Location)
		if err != nil {
			return sent, fmt.Errorf("row %d: %w", record.RowNumber, err)
		}

		if index > 0 {
			delay := record.ObservedAt.Sub(records[index-1].ObservedAt)
			if err := r.wait(ctx, delay); err != nil {
				return sent, err
			}
		}

		reqBody := edge.TapRequest{
			EventID:    edge.DeriveEventID(r.cfg.NodeID, direction, record.Account, record.ObservedAt),
			AccountRaw: record.Account,
			Direction:  string(direction),
			FacilityID: r.cfg.FacilityID,
			ZoneID:     r.cfg.ZoneID,
			NodeID:     r.cfg.NodeID,
			ObservedAt: record.ObservedAt.UTC().Format(time.RFC3339Nano),
		}

		if err := r.postTap(ctx, endpoint, reqBody); err != nil {
			return sent, fmt.Errorf("row %d: %w", record.RowNumber, err)
		}

		sent++
	}

	return sent, nil
}

func (r *Replayer) postTap(ctx context.Context, endpoint string, body edge.TapRequest) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ashton-Edge-Token", r.cfg.Token)

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("edge tap returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

func (r *Replayer) directionForLocation(location string) (domain.PresenceDirection, error) {
	switch strings.TrimSpace(location) {
	case strings.TrimSpace(r.cfg.EntryLocation):
		return domain.DirectionIn, nil
	case strings.TrimSpace(r.cfg.ExitLocation):
		return domain.DirectionOut, nil
	default:
		return "", fmt.Errorf("location %q does not match entry or exit mapping", location)
	}
}

func (r *Replayer) wait(ctx context.Context, delay time.Duration) error {
	if r.cfg.TimeScale == 0 || delay <= 0 {
		return nil
	}

	scaled := time.Duration(float64(delay) / r.cfg.TimeScale)
	if scaled <= 0 {
		return nil
	}

	return r.sleep(ctx, scaled)
}

func parseAccessRecord(columns map[string]int, row []string, rowNumber int) (AccessRecord, error) {
	account := valueAt(row, columns["account"])
	if account == "" {
		return AccessRecord{}, fmt.Errorf("row %d: ACCOUNT is required", rowNumber)
	}

	location := valueAt(row, columns["location"])
	if location == "" {
		return AccessRecord{}, fmt.Errorf("row %d: LOCATION is required", rowNumber)
	}

	dateTimeText := valueAt(row, columns["date time"])
	if dateTimeText == "" {
		return AccessRecord{}, fmt.Errorf("row %d: DATE TIME is required", rowNumber)
	}

	observedAt, err := parseTouchNetTime(dateTimeText)
	if err != nil {
		return AccessRecord{}, fmt.Errorf("row %d: DATE TIME %q: %w", rowNumber, dateTimeText, err)
	}

	return AccessRecord{
		Account:    account,
		Name:       valueAt(row, columns["name"]),
		Location:   location,
		ObservedAt: observedAt.UTC(),
		RowNumber:  rowNumber,
	}, nil
}

func isAccessReportHeader(columns map[string]int) bool {
	_, hasAccount := columns["account"]
	_, hasName := columns["name"]
	_, hasLocation := columns["location"]
	_, hasDateTime := columns["date time"]
	return hasAccount && hasName && hasLocation && hasDateTime
}

func normalizeHeader(row []string) map[string]int {
	columns := make(map[string]int, len(row))
	for index, value := range row {
		key := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
		if key == "" {
			continue
		}
		if _, exists := columns[key]; !exists {
			columns[key] = index
		}
	}
	return columns
}

func valueAt(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func rowIsBlank(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func parseTouchNetTime(value string) (time.Time, error) {
	layouts := []string{
		"01/02/2006 15:04:05",
		"1/2/2006 15:04:05",
		time.RFC3339Nano,
	}

	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return parsed, nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported timestamp format")
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

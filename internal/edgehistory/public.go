package edgehistory

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ixxet/athena/internal/domain"
	"github.com/ixxet/athena/internal/edge"
)

type PublicFilter struct {
	FacilityID string
	Since      time.Time
	Until      time.Time
}

type PublicObservation struct {
	FacilityID         string                   `json:"facility_id"`
	Direction          domain.PresenceDirection `json:"direction"`
	Result             string                   `json:"result"`
	ObservedAt         time.Time                `json:"observed_at"`
	Committed          bool                     `json:"committed"`
	Accepted           bool                     `json:"accepted"`
	AcceptancePath     string                   `json:"acceptance_path,omitempty"`
	AcceptedReasonCode string                   `json:"accepted_reason_code,omitempty"`
}

func ReadPublicObservations(path string, filter PublicFilter) ([]PublicObservation, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return nil, fmt.Errorf("edge observation history path is required")
	}

	facilityID := strings.TrimSpace(filter.FacilityID)
	if facilityID == "" {
		return nil, fmt.Errorf("facility_id is required")
	}
	if filter.Since.IsZero() {
		return nil, fmt.Errorf("since is required")
	}
	if filter.Until.IsZero() {
		return nil, fmt.Errorf("until is required")
	}

	since := filter.Since.UTC()
	until := filter.Until.UTC()
	if until.Before(since) {
		return nil, fmt.Errorf("until must be greater than or equal to since")
	}

	records, err := ReadAll(trimmedPath)
	if err != nil {
		return nil, err
	}

	observations := make([]PublicObservation, 0, len(records))
	for _, record := range records {
		if strings.TrimSpace(record.FacilityID) != facilityID {
			continue
		}

		observedAt := record.ObservedAt.UTC()
		if observedAt.Before(since) || observedAt.After(until) {
			continue
		}

		observations = append(observations, PublicObservation{
			FacilityID:         facilityID,
			Direction:          record.Direction,
			Result:             record.Result,
			ObservedAt:         observedAt,
			Committed:          record.CommittedAt != nil,
			Accepted:           record.AcceptedAt != nil || (record.Result == "pass" && record.CommittedAt != nil),
			AcceptancePath:     acceptancePath(record),
			AcceptedReasonCode: record.AcceptedReasonCode,
		})
	}

	sort.Slice(observations, func(i, j int) bool {
		left := observations[i]
		right := observations[j]
		if !left.ObservedAt.Equal(right.ObservedAt) {
			return left.ObservedAt.Before(right.ObservedAt)
		}
		if left.Result != right.Result {
			return left.Result < right.Result
		}
		return left.Direction < right.Direction
	})

	return observations, nil
}

func acceptancePath(record edge.ObservationRecord) string {
	if strings.TrimSpace(record.AcceptancePath) != "" {
		return record.AcceptancePath
	}
	if record.Result == "pass" && record.CommittedAt != nil {
		return edge.AcceptancePathTouchNetPass
	}
	return ""
}

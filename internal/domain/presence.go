package domain

import "time"

type PresenceDirection string

const (
	DirectionIn  PresenceDirection = "in"
	DirectionOut PresenceDirection = "out"
)

type PresenceSource string

const (
	SourceMock PresenceSource = "mock"
)

type PresenceEvent struct {
	ID                   string            `json:"id"`
	FacilityID           string            `json:"facility_id"`
	ZoneID               string            `json:"zone_id,omitempty"`
	ExternalIdentityHash string            `json:"external_identity_hash,omitempty"`
	Direction            PresenceDirection `json:"direction"`
	Source               PresenceSource    `json:"source"`
	RecordedAt           time.Time         `json:"recorded_at"`
	Metadata             map[string]string `json:"metadata,omitempty"`
}

type OccupancyFilter struct {
	FacilityID string
	ZoneID     string
}

type PresenceState struct {
	FacilityID string    `json:"facility_id"`
	ZoneID     string    `json:"zone_id,omitempty"`
	Arrivals   int       `json:"arrivals"`
	Departures int       `json:"departures"`
	ObservedAt time.Time `json:"observed_at"`
}

type OccupancyState struct {
	FacilityID   string    `json:"facility_id"`
	ZoneID       string    `json:"zone_id,omitempty"`
	CurrentCount int       `json:"current_count"`
	ObservedAt   time.Time `json:"observed_at"`
}

func (e PresenceEvent) Delta() int {
	switch e.Direction {
	case DirectionIn:
		return 1
	case DirectionOut:
		return -1
	default:
		return 0
	}
}

func (s PresenceState) CurrentCount() int {
	currentCount := s.Arrivals - s.Departures
	if currentCount < 0 {
		return 0
	}

	return currentCount
}

func (s PresenceState) Occupancy() OccupancyState {
	return OccupancyState{
		FacilityID:   s.FacilityID,
		ZoneID:       s.ZoneID,
		CurrentCount: s.CurrentCount(),
		ObservedAt:   s.ObservedAt,
	}
}

package edgehistory

import (
	"context"
	"time"

	"github.com/ixxet/athena/internal/edge"
)

type ReplayReader interface {
	ReadAll(context.Context) ([]edge.ObservationRecord, error)
}

type RecentObservationReader interface {
	ReadRecent(context.Context, int) ([]edge.ObservationRecord, error)
}

type MarkerKey struct {
	FacilityID           string `json:"facility_id"`
	ZoneID               string `json:"zone_id,omitempty"`
	ExternalIdentityHash string `json:"external_identity_hash"`
}

type MarkerRecord struct {
	MarkerKey
	ObservationID  string    `json:"observation_id,omitempty"`
	LastRecordedAt time.Time `json:"last_recorded_at"`
	LastEventID    string    `json:"last_event_id"`
	Direction      string    `json:"direction"`
	CommittedAt    time.Time `json:"committed_at"`
}

type MarkerReader interface {
	ReadMarker(context.Context, MarkerKey) (MarkerRecord, bool, error)
}

type PublicObservationReader interface {
	ReadPublicObservations(context.Context, PublicFilter) ([]PublicObservation, error)
}

type AnalyticsReader interface {
	ReadAnalytics(context.Context, AnalyticsFilter) (AnalyticsReport, error)
}

func (s *FileStore) ReadAll(ctx context.Context) ([]edge.ObservationRecord, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	return ReadAll(s.path)
}

func (s *FileStore) ReadRecent(ctx context.Context, limit int) ([]edge.ObservationRecord, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	return ReadRecent(s.path, limit)
}

func (s *FileStore) ReadPublicObservations(ctx context.Context, filter PublicFilter) ([]PublicObservation, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	return ReadPublicObservations(s.path, filter)
}

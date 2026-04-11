package edgehistory

import (
	"context"

	"github.com/ixxet/athena/internal/edge"
)

type ReplayReader interface {
	ReadAll(context.Context) ([]edge.ObservationRecord, error)
}

type RecentObservationReader interface {
	ReadRecent(context.Context, int) ([]edge.ObservationRecord, error)
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

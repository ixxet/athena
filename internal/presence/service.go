package presence

import (
	"context"
	"time"

	"github.com/ixxet/athena/internal/adapter"
	"github.com/ixxet/athena/internal/domain"
)

type Service struct {
	adapter adapter.PresenceAdapter
}

func NewService(adapter adapter.PresenceAdapter) *Service {
	return &Service{adapter: adapter}
}

func (s *Service) CurrentOccupancy(ctx context.Context, filter domain.OccupancyFilter) (domain.OccupancySnapshot, error) {
	events, err := s.adapter.ListEvents(ctx)
	if err != nil {
		return domain.OccupancySnapshot{}, err
	}

	snapshot := domain.OccupancySnapshot{
		FacilityID: filter.FacilityID,
		ZoneID:     filter.ZoneID,
		ObservedAt: time.Now().UTC(),
	}

	for _, event := range events {
		if filter.FacilityID != "" && event.FacilityID != filter.FacilityID {
			continue
		}
		if filter.ZoneID != "" && event.ZoneID != filter.ZoneID {
			continue
		}

		if snapshot.FacilityID == "" {
			snapshot.FacilityID = event.FacilityID
		}
		if snapshot.ZoneID == "" {
			snapshot.ZoneID = event.ZoneID
		}
		if event.RecordedAt.After(snapshot.ObservedAt) {
			snapshot.ObservedAt = event.RecordedAt
		}

		snapshot.CurrentCount += event.Delta()
	}

	if snapshot.CurrentCount < 0 {
		snapshot.CurrentCount = 0
	}

	return snapshot, nil
}

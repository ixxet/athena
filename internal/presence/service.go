package presence

import (
	"context"
	"time"

	"github.com/ixxet/athena/internal/adapter"
	"github.com/ixxet/athena/internal/domain"
)

type Clock func() time.Time

type Service struct {
	adapter adapter.PresenceAdapter
	now     Clock
}

type Option func(*Service)

func WithClock(now Clock) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

func NewService(adapter adapter.PresenceAdapter, opts ...Option) *Service {
	service := &Service{
		adapter: adapter,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}

	for _, opt := range opts {
		opt(service)
	}

	return service
}

func (s *Service) CurrentPresenceState(ctx context.Context, filter domain.OccupancyFilter) (domain.PresenceState, error) {
	events, err := s.adapter.ListEvents(ctx)
	if err != nil {
		return domain.PresenceState{}, err
	}

	return BuildPresenceState(events, filter, s.now()), nil
}

func (s *Service) CurrentOccupancy(ctx context.Context, filter domain.OccupancyFilter) (domain.OccupancyState, error) {
	presenceState, err := s.CurrentPresenceState(ctx, filter)
	if err != nil {
		return domain.OccupancyState{}, err
	}

	return presenceState.Occupancy(), nil
}

func BuildPresenceState(events []domain.PresenceEvent, filter domain.OccupancyFilter, observedAt time.Time) domain.PresenceState {
	state := domain.PresenceState{
		FacilityID: filter.FacilityID,
		ZoneID:     filter.ZoneID,
	}

	for _, event := range events {
		if filter.FacilityID != "" && event.FacilityID != filter.FacilityID {
			continue
		}
		if filter.ZoneID != "" && event.ZoneID != filter.ZoneID {
			continue
		}

		if state.FacilityID == "" {
			state.FacilityID = event.FacilityID
		}
		if state.ZoneID == "" {
			state.ZoneID = event.ZoneID
		}
		if event.RecordedAt.After(state.ObservedAt) {
			state.ObservedAt = event.RecordedAt
		}

		switch event.Direction {
		case domain.DirectionIn:
			state.Arrivals++
		case domain.DirectionOut:
			state.Departures++
		}
	}

	if state.ObservedAt.IsZero() {
		state.ObservedAt = observedAt.UTC()
	}

	return state
}

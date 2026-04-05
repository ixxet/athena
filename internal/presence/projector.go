package presence

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ixxet/athena/internal/domain"
)

type ProjectionResult struct {
	Applied bool
	Reason  string
}

type Projector struct {
	mu         sync.RWMutex
	now        Clock
	identities map[identityKey]identityState
	aggregates map[aggregateKey]aggregateState
}

type identityKey struct {
	FacilityID           string
	ZoneID               string
	ExternalIdentityHash string
}

type aggregateKey struct {
	FacilityID string
	ZoneID     string
}

type identityState struct {
	Present        bool
	LastRecordedAt time.Time
	LastEventID    string
	LastDirection  domain.PresenceDirection
}

type aggregateState struct {
	CurrentCount int
	ObservedAt   time.Time
}

func NewProjector() *Projector {
	return NewProjectorWithClock(func() time.Time {
		return time.Now().UTC()
	})
}

func NewProjectorWithClock(now Clock) *Projector {
	projector := &Projector{
		now: func() time.Time {
			return time.Now().UTC()
		},
		identities: make(map[identityKey]identityState),
		aggregates: make(map[aggregateKey]aggregateState),
	}
	if now != nil {
		projector.now = now
	}

	return projector
}

func (p *Projector) Apply(event domain.PresenceEvent) (ProjectionResult, error) {
	return p.ApplyWithEffect(event, nil)
}

func (p *Projector) ApplyWithEffect(event domain.PresenceEvent, onApply func() error) (ProjectionResult, error) {
	if event.ID == "" {
		return ProjectionResult{}, fmt.Errorf("event id is required")
	}
	if event.FacilityID == "" {
		return ProjectionResult{}, fmt.Errorf("facility_id is required")
	}
	if event.ExternalIdentityHash == "" {
		return ProjectionResult{}, fmt.Errorf("external_identity_hash is required")
	}
	if event.RecordedAt.IsZero() {
		return ProjectionResult{}, fmt.Errorf("recorded_at is required")
	}
	if event.Direction != domain.DirectionIn && event.Direction != domain.DirectionOut {
		return ProjectionResult{}, fmt.Errorf("unsupported direction %q", event.Direction)
	}

	recordedAt := event.RecordedAt.UTC()
	key := identityKey{
		FacilityID:           event.FacilityID,
		ZoneID:               event.ZoneID,
		ExternalIdentityHash: event.ExternalIdentityHash,
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	current, exists := p.identities[key]
	switch compareEventOrder(recordedAt, event.ID, current.LastRecordedAt.UTC(), current.LastEventID) {
	case -1:
		return ProjectionResult{Applied: false, Reason: "stale"}, nil
	case 0:
		return ProjectionResult{Applied: false, Reason: "duplicate"}, nil
	}

	switch event.Direction {
	case domain.DirectionIn:
		if exists && current.Present {
			current.LastRecordedAt = recordedAt
			current.LastEventID = event.ID
			current.LastDirection = event.Direction
			p.identities[key] = current
			p.touchAggregate(event.FacilityID, event.ZoneID, recordedAt)
			return ProjectionResult{Applied: false, Reason: "already_present"}, nil
		}
		if onApply != nil {
			if err := onApply(); err != nil {
				return ProjectionResult{}, err
			}
		}

		current.Present = true
		current.LastRecordedAt = recordedAt
		current.LastEventID = event.ID
		current.LastDirection = event.Direction
		p.identities[key] = current
		p.adjustAggregate(event.FacilityID, event.ZoneID, 1, recordedAt)
		return ProjectionResult{Applied: true, Reason: "entered"}, nil
	case domain.DirectionOut:
		if !exists || !current.Present {
			current.Present = false
			current.LastRecordedAt = recordedAt
			current.LastEventID = event.ID
			current.LastDirection = event.Direction
			p.identities[key] = current
			p.touchAggregate(event.FacilityID, event.ZoneID, recordedAt)
			return ProjectionResult{Applied: false, Reason: "already_absent"}, nil
		}
		if onApply != nil {
			if err := onApply(); err != nil {
				return ProjectionResult{}, err
			}
		}

		current.Present = false
		current.LastRecordedAt = recordedAt
		current.LastEventID = event.ID
		current.LastDirection = event.Direction
		p.identities[key] = current
		p.adjustAggregate(event.FacilityID, event.ZoneID, -1, recordedAt)
		return ProjectionResult{Applied: true, Reason: "exited"}, nil
	default:
		return ProjectionResult{}, fmt.Errorf("unsupported direction %q", event.Direction)
	}
}

func (p *Projector) CurrentOccupancy(_ context.Context, filter domain.OccupancyFilter) (domain.OccupancyState, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	key := aggregateKey{
		FacilityID: filter.FacilityID,
		ZoneID:     filter.ZoneID,
	}
	state, ok := p.aggregates[key]
	if !ok {
		return domain.OccupancyState{
			FacilityID:   filter.FacilityID,
			ZoneID:       filter.ZoneID,
			CurrentCount: 0,
			ObservedAt:   p.now().UTC(),
		}, nil
	}

	return domain.OccupancyState{
		FacilityID:   filter.FacilityID,
		ZoneID:       filter.ZoneID,
		CurrentCount: state.CurrentCount,
		ObservedAt:   state.ObservedAt.UTC(),
	}, nil
}

func (p *Projector) adjustAggregate(facilityID, zoneID string, delta int, observedAt time.Time) {
	p.updateAggregate(aggregateKey{FacilityID: facilityID}, delta, observedAt)
	if zoneID != "" {
		p.updateAggregate(aggregateKey{FacilityID: facilityID, ZoneID: zoneID}, delta, observedAt)
	}
}

func (p *Projector) touchAggregate(facilityID, zoneID string, observedAt time.Time) {
	p.updateAggregate(aggregateKey{FacilityID: facilityID}, 0, observedAt)
	if zoneID != "" {
		p.updateAggregate(aggregateKey{FacilityID: facilityID, ZoneID: zoneID}, 0, observedAt)
	}
}

func (p *Projector) updateAggregate(key aggregateKey, delta int, observedAt time.Time) {
	state := p.aggregates[key]
	state.CurrentCount += delta
	if state.CurrentCount < 0 {
		state.CurrentCount = 0
	}
	if observedAt.After(state.ObservedAt) {
		state.ObservedAt = observedAt
	}
	p.aggregates[key] = state
}

func compareEventOrder(leftAt time.Time, leftID string, rightAt time.Time, rightID string) int {
	if rightAt.IsZero() && rightID == "" {
		return 1
	}
	if leftAt.Before(rightAt) {
		return -1
	}
	if leftAt.After(rightAt) {
		return 1
	}
	switch {
	case leftID < rightID:
		return -1
	case leftID > rightID:
		return 1
	default:
		return 0
	}
}

package presence

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ixxet/athena/internal/domain"
)

const (
	DefaultAbsentIdentityRetention = 24 * time.Hour
	DefaultMaxAbsentIdentities     = 100000
)

type ProjectionResult struct {
	Applied bool
	Reason  string
}

type ProjectorOption func(*Projector)

type Projector struct {
	mu                  sync.RWMutex
	now                 Clock
	absentRetention     time.Duration
	maxAbsentIdentities int
	identities          map[identityKey]identityState
	aggregates          map[aggregateKey]aggregateState
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

type prunableIdentity struct {
	key   identityKey
	state identityState
}

func WithAbsentIdentityRetention(retention time.Duration) ProjectorOption {
	return func(projector *Projector) {
		projector.absentRetention = retention
	}
}

func WithMaxAbsentIdentities(limit int) ProjectorOption {
	return func(projector *Projector) {
		projector.maxAbsentIdentities = limit
	}
}

func NewProjector(opts ...ProjectorOption) *Projector {
	return NewProjectorWithClock(nil, opts...)
}

func NewProjectorWithClock(now Clock, opts ...ProjectorOption) *Projector {
	if now == nil {
		now = func() time.Time {
			return time.Now().UTC()
		}
	}

	projector := &Projector{
		now:                 now,
		absentRetention:     DefaultAbsentIdentityRetention,
		maxAbsentIdentities: DefaultMaxAbsentIdentities,
		identities:          make(map[identityKey]identityState),
		aggregates:          make(map[aggregateKey]aggregateState),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(projector)
		}
	}
	if projector.absentRetention <= 0 {
		projector.absentRetention = DefaultAbsentIdentityRetention
	}
	if projector.maxAbsentIdentities <= 0 {
		projector.maxAbsentIdentities = DefaultMaxAbsentIdentities
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
			p.pruneAbsentIdentities(recordedAt)
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
		p.pruneAbsentIdentities(recordedAt)
		return ProjectionResult{Applied: true, Reason: "entered"}, nil
	case domain.DirectionOut:
		if !exists || !current.Present {
			current.Present = false
			current.LastRecordedAt = recordedAt
			current.LastEventID = event.ID
			current.LastDirection = event.Direction
			p.identities[key] = current
			p.touchAggregate(event.FacilityID, event.ZoneID, recordedAt)
			p.pruneAbsentIdentities(recordedAt)
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
		p.pruneAbsentIdentities(recordedAt)
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

func (p *Projector) pruneAbsentIdentities(recordedAt time.Time) {
	cutoff := recordedAt.Add(-p.absentRetention)
	absent := make([]prunableIdentity, 0)

	for key, state := range p.identities {
		if state.Present {
			continue
		}
		if state.LastRecordedAt.Before(cutoff) {
			delete(p.identities, key)
			continue
		}
		absent = append(absent, prunableIdentity{key: key, state: state})
	}

	if len(absent) <= p.maxAbsentIdentities {
		return
	}

	sort.Slice(absent, func(i, j int) bool {
		if !absent[i].state.LastRecordedAt.Equal(absent[j].state.LastRecordedAt) {
			return absent[i].state.LastRecordedAt.Before(absent[j].state.LastRecordedAt)
		}
		if absent[i].state.LastEventID != absent[j].state.LastEventID {
			return absent[i].state.LastEventID < absent[j].state.LastEventID
		}
		return compareIdentityKey(absent[i].key, absent[j].key) < 0
	})

	for _, candidate := range absent[:len(absent)-p.maxAbsentIdentities] {
		delete(p.identities, candidate.key)
	}
}

func compareIdentityKey(left, right identityKey) int {
	switch {
	case left.FacilityID < right.FacilityID:
		return -1
	case left.FacilityID > right.FacilityID:
		return 1
	case left.ZoneID < right.ZoneID:
		return -1
	case left.ZoneID > right.ZoneID:
		return 1
	case left.ExternalIdentityHash < right.ExternalIdentityHash:
		return -1
	case left.ExternalIdentityHash > right.ExternalIdentityHash:
		return 1
	default:
		return 0
	}
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

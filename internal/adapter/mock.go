package adapter

import (
	"context"
	"fmt"
	"time"

	"github.com/ixxet/athena/internal/domain"
)

type MockConfig struct {
	FacilityID          string
	ZoneID              string
	Entries             int
	Exits               int
	BaseTime            time.Time
	IdentifiedTagHashes []string
	Events              []domain.PresenceEvent
}

type MockAdapter struct {
	events []domain.PresenceEvent
}

func NewMockAdapter(cfg MockConfig) *MockAdapter {
	return &MockAdapter{
		events: buildMockEvents(cfg),
	}
}

func (m *MockAdapter) Name() string {
	return "mock"
}

func (m *MockAdapter) ListEvents(_ context.Context) ([]domain.PresenceEvent, error) {
	out := make([]domain.PresenceEvent, len(m.events))
	copy(out, m.events)

	return out, nil
}

func buildMockEvents(cfg MockConfig) []domain.PresenceEvent {
	if len(cfg.Events) > 0 {
		out := make([]domain.PresenceEvent, len(cfg.Events))
		copy(out, cfg.Events)
		return out
	}

	total := cfg.Entries + cfg.Exits
	events := make([]domain.PresenceEvent, 0, total)
	if total == 0 {
		return events
	}

	start := cfg.BaseTime.UTC()
	if start.IsZero() {
		start = time.Now().UTC()
	}
	start = start.Add(-15 * time.Minute)

	for i := 0; i < cfg.Entries; i++ {
		externalIdentityHash := ""
		if i < len(cfg.IdentifiedTagHashes) {
			externalIdentityHash = cfg.IdentifiedTagHashes[i]
		}

		events = append(events, domain.PresenceEvent{
			ID:                   fmt.Sprintf("mock-in-%03d", i+1),
			FacilityID:           cfg.FacilityID,
			ZoneID:               cfg.ZoneID,
			ExternalIdentityHash: externalIdentityHash,
			Direction:            domain.DirectionIn,
			Source:               domain.SourceMock,
			RecordedAt:           start.Add(time.Duration(i) * time.Minute),
			Metadata: map[string]string{
				"seed": "mock",
			},
		})
	}

	for i := 0; i < cfg.Exits; i++ {
		events = append(events, domain.PresenceEvent{
			ID:         fmt.Sprintf("mock-out-%03d", i+1),
			FacilityID: cfg.FacilityID,
			ZoneID:     cfg.ZoneID,
			Direction:  domain.DirectionOut,
			Source:     domain.SourceMock,
			RecordedAt: start.Add(time.Duration(cfg.Entries+i) * time.Minute),
			Metadata: map[string]string{
				"seed": "mock",
			},
		})
	}

	return events
}

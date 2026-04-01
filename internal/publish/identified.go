package publish

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/ixxet/athena/internal/adapter"
	"github.com/ixxet/athena/internal/domain"
)

const SubjectIdentifiedPresenceArrived = "athena.identified_presence.arrived"

type Publisher interface {
	Publish(ctx context.Context, subject string, payload []byte) error
}

type Message struct {
	ID      string
	Subject string
	Payload []byte
}

type Service struct {
	adapter   adapter.PresenceAdapter
	publisher Publisher
}

type envelope struct {
	ID            string                 `json:"id"`
	Source        string                 `json:"source"`
	Type          string                 `json:"type"`
	Timestamp     string                 `json:"timestamp"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	Data          identifiedPresenceData `json:"data"`
}

type identifiedPresenceData struct {
	FacilityID           string `json:"facility_id"`
	ZoneID               string `json:"zone_id,omitempty"`
	ExternalIdentityHash string `json:"external_identity_hash"`
	Source               string `json:"source"`
	RecordedAt           string `json:"recorded_at"`
}

type NATSPublisher struct {
	conn *nats.Conn
}

func NewService(adapter adapter.PresenceAdapter, publisher Publisher) *Service {
	return &Service{
		adapter:   adapter,
		publisher: publisher,
	}
}

func NewNATSPublisher(conn *nats.Conn) *NATSPublisher {
	return &NATSPublisher{conn: conn}
}

func (p *NATSPublisher) Publish(ctx context.Context, subject string, payload []byte) error {
	if err := p.conn.Publish(subject, payload); err != nil {
		return err
	}

	return p.conn.FlushWithContext(ctx)
}

func (s *Service) BuildBatch(ctx context.Context) ([]Message, error) {
	events, err := s.adapter.ListEvents(ctx)
	if err != nil {
		return nil, err
	}

	return BuildBatch(events)
}

func (s *Service) Publish(ctx context.Context) (int, error) {
	batch, err := s.BuildBatch(ctx)
	if err != nil {
		return 0, err
	}

	return PublishBatch(ctx, s.publisher, batch)
}

func BuildBatch(events []domain.PresenceEvent) ([]Message, error) {
	batch := make([]Message, 0, len(events))
	for _, event := range events {
		message, include, err := buildMessage(event)
		if err != nil {
			return nil, err
		}
		if !include {
			continue
		}

		batch = append(batch, message)
	}

	return batch, nil
}

func PublishBatch(ctx context.Context, publisher Publisher, batch []Message) (int, error) {
	published := 0
	for _, message := range batch {
		if err := publisher.Publish(ctx, message.Subject, message.Payload); err != nil {
			return published, err
		}
		published++
	}

	return published, nil
}

func buildMessage(event domain.PresenceEvent) (Message, bool, error) {
	if event.Direction != domain.DirectionIn {
		return Message{}, false, nil
	}

	if strings.TrimSpace(event.ExternalIdentityHash) == "" {
		return Message{}, false, nil
	}

	if err := validateIdentifiedArrival(event); err != nil {
		return Message{}, false, err
	}

	recordedAt := event.RecordedAt.UTC().Format(time.RFC3339Nano)
	payload, err := json.Marshal(envelope{
		ID:        event.ID,
		Source:    "athena",
		Type:      SubjectIdentifiedPresenceArrived,
		Timestamp: recordedAt,
		Data: identifiedPresenceData{
			FacilityID:           event.FacilityID,
			ZoneID:               event.ZoneID,
			ExternalIdentityHash: event.ExternalIdentityHash,
			Source:               string(event.Source),
			RecordedAt:           recordedAt,
		},
	})
	if err != nil {
		return Message{}, false, err
	}

	return Message{
		ID:      event.ID,
		Subject: SubjectIdentifiedPresenceArrived,
		Payload: payload,
	}, true, nil
}

func validateIdentifiedArrival(event domain.PresenceEvent) error {
	if strings.TrimSpace(event.ID) == "" {
		return fmt.Errorf("identified arrival missing id")
	}
	if strings.TrimSpace(event.FacilityID) == "" {
		return fmt.Errorf("identified arrival %q missing facility_id", event.ID)
	}
	if strings.TrimSpace(event.ExternalIdentityHash) == "" {
		return fmt.Errorf("identified arrival %q missing external_identity_hash", event.ID)
	}
	if strings.TrimSpace(string(event.Source)) == "" {
		return fmt.Errorf("identified arrival %q missing source", event.ID)
	}
	if event.RecordedAt.IsZero() {
		return fmt.Errorf("identified arrival %q missing recorded_at", event.ID)
	}

	return nil
}

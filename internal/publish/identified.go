package publish

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	protoevents "github.com/ixxet/ashton-proto/events"
	athenav1 "github.com/ixxet/ashton-proto/gen/go/ashton/athena/v1"
	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ixxet/athena/internal/adapter"
	"github.com/ixxet/athena/internal/domain"
)

type Publisher interface {
	Publish(ctx context.Context, subject string, payload []byte) error
}

const publishFlushTimeout = 5 * time.Second

type Message struct {
	ID      string
	Subject string
	Payload []byte
}

type Service struct {
	adapter   adapter.PresenceAdapter
	publisher Publisher
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

	flushCtx, cancel := flushContext(ctx, publishFlushTimeout)
	defer cancel()

	return p.conn.FlushWithContext(flushCtx)
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
			slog.Warn("identified arrival rejected", "event_id", event.ID, "error", err)
			return nil, fmt.Errorf("build identified arrival %q: %w", event.ID, err)
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
			slog.Error("identified arrival publish failed", "event_id", message.ID, "subject", message.Subject, "error", err)
			return published, fmt.Errorf("publish identified arrival %q on %s: %w", message.ID, message.Subject, err)
		}
		slog.Info("identified arrival published", "event_id", message.ID, "subject", message.Subject)
		published++
	}

	return published, nil
}

func buildMessage(event domain.PresenceEvent) (Message, bool, error) {
	if event.Direction != domain.DirectionIn {
		slog.Debug("identified arrival skipped", "event_id", event.ID, "reason", "non_arrival")
		return Message{}, false, nil
	}

	if strings.TrimSpace(event.ExternalIdentityHash) == "" {
		slog.Debug("identified arrival skipped", "event_id", event.ID, "reason", "anonymous")
		return Message{}, false, nil
	}

	if err := validateIdentifiedArrival(event); err != nil {
		return Message{}, false, err
	}

	source, err := toProtoPresenceSource(event.Source)
	if err != nil {
		return Message{}, false, fmt.Errorf("identified arrival %q source: %w", event.ID, err)
	}

	payload, err := protoevents.MarshalIdentifiedPresenceArrived(protoevents.IdentifiedPresenceArrivedEvent{
		ID:        event.ID,
		Timestamp: event.RecordedAt.UTC(),
		Data: &athenav1.IdentifiedPresenceArrived{
			FacilityId:           event.FacilityID,
			ZoneId:               event.ZoneID,
			ExternalIdentityHash: event.ExternalIdentityHash,
			Source:               source,
			RecordedAt:           timestamppb.New(event.RecordedAt.UTC()),
		},
	})
	if err != nil {
		return Message{}, false, err
	}

	return Message{
		ID:      event.ID,
		Subject: protoevents.SubjectIdentifiedPresenceArrived,
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
	if event.RecordedAt.IsZero() {
		return fmt.Errorf("identified arrival %q missing recorded_at", event.ID)
	}
	if _, err := toProtoPresenceSource(event.Source); err != nil {
		return fmt.Errorf("identified arrival %q source: %w", event.ID, err)
	}

	return nil
}

func toProtoPresenceSource(source domain.PresenceSource) (athenav1.PresenceSource, error) {
	switch source {
	case domain.SourceMock:
		return athenav1.PresenceSource_PRESENCE_SOURCE_MOCK, nil
	case domain.PresenceSource("rfid"):
		return athenav1.PresenceSource_PRESENCE_SOURCE_RFID, nil
	case domain.PresenceSource("tof"):
		return athenav1.PresenceSource_PRESENCE_SOURCE_TOF, nil
	case domain.PresenceSource("database"):
		return athenav1.PresenceSource_PRESENCE_SOURCE_DATABASE, nil
	case domain.PresenceSource("csv"):
		return athenav1.PresenceSource_PRESENCE_SOURCE_CSV, nil
	default:
		return athenav1.PresenceSource_PRESENCE_SOURCE_UNSPECIFIED, fmt.Errorf("unsupported presence source %q", source)
	}
}

func flushContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, hasDeadline := parent.Deadline(); hasDeadline {
		return context.WithCancel(parent)
	}

	return context.WithTimeout(parent, timeout)
}

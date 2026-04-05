package edge

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ixxet/athena/internal/domain"
	"github.com/ixxet/athena/internal/presence"
	"github.com/ixxet/athena/internal/publish"
)

var (
	ErrMissingToken       = errors.New("missing edge token")
	ErrForbiddenToken     = errors.New("forbidden edge token")
	ErrPublishUnavailable = errors.New("edge publish unavailable")
)

type ValidationError struct {
	message string
}

type TapRequest struct {
	EventID       string `json:"event_id"`
	AccountRaw    string `json:"account_raw"`
	Direction     string `json:"direction"`
	FacilityID    string `json:"facility_id"`
	ZoneID        string `json:"zone_id,omitempty"`
	NodeID        string `json:"node_id"`
	ObservedAt    string `json:"observed_at"`
	Result        string `json:"result,omitempty"`
	AccountType   string `json:"account_type,omitempty"`
	Name          string `json:"name,omitempty"`
	StatusMessage string `json:"status_message,omitempty"`
}

type AcceptedTap struct {
	EventID   string `json:"event_id"`
	Subject   string `json:"subject,omitempty"`
	Status    string `json:"status"`
	Result    string `json:"result"`
	Direction string `json:"direction"`
	Published bool   `json:"published"`
}

type observedTap struct {
	event         domain.PresenceEvent
	result        string
	accountRaw    string
	accountType   string
	name          string
	statusMessage string
	nodeID        string
}

type Service struct {
	publisher publish.Publisher
	hashSalt  string
	tokens    map[string]string
	projector ProjectionApplier
}

type ProjectionApplier interface {
	Apply(domain.PresenceEvent) (presence.ProjectionResult, error)
	ApplyWithEffect(domain.PresenceEvent, func() error) (presence.ProjectionResult, error)
}

type Option func(*Service)

func WithProjection(projector ProjectionApplier) Option {
	return func(service *Service) {
		service.projector = projector
	}
}

func NewService(publisher publish.Publisher, hashSalt string, tokens map[string]string, opts ...Option) (*Service, error) {
	if publisher == nil {
		return nil, fmt.Errorf("edge publisher is required")
	}
	if strings.TrimSpace(hashSalt) == "" {
		return nil, fmt.Errorf("edge hash salt is required")
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("edge tokens are required")
	}

	copiedTokens := make(map[string]string, len(tokens))
	for nodeID, token := range tokens {
		copiedTokens[nodeID] = token
	}

	service := &Service{
		publisher: publisher,
		hashSalt:  hashSalt,
		tokens:    copiedTokens,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(service)
		}
	}

	return service, nil
}

func (s *Service) AcceptTap(ctx context.Context, token string, req TapRequest) (AcceptedTap, error) {
	observed, err := s.normalizeRequest(req)
	if err != nil {
		return AcceptedTap{}, err
	}
	if err := s.authorize(observed.nodeID, token); err != nil {
		return AcceptedTap{}, err
	}

	if observed.result != "pass" {
		s.logObservedTap("edge tap observed", observed, false, "")
		return AcceptedTap{
			EventID:   observed.event.ID,
			Status:    "observed",
			Result:    observed.result,
			Direction: string(observed.event.Direction),
			Published: false,
		}, nil
	}

	if s.projector != nil {
		message, include, err := publish.BuildMessage(observed.event)
		if err != nil {
			return AcceptedTap{}, &ValidationError{message: err.Error()}
		}
		if !include {
			return AcceptedTap{}, &ValidationError{message: "identified presence event is missing an external identity hash"}
		}

		projection, err := s.projector.ApplyWithEffect(observed.event, func() error {
			if err := s.publisher.Publish(ctx, message.Subject, message.Payload); err != nil {
				return fmt.Errorf("%w: %v", ErrPublishUnavailable, err)
			}
			return nil
		})
		if err != nil {
			slog.Error(
				"edge tap projection failed",
				"event_id", observed.event.ID,
				"node_id", observed.nodeID,
				"direction", observed.event.Direction,
				"result", observed.result,
				"account_raw", observed.accountRaw,
				"account_type", observed.accountType,
				"name", observed.name,
				"status_message", observed.statusMessage,
				"error", err,
			)
			if errors.Is(err, ErrPublishUnavailable) {
				return AcceptedTap{}, err
			}
			return AcceptedTap{}, fmt.Errorf("apply live occupancy projection: %w", err)
		}
		if !projection.Applied {
			s.logObservedTap("edge tap observed", observed, false, projection.Reason)
			return AcceptedTap{
				EventID:   observed.event.ID,
				Status:    "observed",
				Result:    observed.result,
				Direction: string(observed.event.Direction),
				Published: false,
			}, nil
		}
		s.logAcceptedTap(observed, message.Subject)

		return AcceptedTap{
			EventID:   observed.event.ID,
			Subject:   message.Subject,
			Status:    "accepted",
			Result:    observed.result,
			Direction: string(observed.event.Direction),
			Published: true,
		}, nil
	}

	message, include, err := publish.BuildMessage(observed.event)
	if err != nil {
		return AcceptedTap{}, &ValidationError{message: err.Error()}
	}
	if !include {
		return AcceptedTap{}, &ValidationError{message: "identified presence event is missing an external identity hash"}
	}

	if err := s.publisher.Publish(ctx, message.Subject, message.Payload); err != nil {
		slog.Error(
			"edge tap publish failed",
			"event_id", observed.event.ID,
			"node_id", observed.nodeID,
			"direction", observed.event.Direction,
			"result", observed.result,
			"account_raw", observed.accountRaw,
			"account_type", observed.accountType,
			"name", observed.name,
			"status_message", observed.statusMessage,
			"error", err,
		)
		return AcceptedTap{}, fmt.Errorf("%w: %v", ErrPublishUnavailable, err)
	}

	s.logAcceptedTap(observed, message.Subject)

	return AcceptedTap{
		EventID:   observed.event.ID,
		Subject:   message.Subject,
		Status:    "accepted",
		Result:    observed.result,
		Direction: string(observed.event.Direction),
		Published: true,
	}, nil
}

func (s *Service) normalizeRequest(req TapRequest) (observedTap, error) {
	eventID := strings.TrimSpace(req.EventID)
	if eventID == "" {
		return observedTap{}, &ValidationError{message: "event_id is required"}
	}

	accountRaw := strings.TrimSpace(req.AccountRaw)
	if accountRaw == "" {
		return observedTap{}, &ValidationError{message: "account_raw is required"}
	}

	direction, err := domain.ParseDirection(req.Direction)
	if err != nil {
		return observedTap{}, &ValidationError{message: err.Error()}
	}

	facilityID := strings.TrimSpace(req.FacilityID)
	if facilityID == "" {
		return observedTap{}, &ValidationError{message: "facility_id is required"}
	}

	nodeID := strings.TrimSpace(req.NodeID)
	if nodeID == "" {
		return observedTap{}, &ValidationError{message: "node_id is required"}
	}

	observedAtText := strings.TrimSpace(req.ObservedAt)
	if observedAtText == "" {
		return observedTap{}, &ValidationError{message: "observed_at is required"}
	}

	observedAt, err := time.Parse(time.RFC3339Nano, observedAtText)
	if err != nil {
		return observedTap{}, &ValidationError{message: fmt.Sprintf("observed_at %q: %v", observedAtText, err)}
	}

	result := strings.ToLower(strings.TrimSpace(req.Result))
	if result == "" {
		result = "pass"
	}
	switch result {
	case "pass", "fail":
	default:
		return observedTap{}, &ValidationError{message: fmt.Sprintf("result %q must be one of pass,fail", req.Result)}
	}

	return observedTap{
		event: domain.PresenceEvent{
			ID:                   eventID,
			FacilityID:           facilityID,
			ZoneID:               strings.TrimSpace(req.ZoneID),
			ExternalIdentityHash: HashAccount(accountRaw, s.hashSalt),
			Direction:            direction,
			Source:               domain.SourceRFID,
			RecordedAt:           observedAt.UTC(),
			Metadata: map[string]string{
				"node_id":        nodeID,
				"result":         result,
				"account_type":   strings.TrimSpace(req.AccountType),
				"name":           strings.TrimSpace(req.Name),
				"status_message": strings.TrimSpace(req.StatusMessage),
			},
		},
		result:        result,
		accountRaw:    accountRaw,
		accountType:   strings.TrimSpace(req.AccountType),
		name:          strings.TrimSpace(req.Name),
		statusMessage: strings.TrimSpace(req.StatusMessage),
		nodeID:        nodeID,
	}, nil
}

func (s *Service) logObservedTap(message string, observed observedTap, published bool, projectionReason string) {
	args := []any{
		"event_id", observed.event.ID,
		"node_id", observed.nodeID,
		"direction", observed.event.Direction,
		"result", observed.result,
		"published", published,
		"account_raw", observed.accountRaw,
		"account_type", observed.accountType,
		"name", observed.name,
		"status_message", observed.statusMessage,
	}
	if projectionReason != "" {
		args = append(args, "projection_reason", projectionReason)
	}
	slog.Info(message, args...)
}

func (s *Service) logAcceptedTap(observed observedTap, subject string) {
	slog.Info(
		"edge tap accepted",
		"event_id", observed.event.ID,
		"node_id", observed.nodeID,
		"direction", observed.event.Direction,
		"result", observed.result,
		"published", true,
		"subject", subject,
		"account_raw", observed.accountRaw,
		"account_type", observed.accountType,
		"name", observed.name,
		"status_message", observed.statusMessage,
	)
}

func (s *Service) authorize(nodeID, token string) error {
	if strings.TrimSpace(token) == "" {
		return ErrMissingToken
	}

	expectedToken, ok := s.tokens[nodeID]
	if !ok || !constantTimeEquals(expectedToken, token) {
		return ErrForbiddenToken
	}

	return nil
}

func (e *ValidationError) Error() string {
	return e.message
}

func IsValidationError(err error) bool {
	var target *ValidationError
	return errors.As(err, &target)
}

func HashAccount(accountRaw, hashSalt string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(hashSalt) + ":" + strings.TrimSpace(accountRaw)))
	return hex.EncodeToString(sum[:])
}

func DeriveEventID(nodeID string, direction domain.PresenceDirection, accountRaw string, observedAt time.Time) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(nodeID),
		string(direction),
		strings.TrimSpace(accountRaw),
		observedAt.UTC().Format(time.RFC3339Nano),
	}, "|")))
	return "edge-" + hex.EncodeToString(sum[:16])
}

func constantTimeEquals(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

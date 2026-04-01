package adapter

import (
	"context"

	"github.com/ixxet/athena/internal/domain"
)

type PresenceAdapter interface {
	Name() string
	ListEvents(ctx context.Context) ([]domain.PresenceEvent, error)
}

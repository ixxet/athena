package publish

import (
	"context"
	"time"
)

type Worker struct {
	service  *Service
	interval time.Duration
	seen     map[string]struct{}
}

func NewWorker(service *Service, interval time.Duration) *Worker {
	return &Worker{
		service:  service,
		interval: interval,
		seen:     make(map[string]struct{}),
	}
}

func (w *Worker) Run(ctx context.Context) error {
	if _, err := w.RunOnce(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := w.RunOnce(ctx); err != nil {
				return err
			}
		}
	}
}

func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	batch, err := w.service.BuildBatch(ctx)
	if err != nil {
		return 0, err
	}

	pending := make([]Message, 0, len(batch))
	for _, message := range batch {
		if _, ok := w.seen[message.ID]; ok {
			continue
		}
		pending = append(pending, message)
	}

	published, err := PublishBatch(ctx, w.service.publisher, pending)
	for _, message := range pending[:published] {
		w.seen[message.ID] = struct{}{}
	}

	return published, err
}

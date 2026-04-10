package publish

import (
	"context"
	"log/slog"
	"time"
)

const (
	defaultSeenLimit           = 4096
	defaultMaxPublishAttempts  = 3
	defaultInitialRetryBackoff = 250 * time.Millisecond
	defaultMaxRetryBackoff     = 2 * time.Second
)

type WorkerOption func(*Worker)

type Worker struct {
	service             *Service
	interval            time.Duration
	seen                map[string]struct{}
	seenOrder           []string
	seenLimit           int
	maxPublishAttempts  int
	initialRetryBackoff time.Duration
	maxRetryBackoff     time.Duration
	sleep               func(context.Context, time.Duration) error
}

func WithSeenLimit(limit int) WorkerOption {
	return func(worker *Worker) {
		if limit > 0 {
			worker.seenLimit = limit
		}
	}
}

func WithRetryPolicy(maxAttempts int, initialBackoff, maxBackoff time.Duration) WorkerOption {
	return func(worker *Worker) {
		if maxAttempts > 0 {
			worker.maxPublishAttempts = maxAttempts
		}
		if initialBackoff > 0 {
			worker.initialRetryBackoff = initialBackoff
		}
		if maxBackoff > 0 {
			worker.maxRetryBackoff = maxBackoff
		}
	}
}

func WithSleep(sleeper func(context.Context, time.Duration) error) WorkerOption {
	return func(worker *Worker) {
		if sleeper != nil {
			worker.sleep = sleeper
		}
	}
}

func NewWorker(service *Service, interval time.Duration, opts ...WorkerOption) *Worker {
	worker := &Worker{
		service:             service,
		interval:            interval,
		seen:                make(map[string]struct{}),
		seenLimit:           defaultSeenLimit,
		maxPublishAttempts:  defaultMaxPublishAttempts,
		initialRetryBackoff: defaultInitialRetryBackoff,
		maxRetryBackoff:     defaultMaxRetryBackoff,
		sleep: func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(worker)
		}
	}

	return worker
}

func (w *Worker) Run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			if _, err := w.RunOnce(ctx); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				slog.Error("identified presence publish cycle failed", "error", err)
			}
			timer.Reset(w.interval)
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

	totalPublished := 0
	remaining := pending
	for attempt := 1; len(remaining) > 0; attempt++ {
		published, err := PublishBatch(ctx, w.service.publisher, remaining)
		for _, message := range remaining[:published] {
			w.rememberSeen(message.ID)
		}
		totalPublished += published
		if err == nil {
			return totalPublished, nil
		}

		remaining = remaining[published:]
		if attempt >= w.maxPublishAttempts {
			return totalPublished, err
		}

		backoff := w.retryBackoff(attempt)
		slog.Warn(
			"identified presence publish retry scheduled",
			"attempt", attempt+1,
			"remaining", len(remaining),
			"backoff", backoff.String(),
			"error", err,
		)
		if err := w.sleep(ctx, backoff); err != nil {
			if ctx.Err() != nil {
				return totalPublished, nil
			}
			return totalPublished, err
		}
	}

	return totalPublished, nil
}

func (w *Worker) retryBackoff(attempt int) time.Duration {
	backoff := w.initialRetryBackoff << max(attempt-1, 0)
	if backoff > w.maxRetryBackoff {
		return w.maxRetryBackoff
	}

	return backoff
}

func (w *Worker) rememberSeen(messageID string) {
	if _, ok := w.seen[messageID]; ok {
		return
	}

	w.seen[messageID] = struct{}{}
	w.seenOrder = append(w.seenOrder, messageID)

	for w.seenLimit > 0 && len(w.seenOrder) > w.seenLimit {
		evicted := w.seenOrder[0]
		w.seenOrder = w.seenOrder[1:]
		delete(w.seen, evicted)
	}
}

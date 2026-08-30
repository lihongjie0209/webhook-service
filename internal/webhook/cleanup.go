package webhook

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type RetentionStore interface {
	DeleteTerminalDeliveriesBefore(context.Context, time.Time, int) (int64, error)
}

type RetentionCleaner struct {
	store     RetentionStore
	logger    *slog.Logger
	retention time.Duration
	interval  time.Duration
	batchSize int
	now       func() time.Time
}

func NewRetentionCleaner(store RetentionStore, logger *slog.Logger, retention, interval time.Duration, batchSize int) (*RetentionCleaner, error) {
	if store == nil || logger == nil || retention <= 0 || interval <= 0 || batchSize <= 0 {
		return nil, errors.New("webhook retention cleaner dependencies and limits are required")
	}
	return &RetentionCleaner{store: store, logger: logger, retention: retention, interval: interval, batchSize: batchSize, now: time.Now}, nil
}

func (c *RetentionCleaner) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		if err := c.clean(ctx); err != nil && !errors.Is(err, context.Canceled) {
			c.logger.ErrorContext(ctx, "clean expired webhook deliveries", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *RetentionCleaner) clean(ctx context.Context) error {
	for {
		deleted, err := c.store.DeleteTerminalDeliveriesBefore(ctx, c.now().Add(-c.retention), c.batchSize)
		if err != nil {
			return err
		}
		if deleted > 0 {
			c.logger.InfoContext(ctx, "deleted expired webhook deliveries", "count", deleted)
		}
		if deleted < int64(c.batchSize) {
			return nil
		}
	}
}

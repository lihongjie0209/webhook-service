package webhook

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type retentionStoreStub struct {
	counts []int64
	before []time.Time
}

func (s *retentionStoreStub) DeleteTerminalDeliveriesBefore(_ context.Context, before time.Time, _ int) (int64, error) {
	s.before = append(s.before, before)
	count := s.counts[0]
	s.counts = s.counts[1:]
	return count, nil
}

func TestRetentionCleanerDeletesInBoundedBatches(t *testing.T) {
	t.Parallel()
	store := &retentionStoreStub{counts: []int64{2, 1}}
	cleaner, err := NewRetentionCleaner(store, slog.New(slog.NewTextHandler(io.Discard, nil)), 24*time.Hour, time.Hour, 2)
	require.NoError(t, err)
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	cleaner.now = func() time.Time { return now }

	require.NoError(t, cleaner.clean(context.Background()))
	require.Len(t, store.before, 2)
	require.Equal(t, now.Add(-24*time.Hour), store.before[0])
}

func TestNewRetentionCleanerRejectsInvalidLimits(t *testing.T) {
	t.Parallel()
	_, err := NewRetentionCleaner(&retentionStoreStub{}, slog.Default(), 0, time.Hour, 1)
	require.Error(t, err)
}

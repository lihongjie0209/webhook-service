package webhook

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"
)

type DeliveryStore interface {
	ClaimDeliveries(context.Context, int, time.Duration, string) ([]Delivery, error)
	GetSubscription(context.Context, string, string) (Subscription, error)
	CompleteDelivery(context.Context, Delivery, SendResult, string) error
	FailDelivery(context.Context, Delivery, SendResult, error, *time.Time, string) error
}

type Dispatcher struct {
	store        DeliveryStore
	sender       *Sender
	box          *SecretBox
	logger       *slog.Logger
	pollInterval time.Duration
	claimLease   time.Duration
	workers      int
	batchSize    int
	now          func() time.Time
}

func NewDispatcher(store DeliveryStore, sender *Sender, box *SecretBox, logger *slog.Logger, pollInterval, claimLease time.Duration, workers, batchSize int) (*Dispatcher, error) {
	if store == nil || sender == nil || box == nil || logger == nil || pollInterval <= 0 || claimLease <= 0 || workers <= 0 || batchSize <= 0 {
		return nil, errors.New("webhook dispatcher dependencies and limits are required")
	}
	return &Dispatcher{store: store, sender: sender, box: box, logger: logger, pollInterval: pollInterval, claimLease: claimLease, workers: workers, batchSize: batchSize, now: time.Now}, nil
}

func (d *Dispatcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()
	for {
		if err := d.dispatchBatch(ctx); err != nil && !errors.Is(err, context.Canceled) {
			d.logger.ErrorContext(ctx, "dispatch webhook batch", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) dispatchBatch(ctx context.Context) error {
	deliveries, err := d.store.ClaimDeliveries(ctx, d.batchSize, d.claimLease, "webhook-dispatcher")
	if err != nil {
		return err
	}
	jobs := make(chan Delivery)
	var workers sync.WaitGroup
	var failuresMu sync.Mutex
	var failures []error
	workerCount := min(d.workers, len(deliveries))
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for delivery := range jobs {
				if err := d.dispatchOne(ctx, delivery); err != nil {
					failuresMu.Lock()
					failures = append(failures, err)
					failuresMu.Unlock()
				}
			}
		}()
	}
	for _, delivery := range deliveries {
		select {
		case jobs <- delivery:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return errors.Join(ctx.Err(), errors.Join(failures...))
		}
	}
	close(jobs)
	workers.Wait()
	return errors.Join(failures...)
}

func (d *Dispatcher) dispatchOne(ctx context.Context, delivery Delivery) error {
	subscription, err := d.store.GetSubscription(ctx, delivery.TenantID, delivery.SubscriptionID)
	if err != nil {
		return d.recordFailure(ctx, delivery, Subscription{}, SendResult{}, fmt.Errorf("load webhook subscription: %w", err), false)
	}
	secret, err := d.box.Decrypt(subscription.SecretCiphertext, subscription.SecretKeyID)
	if err != nil {
		return d.recordFailure(ctx, delivery, subscription, SendResult{}, err, false)
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(subscription.TimeoutMS)*time.Millisecond)
	result, sendErr := d.sender.Send(requestCtx, SendRequest{
		EndpointURL: subscription.EndpointURL, DeliveryID: delivery.ID, EventType: delivery.EventSubject,
		Body: delivery.Payload, Secret: secret, Timestamp: d.now(),
	})
	cancel()
	if sendErr == nil && result.Successful() {
		if err := d.store.CompleteDelivery(ctx, delivery, result, "webhook-dispatcher"); err != nil {
			return fmt.Errorf("complete webhook delivery %q: %w", delivery.ID, err)
		}
		return nil
	}
	retryable := sendErr != nil || retryableStatus(result.StatusCode)
	return d.recordFailure(ctx, delivery, subscription, result, sendErr, retryable)
}

func (d *Dispatcher) recordFailure(ctx context.Context, delivery Delivery, subscription Subscription, result SendResult, cause error, retryable bool) error {
	var retryAt *time.Time
	maxAttempts := subscription.MaxAttempts
	if retryable && maxAttempts > 0 && delivery.AttemptCount < maxAttempts {
		next := d.now().Add(retryDelay(subscription.RetryInitialSeconds, delivery.AttemptCount))
		retryAt = &next
	}
	if cause == nil && !result.Successful() {
		cause = fmt.Errorf("webhook endpoint returned HTTP %d", result.StatusCode)
	}
	if err := d.store.FailDelivery(ctx, delivery, result, cause, retryAt, "webhook-dispatcher"); err != nil {
		return fmt.Errorf("record webhook delivery %q failure: %w", delivery.ID, err)
	}
	return nil
}

func retryableStatus(statusCode int) bool {
	return statusCode == http.StatusRequestTimeout || statusCode == http.StatusTooEarly || statusCode == http.StatusTooManyRequests || statusCode >= 500
}

func retryDelay(initialSeconds, attempt int) time.Duration {
	if initialSeconds < 1 {
		initialSeconds = 1
	}
	exponent := min(max(attempt-1, 0), 16)
	seconds := math.Min(float64(initialSeconds)*math.Pow(2, float64(exponent)), float64(24*time.Hour/time.Second))
	return time.Duration(seconds) * time.Second
}

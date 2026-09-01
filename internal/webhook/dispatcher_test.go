package webhook

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"testing"
	"time"
)

type fakeDeliveryStore struct {
	DeliveryStore
	subscription Subscription
	completed    bool
	failed       bool
	retryAt      *time.Time
}

func (f *fakeDeliveryStore) GetSubscription(context.Context, string, string, string) (Subscription, error) {
	return f.subscription, nil
}

func (f *fakeDeliveryStore) CompleteDelivery(context.Context, Delivery, SendResult, string) error {
	f.completed = true
	return nil
}

func (f *fakeDeliveryStore) FailDelivery(_ context.Context, _ Delivery, _ SendResult, _ error, retryAt *time.Time, _ string) error {
	f.failed = true
	f.retryAt = retryAt
	return nil
}

func TestDispatcherCompletesSuccessfulDelivery(t *testing.T) {
	dispatcher, store := newTestDispatcher(t, http.StatusNoContent)
	delivery := Delivery{ID: "delivery-1", SubscriptionID: "subscription-1", TenantID: "tenant-1", EventSubject: "platform.identity.user.created.v1", Payload: []byte(`{"id":"event-1"}`), AttemptCount: 1, Version: 2}
	if err := dispatcher.dispatchOne(t.Context(), delivery); err != nil {
		t.Fatal(err)
	}
	if !store.completed || store.failed {
		t.Fatalf("completed = %v, failed = %v", store.completed, store.failed)
	}
}

func TestDispatcherRetriesTransientFailureWithinAttemptBudget(t *testing.T) {
	dispatcher, store := newTestDispatcher(t, http.StatusServiceUnavailable)
	now := time.Unix(1_700_000_000, 0)
	dispatcher.now = func() time.Time { return now }
	delivery := Delivery{ID: "delivery-1", SubscriptionID: "subscription-1", TenantID: "tenant-1", EventSubject: "platform.identity.user.created.v1", Payload: []byte(`{}`), AttemptCount: 2, Version: 3}
	if err := dispatcher.dispatchOne(t.Context(), delivery); err != nil {
		t.Fatal(err)
	}
	if !store.failed || store.retryAt == nil || !store.retryAt.Equal(now.Add(10*time.Second)) {
		t.Fatalf("failed = %v, retryAt = %v", store.failed, store.retryAt)
	}
}

func TestDispatcherDoesNotRetryPermanentOrExhaustedFailure(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		attempts   int
	}{
		{name: "permanent client error", statusCode: http.StatusBadRequest, attempts: 1},
		{name: "attempts exhausted", statusCode: http.StatusServiceUnavailable, attempts: 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dispatcher, store := newTestDispatcher(t, test.statusCode)
			delivery := Delivery{ID: "delivery-1", SubscriptionID: "subscription-1", TenantID: "tenant-1", EventSubject: "platform.identity.user.created.v1", Payload: []byte(`{}`), AttemptCount: test.attempts, Version: 2}
			if err := dispatcher.dispatchOne(t.Context(), delivery); err != nil {
				t.Fatal(err)
			}
			if !store.failed || store.retryAt != nil {
				t.Fatalf("failed = %v, retryAt = %v", store.failed, store.retryAt)
			}
		})
	}
}

func newTestDispatcher(t *testing.T, statusCode int) (*Dispatcher, *fakeDeliveryStore) {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	box, err := NewSecretBox(key, "test-v1")
	if err != nil {
		t.Fatal(err)
	}
	box.rand = bytes.NewReader(bytes.Repeat([]byte{2}, 128))
	ciphertext, err := box.Encrypt([]byte("signing-secret"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewURLPolicy(staticResolver{addresses: map[string][]netip.Addr{"hooks.example.com": {netip.MustParseAddr("93.184.216.34")}}}, false, []int{443})
	if err != nil {
		t.Fatal(err)
	}
	sender, err := NewSender(policy, 1024)
	if err != nil {
		t.Fatal(err)
	}
	sender.transport = func(*url.URL, []netip.Addr) http.RoundTripper {
		return roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: statusCode, Body: io.NopCloser(bytes.NewBufferString("result")), Header: make(http.Header)}, nil
		})
	}
	store := &fakeDeliveryStore{subscription: Subscription{
		ID: "subscription-1", TenantID: "tenant-1", EndpointURL: "https://hooks.example.com/events",
		TimeoutMS: 1000, MaxAttempts: 5, RetryInitialSeconds: 5, SecretCiphertext: ciphertext, SecretKeyID: "test-v1",
	}}
	dispatcher, err := NewDispatcher(store, sender, box, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second, 30*time.Second, 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	return dispatcher, store
}

package webhook

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lihongjie0209/microservice-platform-go/principal"
	commonv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/common/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeStore struct {
	Store
	createFn     func(context.Context, Subscription) (Subscription, error)
	listActiveFn func(context.Context, *sqlx.Tx, string) ([]Subscription, error)
	insertFn     func(context.Context, *sqlx.Tx, Delivery) (bool, error)
}

func (f *fakeStore) CreateSubscription(ctx context.Context, value Subscription) (Subscription, error) {
	return f.createFn(ctx, value)
}

func (f *fakeStore) ListActiveSubscriptionsTx(ctx context.Context, tx *sqlx.Tx, tenantID string) ([]Subscription, error) {
	return f.listActiveFn(ctx, tx, tenantID)
}

func (f *fakeStore) InsertDeliveryTx(ctx context.Context, tx *sqlx.Tx, value Delivery) (bool, error) {
	return f.insertFn(ctx, tx, value)
}

func TestServiceCreateSubscriptionReturnsSecretOnce(t *testing.T) {
	var stored Subscription
	store := &fakeStore{createFn: func(_ context.Context, value Subscription) (Subscription, error) {
		stored = value
		return value, nil
	}}
	service := newTestService(t, store)
	service.newID = func() string { return "subscription-1" }
	service.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "admin-1", Type: principal.TypeUser, TenantID: "tenant-1"})

	created, secret, err := service.CreateSubscription(ctx, CreateSubscriptionInput{
		TenantID: "tenant-1", ApplicationID: "app-1", Name: "CRM events",
		EndpointURL: "https://hooks.example.com/events", SubjectFilter: "platform.identity.user.>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if secret == "" || bytes.Contains(stored.SecretCiphertext, []byte(secret)) {
		t.Fatal("signing secret was missing or stored in plaintext")
	}
	if created.ID != "subscription-1" || created.CreatedBy != "admin-1" || created.TimeoutMS != 5000 || created.MaxAttempts != 5 {
		t.Fatalf("CreateSubscription() = %+v", created)
	}
}

func TestServiceCreateSubscriptionRequiresActorAndPublicEndpoint(t *testing.T) {
	store := &fakeStore{createFn: func(_ context.Context, value Subscription) (Subscription, error) { return value, nil }}
	service := newTestService(t, store)
	input := CreateSubscriptionInput{TenantID: "tenant-1", Name: "events", EndpointURL: "https://hooks.example.com/events", SubjectFilter: "platform.identity.>"}
	if _, _, err := service.CreateSubscription(t.Context(), input); err == nil {
		t.Fatal("CreateSubscription() accepted a missing actor")
	}
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "admin-1", Type: principal.TypeUser, TenantID: "tenant-1"})
	input.EndpointURL = "https://127.0.0.1/events"
	if _, _, err := service.CreateSubscription(ctx, input); err == nil {
		t.Fatal("CreateSubscription() accepted a private endpoint")
	}
}

func TestServiceCreateSubscriptionRejectsDifferentTenant(t *testing.T) {
	called := false
	store := &fakeStore{createFn: func(_ context.Context, value Subscription) (Subscription, error) {
		called = true
		return value, nil
	}}
	service := newTestService(t, store)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "user-1", Type: principal.TypeUser, TenantID: "tenant-1"})
	_, _, err := service.CreateSubscription(ctx, CreateSubscriptionInput{
		TenantID: "tenant-2", Name: "events", EndpointURL: "https://hooks.example.com/events", SubjectFilter: "platform.identity.>",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("CreateSubscription() error = %v, want ErrForbidden", err)
	}
	if called {
		t.Fatal("CreateSubscription() wrote a cross-tenant subscription")
	}
}

func TestAuthorizeTenantAllowsExplicitServiceCallers(t *testing.T) {
	t.Parallel()
	for _, identity := range []principal.Principal{
		{ID: "service-1", Type: principal.TypeServiceAccount},
		{ID: "system-1", Type: principal.TypeSystem},
	} {
		if err := authorizeTenant(principal.WithContext(t.Context(), identity), "tenant-2"); err != nil {
			t.Fatalf("authorizeTenant(%s) error = %v", identity.Type, err)
		}
	}
}

func TestServicePlanDeliveriesFiltersAndDeduplicates(t *testing.T) {
	var inserted []Delivery
	store := &fakeStore{
		listActiveFn: func(_ context.Context, _ *sqlx.Tx, tenantID string) ([]Subscription, error) {
			if tenantID != "tenant-1" {
				t.Fatalf("tenant ID = %q", tenantID)
			}
			return []Subscription{
				{ID: "matching", TenantID: tenantID, SubjectFilter: "platform.identity.user.>"},
				{ID: "different", TenantID: tenantID, SubjectFilter: "platform.tenant.>"},
			}, nil
		},
		insertFn: func(_ context.Context, _ *sqlx.Tx, value Delivery) (bool, error) {
			inserted = append(inserted, value)
			return true, nil
		},
	}
	service := newTestService(t, store)
	service.newID = func() string { return "delivery-1" }
	service.now = func() time.Time { return time.Unix(1_700_000_100, 0) }
	envelope := &commonv1.EventEnvelope{
		EventId: "event-1", EventType: "platform.identity.user.created.v1", AggregateId: "user-1",
		AggregateType: "user", TenantId: "tenant-1", SchemaVersion: 1, OccurredAt: timestamppb.New(time.Unix(1_700_000_000, 0)),
		Context: &commonv1.RequestContext{RequestId: "request-1", TraceId: "trace-1", ActorId: "actor-1"}, Payload: []byte("protobuf-payload"),
	}
	count, err := service.PlanDeliveriesTx(t.Context(), &sqlx.Tx{}, "platform.identity.user.created.v1", envelope)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || len(inserted) != 1 || inserted[0].SubscriptionID != "matching" || !bytes.Contains(inserted[0].Payload, []byte(`"payload_base64"`)) {
		t.Fatalf("planned deliveries = %+v, count = %d", inserted, count)
	}
	if _, err := service.PlanDeliveriesTx(t.Context(), &sqlx.Tx{}, "platform.webhook.delivery.succeeded.v1", envelope); err == nil {
		t.Fatal("PlanDeliveriesTx() accepted recursive webhook event")
	}
}

func newTestService(t *testing.T, store Store) *Service {
	t.Helper()
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32)), "test-v1")
	if err != nil {
		t.Fatal(err)
	}
	box.rand = bytes.NewReader(bytes.Repeat([]byte{2}, 1024))
	policy, err := NewURLPolicy(staticResolver{addresses: map[string][]netip.Addr{
		"hooks.example.com": {netip.MustParseAddr("93.184.216.34")},
	}}, false, []int{443})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, box, policy)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

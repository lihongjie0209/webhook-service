package webhook

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lihongjie0209/microservice-platform-go/appaccess"
	"github.com/lihongjie0209/microservice-platform-go/principal"
	commonv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/common/v1"
)

type Store interface {
	CreateSubscription(context.Context, Subscription) (Subscription, error)
	UpdateSubscription(context.Context, Subscription, int64, string) (Subscription, error)
	RotateSecret(context.Context, string, string, string, int64, []byte, string, string) (Subscription, error)
	DeleteSubscription(context.Context, string, string, string, int64, string) error
	GetSubscription(context.Context, string, string, string) (Subscription, error)
	ListSubscriptions(context.Context, SubscriptionFilter) (Page[Subscription], error)
	GetDelivery(context.Context, string, string, string) (Delivery, error)
	ListDeliveries(context.Context, DeliveryFilter) (Page[Delivery], error)
	ReplayDelivery(context.Context, string, string, string, int64, string) (Delivery, error)
	ListActiveSubscriptionsTx(context.Context, *sqlx.Tx, string, string) ([]Subscription, error)
	InsertDeliveryTx(context.Context, *sqlx.Tx, Delivery) (bool, error)
	InsertTestDelivery(context.Context, Delivery, int64) (Delivery, error)
}

type Service struct {
	store        Store
	box          *SecretBox
	policy       *URLPolicy
	applications appaccess.Verifier
	now          func() time.Time
	newID        func() string
}

func NewService(store Store, box *SecretBox, policy *URLPolicy, applications appaccess.Verifier) (*Service, error) {
	if store == nil || box == nil || policy == nil || applications == nil {
		return nil, errors.New("webhook store, secret box, URL policy, and application verifier are required")
	}
	return &Service{store: store, box: box, policy: policy, applications: applications, now: time.Now, newID: uuid.NewString}, nil
}

type CreateSubscriptionInput struct {
	TenantID            string
	ApplicationID       string
	Name                string
	EndpointURL         string
	SubjectFilter       string
	TimeoutMS           int
	MaxAttempts         int
	RetryInitialSeconds int
}

func (s *Service) CreateSubscription(ctx context.Context, input CreateSubscriptionInput) (Subscription, string, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return Subscription{}, "", err
	}
	input = defaultCreate(input)
	if err := validateSubscription(input.TenantID, input.Name, input.EndpointURL, input.SubjectFilter, input.TimeoutMS, input.MaxAttempts, input.RetryInitialSeconds); err != nil {
		return Subscription{}, "", err
	}
	if err := s.authorizeApplication(ctx, input.TenantID, input.ApplicationID); err != nil {
		return Subscription{}, "", err
	}
	if _, _, err := s.policy.Resolve(ctx, input.EndpointURL); err != nil {
		return Subscription{}, "", fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	plain, encrypted, keyID, err := s.box.Generate()
	if err != nil {
		return Subscription{}, "", err
	}
	now := s.now()
	value := Subscription{
		ID: s.newID(), TenantID: input.TenantID, ApplicationID: input.ApplicationID, Name: input.Name,
		EndpointURL: input.EndpointURL, SubjectFilter: input.SubjectFilter, Status: SubscriptionActive,
		TimeoutMS: input.TimeoutMS, MaxAttempts: input.MaxAttempts, RetryInitialSeconds: input.RetryInitialSeconds,
		SecretCiphertext: encrypted, SecretKeyID: keyID, SecretVersion: 1,
		Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: actor, UpdatedBy: actor,
	}
	created, err := s.store.CreateSubscription(ctx, value)
	if err != nil {
		return Subscription{}, "", err
	}
	return created, plain, nil
}

type UpdateSubscriptionInput struct {
	ID                  string
	TenantID            string
	ApplicationID       string
	Name                string
	EndpointURL         string
	SubjectFilter       string
	Status              string
	TimeoutMS           int
	MaxAttempts         int
	RetryInitialSeconds int
	ExpectedVersion     int64
}

func (s *Service) UpdateSubscription(ctx context.Context, input UpdateSubscriptionInput) (Subscription, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return Subscription{}, err
	}
	if input.ID == "" || input.ExpectedVersion < 1 || !validSubscriptionStatus(input.Status) {
		return Subscription{}, invalid("webhook subscription ID, status, and expected version are required")
	}
	if err := validateSubscription(input.TenantID, input.Name, input.EndpointURL, input.SubjectFilter, input.TimeoutMS, input.MaxAttempts, input.RetryInitialSeconds); err != nil {
		return Subscription{}, err
	}
	if err := s.authorizeApplication(ctx, input.TenantID, input.ApplicationID); err != nil {
		return Subscription{}, err
	}
	if _, _, err := s.policy.Resolve(ctx, input.EndpointURL); err != nil {
		return Subscription{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return s.store.UpdateSubscription(ctx, Subscription{
		ID: input.ID, TenantID: input.TenantID, ApplicationID: input.ApplicationID, Name: input.Name, EndpointURL: input.EndpointURL,
		SubjectFilter: input.SubjectFilter, Status: input.Status, TimeoutMS: input.TimeoutMS,
		MaxAttempts: input.MaxAttempts, RetryInitialSeconds: input.RetryInitialSeconds,
	}, input.ExpectedVersion, actor)
}

func (s *Service) RotateSecret(ctx context.Context, tenantID, applicationID, id string, expectedVersion int64) (Subscription, string, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return Subscription{}, "", err
	}
	if tenantID == "" || applicationID == "" || id == "" || expectedVersion < 1 {
		return Subscription{}, "", invalid("tenant ID, subscription ID, and expected version are required")
	}
	if err := s.authorizeApplication(ctx, tenantID, applicationID); err != nil {
		return Subscription{}, "", err
	}
	plain, encrypted, keyID, err := s.box.Generate()
	if err != nil {
		return Subscription{}, "", err
	}
	value, err := s.store.RotateSecret(ctx, tenantID, applicationID, id, expectedVersion, encrypted, keyID, actor)
	if err != nil {
		return Subscription{}, "", err
	}
	return value, plain, nil
}

func (s *Service) DeleteSubscription(ctx context.Context, tenantID, applicationID, id string, expectedVersion int64) error {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return err
	}
	if tenantID == "" || applicationID == "" || id == "" || expectedVersion < 1 {
		return invalid("tenant ID, subscription ID, and expected version are required")
	}
	if err := s.authorizeApplication(ctx, tenantID, applicationID); err != nil {
		return err
	}
	return s.store.DeleteSubscription(ctx, tenantID, applicationID, id, expectedVersion, actor)
}

func (s *Service) GetSubscription(ctx context.Context, tenantID, applicationID, id string) (Subscription, error) {
	if tenantID == "" || applicationID == "" || id == "" {
		return Subscription{}, invalid("tenant ID and subscription ID are required")
	}
	if err := s.authorizeApplication(ctx, tenantID, applicationID); err != nil {
		return Subscription{}, err
	}
	return s.store.GetSubscription(ctx, tenantID, applicationID, id)
}

func (s *Service) ListSubscriptions(ctx context.Context, filter SubscriptionFilter) (Page[Subscription], error) {
	if filter.TenantID == "" || filter.ApplicationID == "" {
		return Page[Subscription]{}, invalid("tenant ID is required")
	}
	if err := s.authorizeApplication(ctx, filter.TenantID, filter.ApplicationID); err != nil {
		return Page[Subscription]{}, err
	}
	filter.Page, filter.PageSize = normalizePage(filter.Page, filter.PageSize)
	if filter.Status != "" && !validSubscriptionStatus(filter.Status) {
		return Page[Subscription]{}, invalid("invalid webhook subscription status")
	}
	return s.store.ListSubscriptions(ctx, filter)
}

func (s *Service) GetDelivery(ctx context.Context, tenantID, applicationID, id string) (Delivery, error) {
	if tenantID == "" || applicationID == "" || id == "" {
		return Delivery{}, invalid("tenant ID and delivery ID are required")
	}
	if err := s.authorizeApplication(ctx, tenantID, applicationID); err != nil {
		return Delivery{}, err
	}
	return s.store.GetDelivery(ctx, tenantID, applicationID, id)
}

func (s *Service) ListDeliveries(ctx context.Context, filter DeliveryFilter) (Page[Delivery], error) {
	if filter.TenantID == "" || filter.ApplicationID == "" || (filter.Status != "" && !validDeliveryStatus(filter.Status)) {
		return Page[Delivery]{}, invalid("tenant ID and a valid delivery status are required")
	}
	if err := s.authorizeApplication(ctx, filter.TenantID, filter.ApplicationID); err != nil {
		return Page[Delivery]{}, err
	}
	filter.Page, filter.PageSize = normalizePage(filter.Page, filter.PageSize)
	return s.store.ListDeliveries(ctx, filter)
}

func (s *Service) ReplayDelivery(ctx context.Context, tenantID, applicationID, id string, expectedVersion int64) (Delivery, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return Delivery{}, err
	}
	if tenantID == "" || applicationID == "" || id == "" || expectedVersion < 1 {
		return Delivery{}, invalid("tenant ID, delivery ID, and expected version are required")
	}
	if err := s.authorizeApplication(ctx, tenantID, applicationID); err != nil {
		return Delivery{}, err
	}
	return s.store.ReplayDelivery(ctx, tenantID, applicationID, id, expectedVersion, actor)
}

func (s *Service) TestSubscription(ctx context.Context, tenantID, applicationID, id string, payloadJSON []byte, expectedVersion int64) (Delivery, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return Delivery{}, err
	}
	if tenantID == "" || applicationID == "" || id == "" || expectedVersion < 1 || !json.Valid(payloadJSON) || len(payloadJSON) > 1<<20 {
		return Delivery{}, invalid("tenant ID, subscription ID, and a valid JSON payload up to 1 MiB are required")
	}
	if err := s.authorizeApplication(ctx, tenantID, applicationID); err != nil {
		return Delivery{}, err
	}
	now := s.now()
	deliveryID := s.newID()
	return s.store.InsertTestDelivery(ctx, Delivery{
		ID: deliveryID, SubscriptionID: id, TenantID: tenantID, ApplicationID: applicationID, EventID: "test:" + deliveryID,
		EventSubject: "platform.webhook.test.v1", Payload: append([]byte(nil), payloadJSON...), Status: DeliveryPending,
		NextAttemptAt: now, Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: actor, UpdatedBy: actor,
	}, expectedVersion)
}

type externalEnvelope struct {
	EventID       string `json:"event_id"`
	EventType     string `json:"event_type"`
	AggregateID   string `json:"aggregate_id"`
	AggregateType string `json:"aggregate_type"`
	TenantID      string `json:"tenant_id"`
	ApplicationID string `json:"application_id"`
	SchemaVersion uint32 `json:"schema_version"`
	OccurredAt    string `json:"occurred_at"`
	RequestID     string `json:"request_id,omitempty"`
	TraceID       string `json:"trace_id,omitempty"`
	ActorID       string `json:"actor_id,omitempty"`
	PayloadBase64 string `json:"payload_base64"`
}

func (s *Service) PlanDeliveriesTx(ctx context.Context, tx *sqlx.Tx, subject string, envelope *commonv1.EventEnvelope) (int, error) {
	if tx == nil || envelope == nil || envelope.GetEventId() == "" || envelope.GetTenantId() == "" || envelope.GetApplicationId() == "" || strings.HasPrefix(subject, "platform.webhook.") {
		return 0, errors.New("valid tenant event and transaction are required")
	}
	subscriptions, err := s.store.ListActiveSubscriptionsTx(ctx, tx, envelope.GetTenantId(), envelope.GetApplicationId())
	if err != nil {
		return 0, err
	}
	payload, err := json.Marshal(externalEnvelope{
		EventID: envelope.GetEventId(), EventType: envelope.GetEventType(), AggregateID: envelope.GetAggregateId(),
		AggregateType: envelope.GetAggregateType(), TenantID: envelope.GetTenantId(), ApplicationID: envelope.GetApplicationId(), SchemaVersion: envelope.GetSchemaVersion(),
		OccurredAt: envelope.GetOccurredAt().AsTime().Format(time.RFC3339Nano), RequestID: envelope.GetContext().GetRequestId(),
		TraceID: envelope.GetContext().GetTraceId(), ActorID: envelope.GetContext().GetActorId(), PayloadBase64: base64.StdEncoding.EncodeToString(envelope.GetPayload()),
	})
	if err != nil {
		return 0, fmt.Errorf("encode external webhook envelope: %w", err)
	}
	now := s.now()
	created := 0
	for _, subscription := range subscriptions {
		if !SubjectMatches(subscription.SubjectFilter, subject) {
			continue
		}
		inserted, err := s.store.InsertDeliveryTx(ctx, tx, Delivery{
			ID: s.newID(), SubscriptionID: subscription.ID, TenantID: subscription.TenantID, ApplicationID: subscription.ApplicationID,
			EventID: envelope.GetEventId(), EventSubject: subject, Payload: payload, Status: DeliveryPending,
			NextAttemptAt: now, CreatedAt: now, UpdatedAt: now, CreatedBy: "webhook-event-consumer", UpdatedBy: "webhook-event-consumer",
		})
		if err != nil {
			return 0, err
		}
		if inserted {
			created++
		}
	}
	return created, nil
}

func actorFromContext(ctx context.Context) (string, error) {
	value, ok := principal.FromContext(ctx)
	if !ok {
		return "", ErrActorRequired
	}
	return value.ID, nil
}

func authorizeTenant(ctx context.Context, tenantID string) error {
	identity, ok := principal.FromContext(ctx)
	if !ok {
		return ErrActorRequired
	}
	switch identity.Type {
	case principal.TypeServiceAccount, principal.TypeSystem:
		return nil
	case principal.TypeUser:
		if identity.TenantID != "" && identity.TenantID == strings.TrimSpace(tenantID) {
			return nil
		}
	}
	return ErrForbidden
}

func (s *Service) authorizeApplication(ctx context.Context, tenantID, applicationID string) error {
	if strings.TrimSpace(applicationID) == "" {
		return invalid("application ID is required")
	}
	if err := authorizeTenant(ctx, tenantID); err != nil {
		return err
	}
	if err := s.applications.Verify(ctx, tenantID, applicationID); err != nil {
		if errors.Is(err, appaccess.ErrNotGranted) {
			return ErrForbidden
		}
		return fmt.Errorf("verify tenant application grant: %w", err)
	}
	return nil
}

func defaultCreate(input CreateSubscriptionInput) CreateSubscriptionInput {
	if input.TimeoutMS == 0 {
		input.TimeoutMS = 5000
	}
	if input.MaxAttempts == 0 {
		input.MaxAttempts = 5
	}
	if input.RetryInitialSeconds == 0 {
		input.RetryInitialSeconds = 5
	}
	return input
}

func validateSubscription(tenantID, name, endpoint, filter string, timeoutMS, maxAttempts, retrySeconds int) error {
	if tenantID == "" || strings.TrimSpace(name) == "" || endpoint == "" || !ValidSubjectFilter(filter) {
		return invalid("tenant ID, name, endpoint URL, and valid platform subject filter are required")
	}
	if len(name) > 255 || timeoutMS < 100 || timeoutMS > 30000 || maxAttempts < 1 || maxAttempts > 20 || retrySeconds < 1 || retrySeconds > 86400 {
		return invalid("webhook subscription limits are invalid")
	}
	return nil
}

func invalid(message string) error { return fmt.Errorf("%w: %s", ErrInvalid, message) }

func validSubscriptionStatus(status string) bool {
	return status == SubscriptionActive || status == SubscriptionPaused || status == SubscriptionDisabled
}

func validDeliveryStatus(status string) bool {
	return status == DeliveryPending || status == DeliveryProcessing || status == DeliverySucceeded || status == DeliveryRetrying || status == DeliveryDead
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}

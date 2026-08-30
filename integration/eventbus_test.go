//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	platformeventbus "github.com/lihongjie0209/microservice-platform-go/eventbus"
	platforminbox "github.com/lihongjie0209/microservice-platform-go/inbox"
	identityv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/identity/v1"
	"github.com/lihongjie0209/webhook-service/internal/config"
	appdb "github.com/lihongjie0209/webhook-service/internal/database"
	serviceeventbus "github.com/lihongjie0209/webhook-service/internal/eventbus"
	"github.com/lihongjie0209/webhook-service/internal/migration"
	webhookdomain "github.com/lihongjie0209/webhook-service/internal/webhook"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestJetStreamEventCreatesOneInboxProtectedDelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	dsn, migrationURL := startDatabase(t, ctx, "postgres")
	migrationPath, err := filepath.Abs(filepath.Join("..", "migrations", "postgres"))
	if err != nil {
		t.Fatal(err)
	}
	const schema = "webhook_event_integration"
	if err := migration.Run(config.Migration{Path: migrationPath, DatabaseURL: migrationURL, Table: "webhook_event_schema_migrations", Schema: schema, CreateSchema: true}, "up", 0); err != nil {
		t.Fatal(err)
	}
	db, err := appdb.Open(ctx, config.Database{Type: "postgres", DSN: dsn, Schema: schema, MaxOpenConns: 8, MaxIdleConns: 2, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repository, err := webhookdomain.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := repository.CreateSubscription(ctx, webhookdomain.Subscription{
		ID: "subscription-event", TenantID: "tenant-event", Name: "identity events", EndpointURL: "https://hooks.example.com/events",
		SubjectFilter: "platform.identity.user.>", Status: webhookdomain.SubscriptionActive, TimeoutMS: 1000, MaxAttempts: 3,
		RetryInitialSeconds: 1, SecretCiphertext: []byte("not-used-by-planner"), SecretKeyID: "test-v1", SecretVersion: 1,
		CreatedAt: now, UpdatedAt: now, CreatedBy: "test", UpdatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
	box, err := webhookdomain.NewSecretBox(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)), "test-v1")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := webhookdomain.NewURLPolicy(net.DefaultResolver, false, []int{443})
	if err != nil {
		t.Fatal(err)
	}
	service, err := webhookdomain.NewService(repository, box, policy)
	if err != nil {
		t.Fatal(err)
	}
	inboxStore, err := platforminbox.NewSQLStore(db, platforminbox.DialectPostgres, "webhook_event_inbox")
	if err != nil {
		t.Fatal(err)
	}

	natsURL := startNATS(t, ctx)
	bus, err := serviceeventbus.New(ctx, config.Config{App: config.App{Name: "webhook-event-integration"}, EventBus: config.EventBus{
		Enabled: true, URLs: []string{natsURL}, StreamName: "PLATFORM_EVENTS", Subjects: []string{"platform.>"},
		Storage: "memory", MaxAge: time.Hour, DuplicateWindow: time.Minute, ConnectTimeout: 10 * time.Second,
		ReconnectWait: time.Second, PublishTimeout: 5 * time.Second, ConsumerAckWait: 10 * time.Second,
		ConsumerAckTimeout: time.Second, ConsumerHandlerTimeout: 5 * time.Second, ConsumerRetryDelay: 100 * time.Millisecond,
		ConsumerMaxRetryDelay: time.Second, ConsumerMaxDeliver: 3, ConsumerMaxAckPending: 8, DeadLetterMaxDataBytes: 1024,
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bus.Close() })
	consumeCtx, stopConsumer := context.WithCancel(ctx)
	consumerDone := make(chan error, 1)
	go func() {
		consumerDone <- bus.Consume(consumeCtx, "webhook-event-integration", "platform.identity.>", func(handlerCtx context.Context, envelope *serviceeventbus.Envelope) error {
			_, err := inboxStore.Process(handlerCtx, platforminbox.Key{Consumer: "webhook-event-integration", EventID: envelope.GetEventId()}, "webhook-event-consumer", func(handlerCtx context.Context, tx *sqlx.Tx) error {
				_, err := service.PlanDeliveriesTx(handlerCtx, tx, envelope.GetEventType(), envelope)
				return err
			})
			return err
		})
	}()
	envelope, err := platformeventbus.NewEnvelope(platformeventbus.Metadata{
		EventID: "event-webhook-1", EventType: "platform.identity.user.created.v1", AggregateID: "user-1",
		AggregateType: "user", TenantID: "tenant-event", SchemaVersion: 1,
	}, &identityv1.UserStatusChangedEvent{UserId: "user-1", Reason: "created"})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := bus.Publish(ctx, envelope.GetEventType(), envelope); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		var count int
		if err := db.GetContext(ctx, &count, `SELECT COUNT(*) FROM webhook_deliveries WHERE event_id='event-webhook-1'`); err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("webhook deliveries = %d, want 1", count)
		}
		time.Sleep(100 * time.Millisecond)
	}
	stopConsumer()
	if err := <-consumerDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("consumer stop error = %v", err)
	}
}

func startNATS(t *testing.T, ctx context.Context) string {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "nats:2.14.6-alpine", AlwaysPullImage: true, ExposedPorts: []string{"4222/tcp"},
			Cmd: []string{"--jetstream", "--store_dir=/data"}, WaitingFor: wait.ForLog("Server is ready").WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "4222/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("nats://%s:%s", host, port.Port())
}

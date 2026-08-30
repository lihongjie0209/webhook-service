//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lihongjie0209/webhook-service/internal/config"
	appdb "github.com/lihongjie0209/webhook-service/internal/database"
	"github.com/lihongjie0209/webhook-service/internal/migration"
	webhookdomain "github.com/lihongjie0209/webhook-service/internal/webhook"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestRepositoryAndMigrations(t *testing.T) {
	for _, databaseType := range []string{"postgres", "mysql"} {
		t.Run(databaseType, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			dsn, migrationURL := startDatabase(t, ctx, databaseType)
			migrationPath, err := filepath.Abs(filepath.Join("..", "migrations", databaseType))
			if err != nil {
				t.Fatal(err)
			}
			schema := ""
			if databaseType == "postgres" {
				schema = "integration_postgres"
			}
			migrationCfg := config.Migration{Path: migrationPath, DatabaseURL: migrationURL, Table: "integration_" + databaseType + "_schema_migrations", Schema: schema, CreateSchema: schema != ""}
			migrationErrors := make(chan error, 3)
			var migrations sync.WaitGroup
			for range 3 {
				migrations.Add(1)
				go func() {
					defer migrations.Done()
					migrationErrors <- migration.Run(migrationCfg, "up", 0)
				}()
			}
			migrations.Wait()
			close(migrationErrors)
			for err := range migrationErrors {
				if err != nil {
					t.Fatalf("concurrent migration up: %v", err)
				}
			}

			db, err := appdb.Open(ctx, config.Database{Type: databaseType, DSN: dsn, Schema: schema, MaxOpenConns: 5, MaxIdleConns: 2, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 10 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			var userTables int
			if databaseType == "postgres" {
				if err := db.GetContext(ctx, &userTables, `SELECT count(*) FROM pg_tables WHERE schemaname = current_schema() AND tablename = 'users'`); err != nil {
					t.Fatal(err)
				}
				var timezone string
				if err := db.GetContext(ctx, &timezone, `SHOW TIMEZONE`); err != nil || timezone != "Asia/Shanghai" {
					t.Fatalf("timezone=%q err=%v", timezone, err)
				}
			} else if err := db.GetContext(ctx, &userTables, `SELECT count(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'users'`); err != nil {
				t.Fatal(err)
			}
			if userTables != 0 {
				t.Fatal("generic template migration must not create a users table")
			}
			assertWebhookRepository(t, ctx, db)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if err := migration.Run(migrationCfg, "down", 0); err != nil {
				t.Fatalf("migration down: %v", err)
			}
		})
	}
}

func assertWebhookRepository(t *testing.T, ctx context.Context, sqlDB *sqlx.DB) {
	t.Helper()
	repository, err := webhookdomain.NewRepository(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Microsecond)
	created, err := repository.CreateSubscription(ctx, webhookdomain.Subscription{
		ID: "subscription-1", TenantID: "tenant-1", ApplicationID: "application-1", Name: "CRM",
		EndpointURL: "https://hooks.example.com/events", SubjectFilter: "platform.identity.>", Status: webhookdomain.SubscriptionActive,
		TimeoutMS: 5000, MaxAttempts: 5, RetryInitialSeconds: 5, SecretCiphertext: []byte("encrypted"),
		SecretKeyID: "test-v1", SecretVersion: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: "tester", UpdatedBy: "tester",
	})
	if err != nil || created.Version != 1 {
		t.Fatalf("CreateSubscription() = %+v, %v", created, err)
	}
	created.Name = "CRM updated"
	updated, err := repository.UpdateSubscription(ctx, created, 1, "tester-2")
	if err != nil || updated.Version != 2 || updated.Name != "CRM updated" {
		t.Fatalf("UpdateSubscription() = %+v, %v", updated, err)
	}
	if _, err := repository.UpdateSubscription(ctx, created, 1, "tester-2"); !errors.Is(err, webhookdomain.ErrVersionConflict) {
		t.Fatalf("stale UpdateSubscription() error = %v", err)
	}
	delivery, err := repository.InsertDelivery(ctx, webhookdomain.Delivery{
		ID: "delivery-1", SubscriptionID: created.ID, TenantID: created.TenantID, EventID: "event-1",
		EventSubject: "platform.identity.user.created.v1", Payload: []byte(`{"event_id":"event-1"}`),
		Status: webhookdomain.DeliveryPending, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now, CreatedBy: "consumer", UpdatedBy: "consumer",
	})
	if err != nil || delivery.Version != 1 {
		t.Fatalf("InsertDelivery() = %+v, %v", delivery, err)
	}
	claimed, err := repository.ClaimDeliveries(ctx, 10, time.Minute, "dispatcher")
	if err != nil || len(claimed) != 1 || claimed[0].AttemptCount != 1 {
		t.Fatalf("ClaimDeliveries() = %+v, %v", claimed, err)
	}
	retryAt := time.Now().Add(-time.Second)
	if err := repository.FailDelivery(ctx, claimed[0], webhookdomain.SendResult{StatusCode: 503, Body: "unavailable"}, errors.New("temporary"), &retryAt, "dispatcher"); err != nil {
		t.Fatal(err)
	}
	claimed, err = repository.ClaimDeliveries(ctx, 10, time.Minute, "dispatcher")
	if err != nil || len(claimed) != 1 || claimed[0].AttemptCount != 2 {
		t.Fatalf("retry ClaimDeliveries() = %+v, %v", claimed, err)
	}
	if err := repository.CompleteDelivery(ctx, claimed[0], webhookdomain.SendResult{StatusCode: 204}, "dispatcher"); err != nil {
		t.Fatal(err)
	}
	result, err := repository.ListDeliveries(ctx, webhookdomain.DeliveryFilter{TenantID: "tenant-1", Status: webhookdomain.DeliverySucceeded, Page: 1, PageSize: 20})
	if err != nil || result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("ListDeliveries() = %+v, %v", result, err)
	}
}

func startDatabase(t *testing.T, ctx context.Context, databaseType string) (string, string) {
	t.Helper()
	switch databaseType {
	case "postgres":
		container, err := postgres.Run(ctx, "postgres:17-alpine", postgres.WithDatabase("app"), postgres.WithUsername("app"), postgres.WithPassword("app"), postgres.BasicWaitStrategies(), postgres.WithSQLDriver("pgx"))
		if err != nil {
			t.Fatal(err)
		}
		testcontainers.CleanupContainer(t, container)
		dsn, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			t.Fatal(err)
		}
		return dsn, dsn
	case "mysql":
		container, err := mysql.Run(ctx, "mysql:8.4", mysql.WithDatabase("app"), mysql.WithUsername("app"), mysql.WithPassword("app"))
		if err != nil {
			t.Fatal(err)
		}
		testcontainers.CleanupContainer(t, container)
		dsn, err := container.ConnectionString(ctx, "parseTime=true")
		if err != nil {
			t.Fatal(err)
		}
		migrationDSN, err := container.ConnectionString(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return dsn, "mysql://" + migrationDSN
	default:
		t.Fatal(fmt.Errorf("unsupported database %q", databaseType))
		return "", ""
	}
}

package webhook

import (
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestApplicationScopedFiltersAlwaysReachSQL(t *testing.T) {
	t.Parallel()

	subscriptionSQL, subscriptionArgs := subscriptionWhere(SubscriptionFilter{TenantID: "tenant-1", ApplicationID: "app-1"})
	if subscriptionSQL != "WHERE deleted_at IS NULL AND tenant_id=? AND application_id=?" || len(subscriptionArgs) != 2 || subscriptionArgs[1] != "app-1" {
		t.Fatalf("subscriptionWhere() = %q, %#v", subscriptionSQL, subscriptionArgs)
	}
	deliverySQL, deliveryArgs := deliveryWhere(DeliveryFilter{TenantID: "tenant-1", ApplicationID: "app-1"})
	if deliverySQL != "WHERE tenant_id=? AND application_id=?" || len(deliveryArgs) != 2 || deliveryArgs[1] != "app-1" {
		t.Fatalf("deliveryWhere() = %q, %#v", deliverySQL, deliveryArgs)
	}
}

func TestInsertTestDeliveryRejectsNonActiveSubscriptionInsideTransaction(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository := &Repository{db: sqlx.NewDb(database, "sqlmock")}
	value := Delivery{ID: "delivery-1", SubscriptionID: "subscription-1", TenantID: "tenant-1", ApplicationID: "app-1"}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT version,status FROM webhook_subscriptions WHERE id=? AND tenant_id=? AND application_id=? AND deleted_at IS NULL FOR UPDATE`)).
		WithArgs(value.SubscriptionID, value.TenantID, value.ApplicationID).
		WillReturnRows(sqlmock.NewRows([]string{"version", "status"}).AddRow(5, SubscriptionPaused))
	mock.ExpectRollback()

	_, err = repository.InsertTestDelivery(t.Context(), value, 5)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("InsertTestDelivery() error = %v, want ErrVersionConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

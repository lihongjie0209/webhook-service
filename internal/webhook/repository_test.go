package webhook

import "testing"

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

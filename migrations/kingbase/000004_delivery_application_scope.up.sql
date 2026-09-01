ALTER TABLE webhook_deliveries ADD COLUMN application_id TEXT;
UPDATE webhook_deliveries d
SET application_id = s.application_id
FROM webhook_subscriptions s
WHERE s.id = d.subscription_id;
ALTER TABLE webhook_deliveries ALTER COLUMN application_id SET NOT NULL;

DROP INDEX idx_webhook_deliveries_tenant;
CREATE INDEX idx_webhook_deliveries_tenant ON webhook_deliveries(tenant_id,application_id,subscription_id,created_at DESC,id);

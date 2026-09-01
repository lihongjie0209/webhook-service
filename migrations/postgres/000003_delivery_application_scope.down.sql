DROP INDEX idx_webhook_deliveries_tenant;
CREATE INDEX idx_webhook_deliveries_tenant ON webhook_deliveries(tenant_id,subscription_id,created_at DESC,id);
ALTER TABLE webhook_deliveries DROP COLUMN application_id;

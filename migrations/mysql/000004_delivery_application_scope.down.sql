ALTER TABLE webhook_deliveries
  DROP INDEX idx_webhook_deliveries_tenant,
  ADD INDEX idx_webhook_deliveries_tenant (tenant_id,subscription_id,created_at,id);
ALTER TABLE webhook_deliveries DROP COLUMN application_id;

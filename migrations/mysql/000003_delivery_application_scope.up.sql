ALTER TABLE webhook_deliveries ADD COLUMN application_id VARCHAR(36) NULL AFTER tenant_id;
UPDATE webhook_deliveries d
JOIN webhook_subscriptions s ON s.id = d.subscription_id
SET d.application_id = s.application_id;
ALTER TABLE webhook_deliveries MODIFY COLUMN application_id VARCHAR(36) NOT NULL;

ALTER TABLE webhook_deliveries
  DROP INDEX idx_webhook_deliveries_tenant,
  ADD INDEX idx_webhook_deliveries_tenant (tenant_id,application_id,subscription_id,created_at,id);

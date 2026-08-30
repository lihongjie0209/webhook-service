CREATE TABLE webhook_subscriptions (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  application_id TEXT NOT NULL,
  name TEXT NOT NULL,
  endpoint_url TEXT NOT NULL,
  subject_filter TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('active','paused','disabled')),
  timeout_ms INTEGER NOT NULL CHECK (timeout_ms BETWEEN 100 AND 30000),
  max_attempts INTEGER NOT NULL CHECK (max_attempts BETWEEN 1 AND 20),
  retry_initial_seconds INTEGER NOT NULL CHECK (retry_initial_seconds BETWEEN 1 AND 86400),
  secret_ciphertext BYTEA NOT NULL,
  secret_key_id TEXT NOT NULL,
  secret_version BIGINT NOT NULL DEFAULT 1,
  version BIGINT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  created_by TEXT NOT NULL,
  updated_by TEXT NOT NULL,
  deleted_at TIMESTAMPTZ NULL
);
CREATE INDEX idx_webhook_subscriptions_tenant ON webhook_subscriptions(tenant_id,status,application_id,id) WHERE deleted_at IS NULL;

-- Delivery event identity is globally idempotent per subscription. Retention is
-- therefore preferred over time partitioning until PostgreSQL supports a global
-- unique index across partitions or the identity contract includes a time key.
CREATE TABLE webhook_deliveries (
  id TEXT PRIMARY KEY,
  subscription_id TEXT NOT NULL REFERENCES webhook_subscriptions(id),
  tenant_id TEXT NOT NULL,
  event_id TEXT NOT NULL,
  event_subject TEXT NOT NULL,
  payload BYTEA NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending','processing','succeeded','retrying','dead')),
  attempt_count INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL,
  last_attempt_at TIMESTAMPTZ NULL,
  response_status INTEGER NOT NULL DEFAULT 0,
  response_body TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  delivered_at TIMESTAMPTZ NULL,
  version BIGINT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  created_by TEXT NOT NULL,
  updated_by TEXT NOT NULL,
  UNIQUE(subscription_id,event_id)
);
CREATE INDEX idx_webhook_deliveries_claim ON webhook_deliveries(status,next_attempt_at,created_at);
CREATE INDEX idx_webhook_deliveries_tenant ON webhook_deliveries(tenant_id,subscription_id,created_at DESC,id);

CREATE TABLE webhook_event_inbox (
  consumer TEXT NOT NULL,
  event_id TEXT NOT NULL,
  status TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  completed_at TIMESTAMPTZ NULL,
  version BIGINT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  created_by TEXT NOT NULL,
  updated_by TEXT NOT NULL,
  PRIMARY KEY(consumer,event_id)
);

CREATE TABLE webhook_outbox_events (
  id TEXT PRIMARY KEY,
  subject TEXT NOT NULL,
  envelope BYTEA NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  available_at TIMESTAMPTZ NOT NULL,
  published_at TIMESTAMPTZ NULL,
  last_error TEXT NOT NULL DEFAULT '',
  version BIGINT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  created_by TEXT NOT NULL,
  updated_by TEXT NOT NULL
);
CREATE INDEX idx_webhook_outbox_pending ON webhook_outbox_events(published_at,available_at,created_at);

CREATE TABLE webhook_subscriptions (
  id VARCHAR(36) PRIMARY KEY, tenant_id VARCHAR(255) NOT NULL, application_id VARCHAR(255) NOT NULL,
  name VARCHAR(255) NOT NULL, endpoint_url TEXT NOT NULL, subject_filter VARCHAR(255) NOT NULL,
  status VARCHAR(32) NOT NULL, timeout_ms INTEGER NOT NULL, max_attempts INTEGER NOT NULL,
  retry_initial_seconds INTEGER NOT NULL, secret_ciphertext BLOB NOT NULL, secret_key_id VARCHAR(255) NOT NULL,
  secret_version BIGINT NOT NULL DEFAULT 1, version BIGINT NOT NULL DEFAULT 1,
  created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
  created_by VARCHAR(255) NOT NULL, updated_by VARCHAR(255) NOT NULL, deleted_at DATETIME(6) NULL,
  INDEX idx_webhook_subscriptions_tenant (tenant_id,status,application_id,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE webhook_deliveries (
  id VARCHAR(36) PRIMARY KEY, subscription_id VARCHAR(36) NOT NULL, tenant_id VARCHAR(255) NOT NULL,
  event_id VARCHAR(255) NOT NULL, event_subject VARCHAR(255) NOT NULL, payload LONGBLOB NOT NULL,
  status VARCHAR(32) NOT NULL, attempt_count INTEGER NOT NULL DEFAULT 0, next_attempt_at DATETIME(6) NOT NULL,
  last_attempt_at DATETIME(6) NULL, response_status INTEGER NOT NULL DEFAULT 0, response_body TEXT NOT NULL,
  error_message TEXT NOT NULL, delivered_at DATETIME(6) NULL, version BIGINT NOT NULL DEFAULT 1,
  created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
  created_by VARCHAR(255) NOT NULL, updated_by VARCHAR(255) NOT NULL,
  UNIQUE KEY uq_webhook_delivery_event (subscription_id,event_id),
  INDEX idx_webhook_deliveries_claim (status,next_attempt_at,created_at),
  INDEX idx_webhook_deliveries_tenant (tenant_id,subscription_id,created_at,id),
  CONSTRAINT fk_webhook_delivery_subscription FOREIGN KEY (subscription_id) REFERENCES webhook_subscriptions(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE webhook_event_inbox (
  consumer VARCHAR(255) NOT NULL, event_id VARCHAR(255) NOT NULL, status VARCHAR(32) NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL, completed_at DATETIME(6) NULL,
  version BIGINT NOT NULL DEFAULT 1, created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
  created_by VARCHAR(255) NOT NULL, updated_by VARCHAR(255) NOT NULL,
  PRIMARY KEY(consumer,event_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE webhook_outbox_events (
  id VARCHAR(36) PRIMARY KEY, subject VARCHAR(255) NOT NULL, envelope LONGBLOB NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0, available_at DATETIME(6) NOT NULL, published_at DATETIME(6) NULL,
  last_error TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1, created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL, created_by VARCHAR(255) NOT NULL, updated_by VARCHAR(255) NOT NULL,
  INDEX idx_webhook_outbox_pending (published_at,available_at,created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

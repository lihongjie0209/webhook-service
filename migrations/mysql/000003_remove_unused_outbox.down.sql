CREATE TABLE webhook_outbox_events (
  id VARCHAR(36) PRIMARY KEY, subject VARCHAR(255) NOT NULL, envelope LONGBLOB NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0, available_at DATETIME(6) NOT NULL, published_at DATETIME(6) NULL,
  last_error TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1, created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL, created_by VARCHAR(255) NOT NULL, updated_by VARCHAR(255) NOT NULL,
  INDEX idx_webhook_outbox_pending (published_at,available_at,created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

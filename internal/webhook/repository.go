package webhook

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

const subscriptionColumns = `id,tenant_id,application_id,name,endpoint_url,subject_filter,status,timeout_ms,max_attempts,retry_initial_seconds,secret_ciphertext,secret_key_id,secret_version,version,created_at,updated_at,created_by,updated_by,deleted_at`
const deliveryColumns = `id,subscription_id,tenant_id,application_id,event_id,event_subject,payload,status,attempt_count,next_attempt_at,last_attempt_at,response_status,response_body,error_message,delivered_at,version,created_at,updated_at,created_by,updated_by`

type Repository struct {
	db  *sqlx.DB
	now func() time.Time
}

func NewRepository(db *sqlx.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("webhook database is required")
	}
	return &Repository{db: db, now: time.Now}, nil
}

func (r *Repository) CreateSubscription(ctx context.Context, value Subscription) (Subscription, error) {
	query := r.db.Rebind(`INSERT INTO webhook_subscriptions (` + subscriptionColumns + `) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?,?,?,NULL)`)
	_, err := r.db.ExecContext(ctx, query,
		value.ID, value.TenantID, value.ApplicationID, value.Name, value.EndpointURL, value.SubjectFilter,
		value.Status, value.TimeoutMS, value.MaxAttempts, value.RetryInitialSeconds, value.SecretCiphertext,
		value.SecretKeyID, value.SecretVersion, value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy,
	)
	if err != nil {
		return Subscription{}, fmt.Errorf("insert webhook subscription: %w", err)
	}
	return r.GetSubscription(ctx, value.TenantID, value.ApplicationID, value.ID)
}

func (r *Repository) UpdateSubscription(ctx context.Context, value Subscription, expectedVersion int64, actor string) (Subscription, error) {
	now := r.now()
	query := r.db.Rebind(`UPDATE webhook_subscriptions SET name=?,endpoint_url=?,subject_filter=?,status=?,timeout_ms=?,max_attempts=?,retry_initial_seconds=?,version=version+1,updated_at=?,updated_by=? WHERE id=? AND tenant_id=? AND application_id=? AND version=? AND deleted_at IS NULL`)
	result, err := r.db.ExecContext(ctx, query, value.Name, value.EndpointURL, value.SubjectFilter, value.Status, value.TimeoutMS, value.MaxAttempts, value.RetryInitialSeconds, now, actor, value.ID, value.TenantID, value.ApplicationID, expectedVersion)
	if err := affectedOne(result, err, "update webhook subscription"); err != nil {
		return Subscription{}, err
	}
	return r.GetSubscription(ctx, value.TenantID, value.ApplicationID, value.ID)
}

func (r *Repository) RotateSecret(ctx context.Context, tenantID, applicationID, id string, expectedVersion int64, ciphertext []byte, keyID, actor string) (Subscription, error) {
	now := r.now()
	query := r.db.Rebind(`UPDATE webhook_subscriptions SET secret_ciphertext=?,secret_key_id=?,secret_version=secret_version+1,version=version+1,updated_at=?,updated_by=? WHERE id=? AND tenant_id=? AND application_id=? AND version=? AND deleted_at IS NULL`)
	result, err := r.db.ExecContext(ctx, query, ciphertext, keyID, now, actor, id, tenantID, applicationID, expectedVersion)
	if err := affectedOne(result, err, "rotate webhook subscription secret"); err != nil {
		return Subscription{}, err
	}
	return r.GetSubscription(ctx, tenantID, applicationID, id)
}

func (r *Repository) DeleteSubscription(ctx context.Context, tenantID, applicationID, id string, expectedVersion int64, actor string) error {
	now := r.now()
	query := r.db.Rebind(`UPDATE webhook_subscriptions SET status='disabled',deleted_at=?,version=version+1,updated_at=?,updated_by=? WHERE id=? AND tenant_id=? AND application_id=? AND version=? AND deleted_at IS NULL`)
	result, err := r.db.ExecContext(ctx, query, now, now, actor, id, tenantID, applicationID, expectedVersion)
	return affectedOne(result, err, "delete webhook subscription")
}

func (r *Repository) GetSubscription(ctx context.Context, tenantID, applicationID, id string) (Subscription, error) {
	var value Subscription
	query := r.db.Rebind(`SELECT ` + subscriptionColumns + ` FROM webhook_subscriptions WHERE id=? AND tenant_id=? AND application_id=? AND deleted_at IS NULL`)
	if err := r.db.GetContext(ctx, &value, query, id, tenantID, applicationID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Subscription{}, ErrNotFound
		}
		return Subscription{}, fmt.Errorf("select webhook subscription: %w", err)
	}
	return value, nil
}

func (r *Repository) ListSubscriptions(ctx context.Context, filter SubscriptionFilter) (Page[Subscription], error) {
	where, args := subscriptionWhere(filter)
	var total int64
	if err := r.db.GetContext(ctx, &total, r.db.Rebind(`SELECT COUNT(*) FROM webhook_subscriptions `+where), args...); err != nil {
		return Page[Subscription]{}, fmt.Errorf("count webhook subscriptions: %w", err)
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	query := r.db.Rebind(`SELECT ` + subscriptionColumns + ` FROM webhook_subscriptions ` + where + ` ORDER BY created_at DESC,id LIMIT ? OFFSET ?`)
	items := make([]Subscription, 0)
	if err := r.db.SelectContext(ctx, &items, query, args...); err != nil {
		return Page[Subscription]{}, fmt.Errorf("list webhook subscriptions: %w", err)
	}
	return Page[Subscription]{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func subscriptionWhere(filter SubscriptionFilter) (string, []any) {
	clauses := []string{"deleted_at IS NULL", "tenant_id=?"}
	args := []any{filter.TenantID}
	if filter.ApplicationID != "" {
		clauses = append(clauses, "application_id=?")
		args = append(args, filter.ApplicationID)
	}
	if filter.Status != "" {
		clauses = append(clauses, "status=?")
		args = append(args, filter.Status)
	}
	if filter.Search != "" {
		clauses = append(clauses, "LOWER(name) LIKE ?")
		args = append(args, "%"+strings.ToLower(filter.Search)+"%")
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func (r *Repository) ListActiveSubscriptions(ctx context.Context, tenantID string) ([]Subscription, error) {
	items := make([]Subscription, 0)
	query := r.db.Rebind(`SELECT ` + subscriptionColumns + ` FROM webhook_subscriptions WHERE tenant_id=? AND status='active' AND deleted_at IS NULL ORDER BY id`)
	if err := r.db.SelectContext(ctx, &items, query, tenantID); err != nil {
		return nil, fmt.Errorf("list active webhook subscriptions: %w", err)
	}
	return items, nil
}

func (r *Repository) ListActiveSubscriptionsTx(ctx context.Context, tx *sqlx.Tx, tenantID, applicationID string) ([]Subscription, error) {
	items := make([]Subscription, 0)
	query := r.db.Rebind(`SELECT ` + subscriptionColumns + ` FROM webhook_subscriptions WHERE tenant_id=? AND application_id=? AND status='active' AND deleted_at IS NULL ORDER BY id`)
	if err := tx.SelectContext(ctx, &items, query, tenantID, applicationID); err != nil {
		return nil, fmt.Errorf("list active webhook subscriptions in transaction: %w", err)
	}
	return items, nil
}

func (r *Repository) InsertDeliveryTx(ctx context.Context, tx *sqlx.Tx, value Delivery) (bool, error) {
	query := `INSERT INTO webhook_deliveries (` + deliveryColumns + `) VALUES (?,?,?,?,?,?,?,?,0,?,NULL,0,'','',NULL,1,?,?,?,?)`
	if r.db.DriverName() == "mysql" {
		query = "INSERT IGNORE" + query[len("INSERT"):]
	} else {
		query += " ON CONFLICT (subscription_id,event_id) DO NOTHING"
	}
	result, err := tx.ExecContext(ctx, r.db.Rebind(query), value.ID, value.SubscriptionID, value.TenantID, value.ApplicationID, value.EventID, value.EventSubject, value.Payload, value.Status, value.NextAttemptAt, value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy)
	if err != nil {
		return false, fmt.Errorf("insert webhook delivery: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count inserted webhook deliveries: %w", err)
	}
	return count == 1, nil
}

func (r *Repository) InsertDelivery(ctx context.Context, value Delivery) (Delivery, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return Delivery{}, fmt.Errorf("begin webhook delivery insert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	inserted, err := r.InsertDeliveryTx(ctx, tx, value)
	if err != nil {
		return Delivery{}, err
	}
	if !inserted {
		return Delivery{}, ErrDuplicate
	}
	if err := tx.Commit(); err != nil {
		return Delivery{}, fmt.Errorf("commit webhook delivery insert: %w", err)
	}
	return r.GetDelivery(ctx, value.TenantID, value.ApplicationID, value.ID)
}

func (r *Repository) GetDelivery(ctx context.Context, tenantID, applicationID, id string) (Delivery, error) {
	var value Delivery
	query := r.db.Rebind(`SELECT ` + deliveryColumns + ` FROM webhook_deliveries WHERE id=? AND tenant_id=? AND application_id=?`)
	if err := r.db.GetContext(ctx, &value, query, id, tenantID, applicationID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Delivery{}, ErrNotFound
		}
		return Delivery{}, fmt.Errorf("select webhook delivery: %w", err)
	}
	return value, nil
}

func (r *Repository) ListDeliveries(ctx context.Context, filter DeliveryFilter) (Page[Delivery], error) {
	where, args := deliveryWhere(filter)
	var total int64
	if err := r.db.GetContext(ctx, &total, r.db.Rebind(`SELECT COUNT(*) FROM webhook_deliveries `+where), args...); err != nil {
		return Page[Delivery]{}, fmt.Errorf("count webhook deliveries: %w", err)
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	query := r.db.Rebind(`SELECT ` + deliveryColumns + ` FROM webhook_deliveries ` + where + ` ORDER BY created_at DESC,id LIMIT ? OFFSET ?`)
	items := make([]Delivery, 0)
	if err := r.db.SelectContext(ctx, &items, query, args...); err != nil {
		return Page[Delivery]{}, fmt.Errorf("list webhook deliveries: %w", err)
	}
	return Page[Delivery]{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (r *Repository) DeleteTerminalDeliveriesBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, ErrInvalid
	}
	var ids []string
	query := r.db.Rebind(`SELECT id FROM webhook_deliveries WHERE status IN ('succeeded','dead') AND updated_at<? ORDER BY updated_at,id LIMIT ?`)
	if err := r.db.SelectContext(ctx, &ids, query, before, limit); err != nil {
		return 0, fmt.Errorf("select expired webhook deliveries: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	deleteQuery, args, err := sqlx.In(`DELETE FROM webhook_deliveries WHERE id IN (?) AND status IN ('succeeded','dead') AND updated_at<?`, ids, before)
	if err != nil {
		return 0, fmt.Errorf("build expired webhook delivery delete: %w", err)
	}
	result, err := r.db.ExecContext(ctx, r.db.Rebind(deleteQuery), args...)
	if err != nil {
		return 0, fmt.Errorf("delete expired webhook deliveries: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted webhook deliveries: %w", err)
	}
	return deleted, nil
}

func deliveryWhere(filter DeliveryFilter) (string, []any) {
	clauses := []string{"tenant_id=?"}
	args := []any{filter.TenantID}
	if filter.ApplicationID != "" {
		clauses = append(clauses, "application_id=?")
		args = append(args, filter.ApplicationID)
	}
	if filter.SubscriptionID != "" {
		clauses = append(clauses, "subscription_id=?")
		args = append(args, filter.SubscriptionID)
	}
	if filter.Status != "" {
		clauses = append(clauses, "status=?")
		args = append(args, filter.Status)
	}
	if filter.CreatedFrom != nil {
		clauses = append(clauses, "created_at>=?")
		args = append(args, *filter.CreatedFrom)
	}
	if filter.CreatedUntil != nil {
		clauses = append(clauses, "created_at<?")
		args = append(args, *filter.CreatedUntil)
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func (r *Repository) ClaimDeliveries(ctx context.Context, limit int, lease time.Duration, actor string) ([]Delivery, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin webhook delivery claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := r.now()
	items := make([]Delivery, 0)
	query := r.db.Rebind(`SELECT ` + deliveryColumns + ` FROM webhook_deliveries d WHERE ((d.status IN ('pending','retrying')) OR d.status='processing') AND d.next_attempt_at<=? AND EXISTS (SELECT 1 FROM webhook_subscriptions s WHERE s.id=d.subscription_id AND s.status='active' AND s.deleted_at IS NULL) ORDER BY d.next_attempt_at,d.created_at LIMIT ? FOR UPDATE SKIP LOCKED`)
	if err := tx.SelectContext(ctx, &items, query, now, limit); err != nil {
		return nil, fmt.Errorf("select webhook delivery claims: %w", err)
	}
	update := r.db.Rebind(`UPDATE webhook_deliveries SET status='processing',attempt_count=attempt_count+1,last_attempt_at=?,next_attempt_at=?,version=version+1,updated_at=?,updated_by=? WHERE id=? AND version=?`)
	for index := range items {
		result, err := tx.ExecContext(ctx, update, now, now.Add(lease), now, actor, items[index].ID, items[index].Version)
		if err := affectedOne(result, err, "claim webhook delivery"); err != nil {
			return nil, err
		}
		items[index].Status = DeliveryProcessing
		items[index].AttemptCount++
		items[index].LastAttemptAt = &now
		items[index].NextAttemptAt = now.Add(lease)
		items[index].Version++
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit webhook delivery claim: %w", err)
	}
	return items, nil
}

func (r *Repository) CompleteDelivery(ctx context.Context, value Delivery, result SendResult, actor string) error {
	now := r.now()
	query := r.db.Rebind(`UPDATE webhook_deliveries SET status='succeeded',response_status=?,response_body=?,error_message='',delivered_at=?,version=version+1,updated_at=?,updated_by=? WHERE id=? AND version=? AND status='processing'`)
	updated, err := r.db.ExecContext(ctx, query, result.StatusCode, result.Body, now, now, actor, value.ID, value.Version)
	return affectedOne(updated, err, "complete webhook delivery")
}

func (r *Repository) FailDelivery(ctx context.Context, value Delivery, result SendResult, cause error, retryAt *time.Time, actor string) error {
	now := r.now()
	status := DeliveryDead
	next := value.NextAttemptAt
	if retryAt != nil {
		status = DeliveryRetrying
		next = *retryAt
	}
	message := ""
	if cause != nil {
		message = truncateText(cause.Error(), 4096)
	}
	query := r.db.Rebind(`UPDATE webhook_deliveries SET status=?,next_attempt_at=?,response_status=?,response_body=?,error_message=?,version=version+1,updated_at=?,updated_by=? WHERE id=? AND version=? AND status='processing'`)
	updated, err := r.db.ExecContext(ctx, query, status, next, result.StatusCode, result.Body, message, now, actor, value.ID, value.Version)
	return affectedOne(updated, err, "fail webhook delivery")
}

func (r *Repository) ReplayDelivery(ctx context.Context, tenantID, applicationID, id string, expectedVersion int64, actor string) (Delivery, error) {
	now := r.now()
	query := r.db.Rebind(`UPDATE webhook_deliveries SET status='pending',attempt_count=0,next_attempt_at=?,last_attempt_at=NULL,response_status=0,response_body='',error_message='',delivered_at=NULL,version=version+1,updated_at=?,updated_by=? WHERE id=? AND tenant_id=? AND application_id=? AND version=? AND status IN ('succeeded','dead')`)
	result, err := r.db.ExecContext(ctx, query, now, now, actor, id, tenantID, applicationID, expectedVersion)
	if err := affectedOne(result, err, "replay webhook delivery"); err != nil {
		return Delivery{}, err
	}
	return r.GetDelivery(ctx, tenantID, applicationID, id)
}

func affectedOne(result sql.Result, err error, action string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count rows after %s: %w", action, err)
	}
	if count != 1 {
		return ErrVersionConflict
	}
	return nil
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && value[limit]&0xc0 == 0x80 {
		limit--
	}
	return value[:limit]
}

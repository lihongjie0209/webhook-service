package webhook

import (
	"errors"
	"time"
)

var (
	ErrNotFound        = errors.New("webhook resource not found")
	ErrVersionConflict = errors.New("webhook version conflict")
	ErrDuplicate       = errors.New("webhook resource already exists")
	ErrInvalid         = errors.New("invalid webhook request")
	ErrActorRequired   = errors.New("authenticated actor is required")
	ErrForbidden       = errors.New("webhook tenant access denied")
)

const (
	SubscriptionActive   = "active"
	SubscriptionPaused   = "paused"
	SubscriptionDisabled = "disabled"

	DeliveryPending    = "pending"
	DeliveryProcessing = "processing"
	DeliverySucceeded  = "succeeded"
	DeliveryRetrying   = "retrying"
	DeliveryDead       = "dead"
)

type Subscription struct {
	ID                  string     `db:"id" json:"id"`
	TenantID            string     `db:"tenant_id" json:"tenant_id"`
	ApplicationID       string     `db:"application_id" json:"application_id"`
	Name                string     `db:"name" json:"name"`
	EndpointURL         string     `db:"endpoint_url" json:"endpoint_url"`
	SubjectFilter       string     `db:"subject_filter" json:"subject_filter"`
	Status              string     `db:"status" json:"status"`
	TimeoutMS           int        `db:"timeout_ms" json:"timeout_ms"`
	MaxAttempts         int        `db:"max_attempts" json:"max_attempts"`
	RetryInitialSeconds int        `db:"retry_initial_seconds" json:"retry_initial_seconds"`
	SecretCiphertext    []byte     `db:"secret_ciphertext" json:"-"`
	SecretKeyID         string     `db:"secret_key_id" json:"-"`
	SecretVersion       int64      `db:"secret_version" json:"secret_version"`
	Version             int64      `db:"version" json:"version"`
	CreatedAt           time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at" json:"updated_at"`
	CreatedBy           string     `db:"created_by" json:"created_by"`
	UpdatedBy           string     `db:"updated_by" json:"updated_by"`
	DeletedAt           *time.Time `db:"deleted_at" json:"-"`
}

type Delivery struct {
	ID             string     `db:"id" json:"id"`
	SubscriptionID string     `db:"subscription_id" json:"subscription_id"`
	TenantID       string     `db:"tenant_id" json:"tenant_id"`
	ApplicationID  string     `db:"application_id" json:"application_id"`
	EventID        string     `db:"event_id" json:"event_id"`
	EventSubject   string     `db:"event_subject" json:"event_subject"`
	Payload        []byte     `db:"payload" json:"-"`
	Status         string     `db:"status" json:"status"`
	AttemptCount   int        `db:"attempt_count" json:"attempt_count"`
	NextAttemptAt  time.Time  `db:"next_attempt_at" json:"next_attempt_at"`
	LastAttemptAt  *time.Time `db:"last_attempt_at" json:"last_attempt_at,omitempty"`
	ResponseStatus int        `db:"response_status" json:"response_status"`
	ResponseBody   string     `db:"response_body" json:"response_body"`
	ErrorMessage   string     `db:"error_message" json:"error_message"`
	DeliveredAt    *time.Time `db:"delivered_at" json:"delivered_at,omitempty"`
	Version        int64      `db:"version" json:"version"`
	CreatedAt      time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at" json:"updated_at"`
	CreatedBy      string     `db:"created_by" json:"created_by"`
	UpdatedBy      string     `db:"updated_by" json:"updated_by"`
}

type SubscriptionFilter struct {
	TenantID      string
	ApplicationID string
	Status        string
	Search        string
	Page          int
	PageSize      int
}

type DeliveryFilter struct {
	TenantID       string
	ApplicationID  string
	SubscriptionID string
	Status         string
	CreatedFrom    *time.Time
	CreatedUntil   *time.Time
	Page           int
	PageSize       int
}

type Page[T any] struct {
	Items    []T
	Total    int64
	Page     int
	PageSize int
}

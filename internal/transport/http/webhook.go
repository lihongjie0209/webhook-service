package httptransport

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lihongjie0209/webhook-service/internal/apperror"
	webhookdomain "github.com/lihongjie0209/webhook-service/internal/webhook"
)

type PageRequest struct {
	Page     int `json:"page" example:"1"`
	PageSize int `json:"page_size" example:"20"`
}

type PageResult struct {
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

type SubscriptionBody struct {
	ID                  string    `json:"id"`
	TenantID            string    `json:"tenant_id"`
	ApplicationID       string    `json:"application_id"`
	Name                string    `json:"name"`
	EndpointURL         string    `json:"endpoint_url"`
	SubjectFilter       string    `json:"subject_filter"`
	Status              string    `json:"status"`
	TimeoutMS           int       `json:"timeout_ms"`
	MaxAttempts         int       `json:"max_attempts"`
	RetryInitialSeconds int       `json:"retry_initial_seconds"`
	SecretVersion       int64     `json:"secret_version"`
	Version             int64     `json:"version"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
	CreatedBy           string    `json:"created_by"`
	UpdatedBy           string    `json:"updated_by"`
}

type DeliveryBody struct {
	ID             string     `json:"id"`
	SubscriptionID string     `json:"subscription_id"`
	TenantID       string     `json:"tenant_id"`
	EventID        string     `json:"event_id"`
	EventSubject   string     `json:"event_subject"`
	Status         string     `json:"status"`
	AttemptCount   int        `json:"attempt_count"`
	NextAttemptAt  time.Time  `json:"next_attempt_at"`
	LastAttemptAt  *time.Time `json:"last_attempt_at,omitempty"`
	ResponseStatus int        `json:"response_status"`
	ResponseBody   string     `json:"response_body"`
	ErrorMessage   string     `json:"error_message"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	Version        int64      `json:"version"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	CreatedBy      string     `json:"created_by"`
	UpdatedBy      string     `json:"updated_by"`
}

type CreateSubscriptionRequest struct {
	TenantID            string `json:"tenant_id" binding:"required"`
	ApplicationID       string `json:"application_id"`
	Name                string `json:"name" binding:"required"`
	EndpointURL         string `json:"endpoint_url" binding:"required" example:"https://hooks.example.com/events"`
	SubjectFilter       string `json:"subject_filter" binding:"required" example:"platform.identity.user.>"`
	TimeoutMS           int    `json:"timeout_ms" example:"5000"`
	MaxAttempts         int    `json:"max_attempts" example:"5"`
	RetryInitialSeconds int    `json:"retry_initial_seconds" example:"5"`
}

type CreateSubscriptionBody struct {
	Subscription  SubscriptionBody `json:"subscription"`
	SigningSecret string           `json:"signing_secret"`
}

type UpdateSubscriptionRequest struct {
	ID                  string `json:"id" binding:"required"`
	TenantID            string `json:"tenant_id" binding:"required"`
	Name                string `json:"name" binding:"required"`
	EndpointURL         string `json:"endpoint_url" binding:"required"`
	SubjectFilter       string `json:"subject_filter" binding:"required"`
	Status              string `json:"status" binding:"required" enums:"active,paused,disabled"`
	TimeoutMS           int    `json:"timeout_ms" binding:"required"`
	MaxAttempts         int    `json:"max_attempts" binding:"required"`
	RetryInitialSeconds int    `json:"retry_initial_seconds" binding:"required"`
	ExpectedVersion     int64  `json:"expected_version" binding:"required"`
}

type ResourceRequest struct {
	ID       string `json:"id" binding:"required"`
	TenantID string `json:"tenant_id" binding:"required"`
}

type VersionedResourceRequest struct {
	ID              string `json:"id" binding:"required"`
	TenantID        string `json:"tenant_id" binding:"required"`
	ExpectedVersion int64  `json:"expected_version" binding:"required"`
}

type ListSubscriptionsRequest struct {
	TenantID      string      `json:"tenant_id" binding:"required"`
	ApplicationID string      `json:"application_id"`
	Status        string      `json:"status" enums:"active,paused,disabled"`
	Search        string      `json:"search"`
	Page          PageRequest `json:"page"`
}

type SubscriptionPageBody struct {
	Items []SubscriptionBody `json:"items"`
	Page  PageResult         `json:"page"`
}

type RotateSecretBody struct {
	Subscription  SubscriptionBody `json:"subscription"`
	SigningSecret string           `json:"signing_secret"`
}

type TestSubscriptionRequest struct {
	ID          string          `json:"id" binding:"required"`
	TenantID    string          `json:"tenant_id" binding:"required"`
	PayloadJSON json.RawMessage `json:"payload_json" binding:"required" swaggertype:"object"`
}

type ListDeliveriesRequest struct {
	TenantID       string      `json:"tenant_id" binding:"required"`
	SubscriptionID string      `json:"subscription_id"`
	Status         string      `json:"status" enums:"pending,processing,succeeded,retrying,dead"`
	CreatedFrom    string      `json:"created_from" example:"2026-08-01T00:00:00+08:00"`
	CreatedUntil   string      `json:"created_until" example:"2026-09-01T00:00:00+08:00"`
	Page           PageRequest `json:"page"`
}

type DeliveryPageBody struct {
	Items []DeliveryBody `json:"items"`
	Page  PageResult     `json:"page"`
}

// CreateSubscription godoc
// @Summary Create an external webhook subscription
// @Tags webhooks
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body CreateSubscriptionRequest true "Subscription"
// @Success 200 {object} Response{body=CreateSubscriptionBody}
// @Failure 400 {object} Response "Code 10001: invalid endpoint or subject filter"
// @Router /api/v1/webhooks/subscriptions/create [post]
func (h *Handler) CreateSubscription(c *gin.Context) {
	var request CreateSubscriptionRequest
	if !h.bind(c, &request) || !h.available(c) {
		return
	}
	value, secret, err := h.webhook.CreateSubscription(c.Request.Context(), webhookdomain.CreateSubscriptionInput{
		TenantID: request.TenantID, ApplicationID: request.ApplicationID, Name: request.Name, EndpointURL: request.EndpointURL,
		SubjectFilter: request.SubjectFilter, TimeoutMS: request.TimeoutMS, MaxAttempts: request.MaxAttempts, RetryInitialSeconds: request.RetryInitialSeconds,
	})
	if err != nil {
		h.failWebhook(c, err)
		return
	}
	OK(c, CreateSubscriptionBody{Subscription: subscriptionBody(value), SigningSecret: secret})
}

// UpdateSubscription godoc
// @Summary Update a webhook subscription with optimistic locking
// @Tags webhooks
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body UpdateSubscriptionRequest true "Subscription"
// @Success 200 {object} Response{body=SubscriptionBody}
// @Failure 409 {object} Response "Code 30009: stale version"
// @Router /api/v1/webhooks/subscriptions/update [post]
func (h *Handler) UpdateSubscription(c *gin.Context) {
	var request UpdateSubscriptionRequest
	if !h.bind(c, &request) || !h.available(c) {
		return
	}
	value, err := h.webhook.UpdateSubscription(c.Request.Context(), webhookdomain.UpdateSubscriptionInput{
		ID: request.ID, TenantID: request.TenantID, Name: request.Name, EndpointURL: request.EndpointURL,
		SubjectFilter: request.SubjectFilter, Status: request.Status, TimeoutMS: request.TimeoutMS,
		MaxAttempts: request.MaxAttempts, RetryInitialSeconds: request.RetryInitialSeconds, ExpectedVersion: request.ExpectedVersion,
	})
	if err != nil {
		h.failWebhook(c, err)
		return
	}
	OK(c, subscriptionBody(value))
}

// GetSubscription godoc
// @Summary Get one tenant webhook subscription
// @Tags webhooks
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ResourceRequest true "Identity"
// @Success 200 {object} Response{body=SubscriptionBody}
// @Router /api/v1/webhooks/subscriptions/get [post]
func (h *Handler) GetSubscription(c *gin.Context) {
	var request ResourceRequest
	if !h.bind(c, &request) || !h.available(c) {
		return
	}
	value, err := h.webhook.GetSubscription(c.Request.Context(), request.TenantID, request.ID)
	if err != nil {
		h.failWebhook(c, err)
		return
	}
	OK(c, subscriptionBody(value))
}

// ListSubscriptions godoc
// @Summary Search tenant webhook subscriptions
// @Tags webhooks
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ListSubscriptionsRequest true "Filter and pagination"
// @Success 200 {object} Response{body=SubscriptionPageBody}
// @Router /api/v1/webhooks/subscriptions/list [post]
func (h *Handler) ListSubscriptions(c *gin.Context) {
	var request ListSubscriptionsRequest
	if !h.bind(c, &request) || !h.available(c) {
		return
	}
	result, err := h.webhook.ListSubscriptions(c.Request.Context(), webhookdomain.SubscriptionFilter{
		TenantID: request.TenantID, ApplicationID: request.ApplicationID, Status: request.Status, Search: request.Search,
		Page: request.Page.Page, PageSize: request.Page.PageSize,
	})
	if err != nil {
		h.failWebhook(c, err)
		return
	}
	OK(c, SubscriptionPageBody{Items: subscriptionBodies(result.Items), Page: PageResult{Total: result.Total, Page: result.Page, PageSize: result.PageSize}})
}

// RotateSubscriptionSecret godoc
// @Summary Rotate and return a webhook signing secret once
// @Tags webhooks
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body VersionedResourceRequest true "Identity and current version"
// @Success 200 {object} Response{body=RotateSecretBody}
// @Router /api/v1/webhooks/subscriptions/rotate-secret [post]
func (h *Handler) RotateSubscriptionSecret(c *gin.Context) {
	var request VersionedResourceRequest
	if !h.bind(c, &request) || !h.available(c) {
		return
	}
	value, secret, err := h.webhook.RotateSecret(c.Request.Context(), request.TenantID, request.ID, request.ExpectedVersion)
	if err != nil {
		h.failWebhook(c, err)
		return
	}
	OK(c, RotateSecretBody{Subscription: subscriptionBody(value), SigningSecret: secret})
}

// DeleteSubscription godoc
// @Summary Soft-delete a webhook subscription
// @Tags webhooks
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body VersionedResourceRequest true "Identity and current version"
// @Success 200 {object} Response
// @Router /api/v1/webhooks/subscriptions/delete [post]
func (h *Handler) DeleteSubscription(c *gin.Context) {
	var request VersionedResourceRequest
	if !h.bind(c, &request) || !h.available(c) {
		return
	}
	if err := h.webhook.DeleteSubscription(c.Request.Context(), request.TenantID, request.ID, request.ExpectedVersion); err != nil {
		h.failWebhook(c, err)
		return
	}
	OK(c, nil)
}

// TestSubscription godoc
// @Summary Queue a synthetic delivery for a subscription
// @Tags webhooks
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body TestSubscriptionRequest true "Test payload"
// @Success 200 {object} Response{body=DeliveryBody}
// @Router /api/v1/webhooks/subscriptions/test [post]
func (h *Handler) TestSubscription(c *gin.Context) {
	var request TestSubscriptionRequest
	if !h.bind(c, &request) || !h.available(c) {
		return
	}
	value, err := h.webhook.TestSubscription(c.Request.Context(), request.TenantID, request.ID, request.PayloadJSON)
	if err != nil {
		h.failWebhook(c, err)
		return
	}
	OK(c, deliveryBody(value))
}

// GetDelivery godoc
// @Summary Get a webhook delivery record
// @Tags webhook-deliveries
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ResourceRequest true "Identity"
// @Success 200 {object} Response{body=DeliveryBody}
// @Router /api/v1/webhooks/deliveries/get [post]
func (h *Handler) GetDelivery(c *gin.Context) {
	var request ResourceRequest
	if !h.bind(c, &request) || !h.available(c) {
		return
	}
	value, err := h.webhook.GetDelivery(c.Request.Context(), request.TenantID, request.ID)
	if err != nil {
		h.failWebhook(c, err)
		return
	}
	OK(c, deliveryBody(value))
}

// ListDeliveries godoc
// @Summary Search webhook delivery history
// @Tags webhook-deliveries
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ListDeliveriesRequest true "Filter and pagination"
// @Success 200 {object} Response{body=DeliveryPageBody}
// @Router /api/v1/webhooks/deliveries/list [post]
func (h *Handler) ListDeliveries(c *gin.Context) {
	var request ListDeliveriesRequest
	if !h.bind(c, &request) || !h.available(c) {
		return
	}
	createdFrom, err := optionalTime(request.CreatedFrom)
	if err != nil {
		h.failWebhook(c, err)
		return
	}
	createdUntil, err := optionalTime(request.CreatedUntil)
	if err != nil {
		h.failWebhook(c, err)
		return
	}
	result, err := h.webhook.ListDeliveries(c.Request.Context(), webhookdomain.DeliveryFilter{
		TenantID: request.TenantID, SubscriptionID: request.SubscriptionID, Status: request.Status,
		CreatedFrom: createdFrom, CreatedUntil: createdUntil, Page: request.Page.Page, PageSize: request.Page.PageSize,
	})
	if err != nil {
		h.failWebhook(c, err)
		return
	}
	OK(c, DeliveryPageBody{Items: deliveryBodies(result.Items), Page: PageResult{Total: result.Total, Page: result.Page, PageSize: result.PageSize}})
}

// ReplayDelivery godoc
// @Summary Replay a completed or dead webhook delivery
// @Tags webhook-deliveries
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body VersionedResourceRequest true "Identity and current version"
// @Success 200 {object} Response{body=DeliveryBody}
// @Router /api/v1/webhooks/deliveries/replay [post]
func (h *Handler) ReplayDelivery(c *gin.Context) {
	var request VersionedResourceRequest
	if !h.bind(c, &request) || !h.available(c) {
		return
	}
	value, err := h.webhook.ReplayDelivery(c.Request.Context(), request.TenantID, request.ID, request.ExpectedVersion)
	if err != nil {
		h.failWebhook(c, err)
		return
	}
	OK(c, deliveryBody(value))
}

func (h *Handler) bind(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid request body", err))
		return false
	}
	return true
}

func (h *Handler) available(c *gin.Context) bool {
	if h.webhook == nil {
		Fail(c, h.logger, apperror.Unavailable("webhook service is unavailable", nil))
		return false
	}
	return true
}

func (h *Handler) failWebhook(c *gin.Context, err error) {
	switch {
	case errors.Is(err, webhookdomain.ErrInvalid):
		Fail(c, h.logger, apperror.Invalid("invalid webhook request", err))
	case errors.Is(err, webhookdomain.ErrNotFound):
		Fail(c, h.logger, apperror.NotFound("webhook resource not found"))
	case errors.Is(err, webhookdomain.ErrVersionConflict), errors.Is(err, webhookdomain.ErrDuplicate):
		Fail(c, h.logger, apperror.Conflict("webhook resource conflict", err))
	default:
		Fail(c, h.logger, apperror.Internal(err))
	}
}

func optionalTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, webhookdomain.ErrInvalid
	}
	return &parsed, nil
}

func subscriptionBody(value webhookdomain.Subscription) SubscriptionBody {
	return SubscriptionBody{
		ID: value.ID, TenantID: value.TenantID, ApplicationID: value.ApplicationID, Name: value.Name,
		EndpointURL: value.EndpointURL, SubjectFilter: value.SubjectFilter, Status: value.Status,
		TimeoutMS: value.TimeoutMS, MaxAttempts: value.MaxAttempts, RetryInitialSeconds: value.RetryInitialSeconds,
		SecretVersion: value.SecretVersion, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy,
	}
}

func subscriptionBodies(values []webhookdomain.Subscription) []SubscriptionBody {
	result := make([]SubscriptionBody, 0, len(values))
	for _, value := range values {
		result = append(result, subscriptionBody(value))
	}
	return result
}

func deliveryBody(value webhookdomain.Delivery) DeliveryBody {
	return DeliveryBody{
		ID: value.ID, SubscriptionID: value.SubscriptionID, TenantID: value.TenantID, EventID: value.EventID,
		EventSubject: value.EventSubject, Status: value.Status, AttemptCount: value.AttemptCount,
		NextAttemptAt: value.NextAttemptAt, LastAttemptAt: value.LastAttemptAt, ResponseStatus: value.ResponseStatus,
		ResponseBody: value.ResponseBody, ErrorMessage: value.ErrorMessage, DeliveredAt: value.DeliveredAt,
		Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy,
	}
}

func deliveryBodies(values []webhookdomain.Delivery) []DeliveryBody {
	result := make([]DeliveryBody, 0, len(values))
	for _, value := range values {
		result = append(result, deliveryBody(value))
	}
	return result
}

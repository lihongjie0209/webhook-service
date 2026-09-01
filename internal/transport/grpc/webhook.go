package grpctransport

import (
	"context"
	"errors"
	"time"

	"github.com/lihongjie0209/microservice-platform-go/appaccess"
	commonv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/common/v1"
	webhookv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/webhook/v1"
	webhookdomain "github.com/lihongjie0209/webhook-service/internal/webhook"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type webhookServer struct {
	webhookv1.UnimplementedWebhookServiceServer
	service *webhookdomain.Service
}

func (s *webhookServer) CreateSubscription(ctx context.Context, request *webhookv1.CreateSubscriptionRequest) (*webhookv1.CreateSubscriptionResponse, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	value, secret, err := s.service.CreateSubscription(ctx, webhookdomain.CreateSubscriptionInput{
		TenantID: request.GetTenantId(), ApplicationID: request.GetApplicationId(), Name: request.GetName(),
		EndpointURL: request.GetEndpointUrl(), SubjectFilter: request.GetSubjectFilter(), TimeoutMS: int(request.GetTimeoutMs()),
		MaxAttempts: int(request.GetMaxAttempts()), RetryInitialSeconds: int(request.GetRetryInitialSeconds()),
	})
	if err != nil {
		return nil, grpcError(err)
	}
	return &webhookv1.CreateSubscriptionResponse{Subscription: subscriptionProto(value), SigningSecret: secret}, nil
}

func (s *webhookServer) UpdateSubscription(ctx context.Context, request *webhookv1.UpdateSubscriptionRequest) (*webhookv1.UpdateSubscriptionResponse, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	value, err := s.service.UpdateSubscription(ctx, webhookdomain.UpdateSubscriptionInput{
		ID: request.GetId(), TenantID: request.GetTenantId(), ApplicationID: request.GetApplicationId(), Name: request.GetName(), EndpointURL: request.GetEndpointUrl(),
		SubjectFilter: request.GetSubjectFilter(), Status: subscriptionStatusDomain(request.GetStatus()), TimeoutMS: int(request.GetTimeoutMs()),
		MaxAttempts: int(request.GetMaxAttempts()), RetryInitialSeconds: int(request.GetRetryInitialSeconds()), ExpectedVersion: request.GetExpectedVersion(),
	})
	if err != nil {
		return nil, grpcError(err)
	}
	return &webhookv1.UpdateSubscriptionResponse{Subscription: subscriptionProto(value)}, nil
}

func (s *webhookServer) GetSubscription(ctx context.Context, request *webhookv1.GetSubscriptionRequest) (*webhookv1.GetSubscriptionResponse, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	value, err := s.service.GetSubscription(ctx, request.GetTenantId(), request.GetApplicationId(), request.GetId())
	if err != nil {
		return nil, grpcError(err)
	}
	return &webhookv1.GetSubscriptionResponse{Subscription: subscriptionProto(value)}, nil
}

func (s *webhookServer) ListSubscriptions(ctx context.Context, request *webhookv1.ListSubscriptionsRequest) (*webhookv1.ListSubscriptionsResponse, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	page, pageSize := pageRequest(request.GetPage())
	result, err := s.service.ListSubscriptions(ctx, webhookdomain.SubscriptionFilter{
		TenantID: request.GetTenantId(), ApplicationID: request.GetApplicationId(), Status: subscriptionStatusDomain(request.GetStatus()),
		Search: request.GetSearch(), Page: page, PageSize: pageSize,
	})
	if err != nil {
		return nil, grpcError(err)
	}
	items := make([]*webhookv1.Subscription, 0, len(result.Items))
	for _, value := range result.Items {
		items = append(items, subscriptionProto(value))
	}
	return &webhookv1.ListSubscriptionsResponse{Subscriptions: items, Page: pageProto(result.Total, result.Page, result.PageSize)}, nil
}

func (s *webhookServer) RotateSubscriptionSecret(ctx context.Context, request *webhookv1.RotateSubscriptionSecretRequest) (*webhookv1.RotateSubscriptionSecretResponse, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	value, secret, err := s.service.RotateSecret(ctx, request.GetTenantId(), request.GetApplicationId(), request.GetId(), request.GetExpectedVersion())
	if err != nil {
		return nil, grpcError(err)
	}
	return &webhookv1.RotateSubscriptionSecretResponse{Subscription: subscriptionProto(value), SigningSecret: secret}, nil
}

func (s *webhookServer) DeleteSubscription(ctx context.Context, request *webhookv1.DeleteSubscriptionRequest) (*webhookv1.DeleteSubscriptionResponse, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	if err := s.service.DeleteSubscription(ctx, request.GetTenantId(), request.GetApplicationId(), request.GetId(), request.GetExpectedVersion()); err != nil {
		return nil, grpcError(err)
	}
	return &webhookv1.DeleteSubscriptionResponse{}, nil
}

func (s *webhookServer) TestSubscription(ctx context.Context, request *webhookv1.TestSubscriptionRequest) (*webhookv1.TestSubscriptionResponse, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	value, err := s.service.TestSubscription(ctx, request.GetTenantId(), request.GetApplicationId(), request.GetId(), []byte(request.GetPayloadJson()))
	if err != nil {
		return nil, grpcError(err)
	}
	return &webhookv1.TestSubscriptionResponse{Delivery: deliveryProto(value)}, nil
}

func (s *webhookServer) GetDelivery(ctx context.Context, request *webhookv1.GetDeliveryRequest) (*webhookv1.GetDeliveryResponse, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	value, err := s.service.GetDelivery(ctx, request.GetTenantId(), request.GetApplicationId(), request.GetId())
	if err != nil {
		return nil, grpcError(err)
	}
	return &webhookv1.GetDeliveryResponse{Delivery: deliveryProto(value)}, nil
}

func (s *webhookServer) ListDeliveries(ctx context.Context, request *webhookv1.ListDeliveriesRequest) (*webhookv1.ListDeliveriesResponse, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	page, pageSize := pageRequest(request.GetPage())
	result, err := s.service.ListDeliveries(ctx, webhookdomain.DeliveryFilter{
		TenantID: request.GetTenantId(), ApplicationID: request.GetApplicationId(), SubscriptionID: request.GetSubscriptionId(), Status: deliveryStatusDomain(request.GetStatus()),
		CreatedFrom: timePointer(request.GetCreatedFrom()), CreatedUntil: timePointer(request.GetCreatedUntil()), Page: page, PageSize: pageSize,
	})
	if err != nil {
		return nil, grpcError(err)
	}
	items := make([]*webhookv1.Delivery, 0, len(result.Items))
	for _, value := range result.Items {
		items = append(items, deliveryProto(value))
	}
	return &webhookv1.ListDeliveriesResponse{Deliveries: items, Page: pageProto(result.Total, result.Page, result.PageSize)}, nil
}

func (s *webhookServer) ReplayDelivery(ctx context.Context, request *webhookv1.ReplayDeliveryRequest) (*webhookv1.ReplayDeliveryResponse, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	value, err := s.service.ReplayDelivery(ctx, request.GetTenantId(), request.GetApplicationId(), request.GetId(), request.GetExpectedVersion())
	if err != nil {
		return nil, grpcError(err)
	}
	return &webhookv1.ReplayDeliveryResponse{Delivery: deliveryProto(value)}, nil
}

func (s *webhookServer) available() error {
	if s.service == nil {
		return status.Error(codes.Unavailable, "webhook service is unavailable")
	}
	return nil
}

func grpcError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(err).Err()
	case errors.Is(err, webhookdomain.ErrNotFound):
		return status.Error(codes.NotFound, "webhook resource not found")
	case errors.Is(err, webhookdomain.ErrVersionConflict):
		return status.Error(codes.Aborted, "webhook resource version is stale")
	case errors.Is(err, webhookdomain.ErrDuplicate):
		return status.Error(codes.AlreadyExists, "webhook resource already exists")
	case errors.Is(err, webhookdomain.ErrInvalid):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, webhookdomain.ErrActorRequired):
		return status.Error(codes.Unauthenticated, "authenticated actor is required")
	case errors.Is(err, webhookdomain.ErrForbidden):
		return status.Error(codes.PermissionDenied, "webhook tenant access denied")
	case errors.Is(err, appaccess.ErrUnavailable):
		return status.Error(codes.Unavailable, "application authorization is unavailable")
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}

func subscriptionProto(value webhookdomain.Subscription) *webhookv1.Subscription {
	return &webhookv1.Subscription{
		Id: value.ID, TenantId: value.TenantID, ApplicationId: value.ApplicationID, Name: value.Name,
		EndpointUrl: value.EndpointURL, SubjectFilter: value.SubjectFilter, Status: subscriptionStatusProto(value.Status),
		TimeoutMs: uint32(value.TimeoutMS), MaxAttempts: uint32(value.MaxAttempts), RetryInitialSeconds: uint32(value.RetryInitialSeconds),
		SecretVersion: uint64(value.SecretVersion), Version: value.Version, CreatedAt: timestamppb.New(value.CreatedAt),
		UpdatedAt: timestamppb.New(value.UpdatedAt), CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy,
	}
}

func deliveryProto(value webhookdomain.Delivery) *webhookv1.Delivery {
	return &webhookv1.Delivery{
		Id: value.ID, SubscriptionId: value.SubscriptionID, TenantId: value.TenantID, ApplicationId: value.ApplicationID, EventId: value.EventID,
		EventSubject: value.EventSubject, Status: deliveryStatusProto(value.Status), AttemptCount: uint32(value.AttemptCount),
		NextAttemptAt: timestamppb.New(value.NextAttemptAt), LastAttemptAt: timestampPointer(value.LastAttemptAt),
		ResponseStatus: int32(value.ResponseStatus), ResponseBody: value.ResponseBody, ErrorMessage: value.ErrorMessage,
		DeliveredAt: timestampPointer(value.DeliveredAt), Version: value.Version, CreatedAt: timestamppb.New(value.CreatedAt),
		UpdatedAt: timestamppb.New(value.UpdatedAt), CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy,
	}
}

func subscriptionStatusDomain(value webhookv1.SubscriptionStatus) string {
	switch value {
	case webhookv1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE:
		return webhookdomain.SubscriptionActive
	case webhookv1.SubscriptionStatus_SUBSCRIPTION_STATUS_PAUSED:
		return webhookdomain.SubscriptionPaused
	case webhookv1.SubscriptionStatus_SUBSCRIPTION_STATUS_DISABLED:
		return webhookdomain.SubscriptionDisabled
	default:
		return ""
	}
}

func subscriptionStatusProto(value string) webhookv1.SubscriptionStatus {
	switch value {
	case webhookdomain.SubscriptionActive:
		return webhookv1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE
	case webhookdomain.SubscriptionPaused:
		return webhookv1.SubscriptionStatus_SUBSCRIPTION_STATUS_PAUSED
	case webhookdomain.SubscriptionDisabled:
		return webhookv1.SubscriptionStatus_SUBSCRIPTION_STATUS_DISABLED
	default:
		return webhookv1.SubscriptionStatus_SUBSCRIPTION_STATUS_UNSPECIFIED
	}
}

func deliveryStatusDomain(value webhookv1.DeliveryStatus) string {
	switch value {
	case webhookv1.DeliveryStatus_DELIVERY_STATUS_PENDING:
		return webhookdomain.DeliveryPending
	case webhookv1.DeliveryStatus_DELIVERY_STATUS_PROCESSING:
		return webhookdomain.DeliveryProcessing
	case webhookv1.DeliveryStatus_DELIVERY_STATUS_SUCCEEDED:
		return webhookdomain.DeliverySucceeded
	case webhookv1.DeliveryStatus_DELIVERY_STATUS_RETRYING:
		return webhookdomain.DeliveryRetrying
	case webhookv1.DeliveryStatus_DELIVERY_STATUS_DEAD:
		return webhookdomain.DeliveryDead
	default:
		return ""
	}
}

func deliveryStatusProto(value string) webhookv1.DeliveryStatus {
	return map[string]webhookv1.DeliveryStatus{
		webhookdomain.DeliveryPending:    webhookv1.DeliveryStatus_DELIVERY_STATUS_PENDING,
		webhookdomain.DeliveryProcessing: webhookv1.DeliveryStatus_DELIVERY_STATUS_PROCESSING,
		webhookdomain.DeliverySucceeded:  webhookv1.DeliveryStatus_DELIVERY_STATUS_SUCCEEDED,
		webhookdomain.DeliveryRetrying:   webhookv1.DeliveryStatus_DELIVERY_STATUS_RETRYING,
		webhookdomain.DeliveryDead:       webhookv1.DeliveryStatus_DELIVERY_STATUS_DEAD,
	}[value]
}

func pageRequest(value *commonv1.PageRequest) (int, int) {
	if value == nil {
		return 1, 20
	}
	return int(value.GetPage()), int(value.GetPageSize())
}

func pageProto(total int64, page, pageSize int) *commonv1.PageResult {
	return &commonv1.PageResult{Total: uint64(total), Page: uint32(page), PageSize: uint32(pageSize)}
}

func timePointer(value *timestamppb.Timestamp) *time.Time {
	if value == nil {
		return nil
	}
	timestamp := value.AsTime()
	return &timestamp
}

func timestampPointer(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(*value)
}

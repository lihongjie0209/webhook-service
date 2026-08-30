package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"

	"github.com/jmoiron/sqlx"
	platforminbox "github.com/lihongjie0209/microservice-platform-go/inbox"
	"github.com/lihongjie0209/webhook-service/internal/config"
	"github.com/lihongjie0209/webhook-service/internal/eventbus"
	"github.com/lihongjie0209/webhook-service/internal/webhook"
	"go.uber.org/fx"
)

func newWebhookRepository(db *sqlx.DB) (*webhook.Repository, error) {
	if db == nil {
		return nil, nil
	}
	return webhook.NewRepository(db)
}

func newWebhookSecretBox(cfg config.Config) (*webhook.SecretBox, error) {
	if cfg.Webhook.EncryptionKey == "" {
		return nil, nil
	}
	return webhook.NewSecretBox(cfg.Webhook.EncryptionKey, cfg.Webhook.EncryptionKeyID)
}

func newWebhookURLPolicy(cfg config.Config) (*webhook.URLPolicy, error) {
	return webhook.NewURLPolicy(net.DefaultResolver, cfg.Webhook.AllowHTTP, cfg.Webhook.AllowedPorts)
}

func newWebhookSender(policy *webhook.URLPolicy, cfg config.Config) (*webhook.Sender, error) {
	return webhook.NewSender(policy, cfg.Webhook.MaxResponseBytes)
}

func newWebhookService(repository *webhook.Repository, box *webhook.SecretBox, policy *webhook.URLPolicy) (*webhook.Service, error) {
	if repository == nil || box == nil {
		return nil, nil
	}
	return webhook.NewService(repository, box, policy)
}

func newWebhookDispatcher(repository *webhook.Repository, sender *webhook.Sender, box *webhook.SecretBox, cfg config.Config, logger *slog.Logger) (*webhook.Dispatcher, error) {
	if repository == nil || box == nil {
		return nil, nil
	}
	return webhook.NewDispatcher(repository, sender, box, logger, cfg.Webhook.PollInterval, cfg.Webhook.ClaimLease, cfg.Webhook.Workers, cfg.Webhook.BatchSize)
}

func newWebhookRetentionCleaner(repository *webhook.Repository, cfg config.Config, logger *slog.Logger) (*webhook.RetentionCleaner, error) {
	if repository == nil {
		return nil, nil
	}
	return webhook.NewRetentionCleaner(repository, logger, cfg.Webhook.Retention, cfg.Webhook.CleanupInterval, cfg.Webhook.BatchSize)
}

func newWebhookInbox(db *sqlx.DB, cfg config.Config) (*platforminbox.SQLStore, error) {
	if db == nil {
		return nil, nil
	}
	dialect := platforminbox.DialectPostgres
	switch cfg.Database.Type {
	case "mysql":
		dialect = platforminbox.DialectMySQL
	case "kingbase":
		dialect = platforminbox.DialectKingbase
	}
	return platforminbox.NewSQLStore(db, dialect, "webhook_event_inbox")
}

func startWebhookRuntime(lifecycle fx.Lifecycle, bus *eventbus.Bus, inbox *platforminbox.SQLStore, service *webhook.Service, dispatcher *webhook.Dispatcher, cleaner *webhook.RetentionCleaner, logger *slog.Logger) {
	var cancel context.CancelFunc
	var workers sync.WaitGroup
	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			runContext, stop := context.WithCancel(context.Background())
			cancel = stop
			if dispatcher != nil {
				workers.Add(1)
				go func() {
					defer workers.Done()
					if err := dispatcher.Run(runContext); err != nil && !errors.Is(err, context.Canceled) {
						logger.Error("webhook dispatcher stopped", "error", err)
					}
				}()
			}
			if cleaner != nil {
				workers.Add(1)
				go func() {
					defer workers.Done()
					if err := cleaner.Run(runContext); err != nil && !errors.Is(err, context.Canceled) {
						logger.Error("webhook retention cleaner stopped", "error", err)
					}
				}()
			}
			if bus != nil && inbox != nil && service != nil {
				workers.Add(1)
				go func() {
					defer workers.Done()
					err := bus.ConsumeWithOptions(runContext, eventbus.ConsumerOptions{
						Durable: "webhook-delivery-planner", FilterSubject: "platform.>",
						Handler: func(ctx context.Context, envelope *eventbus.Envelope) error {
							_, err := inbox.Process(ctx, platforminbox.Key{Consumer: "webhook-delivery-planner", EventID: envelope.GetEventId()}, "webhook-event-consumer", func(ctx context.Context, tx *sqlx.Tx) error {
								_, err := service.PlanDeliveriesTx(ctx, tx, envelope.GetEventType(), envelope)
								return err
							})
							return err
						},
						OnError: func(err error) { logger.Error("webhook event consumer", "error", err) },
					})
					if err != nil && !errors.Is(err, context.Canceled) {
						logger.Error("webhook event consumer stopped", "error", err)
					}
				}()
			}
			return nil
		},
		OnStop: func(context.Context) error {
			if cancel != nil {
				cancel()
			}
			workers.Wait()
			return nil
		},
	})
}

var WebhookModule = fx.Module("webhook",
	fx.Provide(newWebhookRepository, newWebhookSecretBox, newWebhookURLPolicy, newWebhookSender, newWebhookService, newWebhookDispatcher, newWebhookRetentionCleaner, newWebhookInbox),
	fx.Invoke(startWebhookRuntime),
)

# webhook-service

平台统一 Webhook 订阅与投递服务。服务订阅 `PLATFORM_EVENTS` JetStream 事件，按租户和事件主题生成投递任务，并以可重试、可审计的方式推送到外部 HTTPS 地址。

## 能力

- HTTP 与 gRPC 管理接口共用应用服务；业务 HTTP 接口统一使用 `POST + JSON` 响应包络。
- 订阅支持 NATS `*`、`>` 主题通配符、启停、密钥轮换、软删除和版本号乐观锁。
- 每个订阅生成独立 32 字节签名密钥；数据库仅保存 AES-256-GCM 密文，明文只在创建或轮换时返回一次。
- 投递使用 HMAC-SHA256；网络错误、408/425/429/5xx 指数退避，永久错误或重试耗尽进入 dead 状态并可人工重放。
- URL 默认仅允许 443/HTTPS；解析并固定已验证的公网 IP，拒绝私网、回环、链路本地、元数据地址、混合 DNS 结果及重定向，防止 SSRF 和 DNS rebinding。
- JetStream durable consumer 与数据库 Inbox 在同一事务中去重，重复事件不会生成重复任务。
- PostgreSQL、Kingbase、MySQL 独立迁移；所有可变表包含审计字段和 `version`。

protobuf 契约由中央仓库 [`platform-protos`](https://github.com/lihongjie0209/platform-protos) 管理。本服务固定依赖已发布版本，不在服务仓库复制 `.proto`。

## 本地启动

完整开发环境包含 PostgreSQL、MySQL、Redis、NATS、自动迁移和 API：

```bash
make dev-up
make dev-logs
make dev-down
```

HTTP 为 `127.0.0.1:8080`，gRPC 为 `127.0.0.1:9090`。Compose 中的密钥只用于开发，生产必须由 Secret Manager 注入独立值。

直接启动时，配置按 `config.yaml`、`config-{env}.yaml`、`APP_*` 环境变量顺序覆盖：

```bash
export APP_ENV=development
export APP_DATABASE_ENABLED=true
export APP_DATABASE_DSN='postgres://app:app@127.0.0.1:5432/platform?sslmode=disable&search_path=webhook'
export APP_WEBHOOK_ENCRYPTION_KEY="$(openssl rand -base64 32)"
go run ./cmd/api -config config/config.yaml
```

生产环境还必须配置 identity-service 的 JWKS、issuer、audience，以及数据库、NATS、Redis 和 TLS。`deployments/secret.example.yaml` 只描述字段，不能直接作为生产 Secret。

## 接口

HTTP 管理接口：

- `/api/v1/webhooks/subscriptions/create|update|get|list|rotate-secret|delete|test`
- `/api/v1/webhooks/deliveries/get|list|replay`

所有接口使用 POST JSON；列表接口支持 `page`、`page_size` 和服务端筛选，供未来 Web UI 或其他管理客户端直接使用。Swagger UI 在开发环境位于 `/swagger/index.html`。

gRPC 实现中央契约 `platform.webhook.v1.WebhookService`，独立监听 9090。JWT、PSK、Request ID、deadline 和 Trace context 由共享拦截器处理，标准健康检查无需认证。

接收端应使用原始请求体校验签名：

```text
canonical = Webhook-Timestamp + "." + raw_body
expected  = hex(HMAC-SHA256(secret, canonical))
header    = Webhook-Signature: v1=<expected>
```

接收端还应限制时间戳偏差，并以 `Webhook-Id` 去重。密钥轮换后旧签名立即失效，建议先更新接收端再轮换。

## 运行与安全

- `/live` 表示进程存活；`/ready` 分别检查数据库与 Redis；`/metrics` 导出 Prometheus 指标。
- `X-Request-ID` 写入响应、上下文和日志；Trace ID 与 Request ID 关联。
- 认证跳过路径和 PSK 路径由配置维护并支持 `path.Match` 通配符。
- 意外数据库或网络错误只记录在服务端，不泄漏给客户端。
- 自动迁移表为 `webhook_schema_migrations`，默认数据库 `platform`、schema `webhook`，避免共享数据库中的迁移冲突。
- 投递默认保留 30 天。上线前应配置清理/归档任务；达到分区收益阈值后再引入 pg_partman，同时保留全局幂等约束。

## 常用命令

```bash
make build             # 注入版本、Git commit 和构建时间
make test
make test-race
make test-integration  # Testcontainers: PG/MySQL/NATS/HTTP/gRPC
make lint
make swagger
make swagger-check
make migrate-up
make migrate-down
```

CI 分离运行单元/竞态/静态检查和集成测试；发布 tag 时构建 amd64/arm64 镜像，并附带 OCI Git 元数据、SBOM 和 provenance。

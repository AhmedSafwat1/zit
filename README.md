# identity-gateway

Authentication gateway in front of Zitadel (`https://zauth.4ja.org`). The hot
endpoint `POST /api/v1/users/introspect` validates a bearer token and returns
an identity report. Postgres is never touched on that path: the API caches
reports in Redis and publishes `user.sync` events to RabbitMQ; a separate
worker binary consumes them and upserts one `users` row per subject (`sub`, `email`,
`nickname`, a `snapshot` jsonb of the whole payload, an `identity_hash` that skips
writes when nothing changed) and reconciles the `sub_accounts` parent/child relation.
A parent referenced by a sub-account gets a row holding only its `sub` (everything else
NULL, `synced_at IS NULL`) until it logs in itself.

## Layout

```
cmd/api            HTTP gateway (Redis + RabbitMQ publisher + Zitadel; Postgres only when DB_DSN is set)
cmd/worker         RabbitMQ consumer (Postgres + Redis; no Zitadel, no publisher)
internal/app       dependency container + Module interface
internal/bootstrap wires modules from the container
internal/config    env-only configuration (godotenv + caarlos0/env)
internal/response  one JSON envelope + AppError catalog
internal/reqctx    request id / auth user carried on context
internal/types     small shared types (AuthUser)
internal/middleware request id, access log, recovery, body limit, timeout, auth, roles, rate limit
internal/modules/user  models / dto / repositories / usercache / services / handlers / jobs / routes / tests
pkg/circuitbreaker sony/gobreaker around an http.RoundTripper
pkg/cache          generic byte Store (Redis + in-memory), key/hash/JSON helpers
pkg/queue          envelope, confirm-mode publisher, consumer with retry/DLQ
pkg/zitadel        userinfo client + claims decoding
migrations         golang-migrate SQL
```

## Quickstart

```bash
cp .env.example .env    # host ports default to 5434 (pg), 6380 (redis), 5672 (amqp), API :8081
make up                 # postgres 16, redis 7, rabbitmq 3-management
make migrate            # apply migrations to $DB_DSN
make dev                # API with live reload (air); or: make run-api
make dev-worker         # worker with live reload;    or: make run-worker
```

Worker health/ready listens on `SERVER_WORKER_PORT` (8082) so both binaries can run on one host.
To exercise the worker without a real token:

```bash
go run ./internal/tools/publishsync -sub 900000000000000001 -parent 900000000000000002
```

```bash
curl -s -X POST localhost:8080/api/v1/users/introspect \
  -H "Authorization: Bearer $TOKEN" | jq
```

Swagger UI: `http://localhost:8081/swagger/index.html` (hidden when `APP_ENV=production`).

Errors: every 5xx is logged with its cause and request id. With `APP_DEBUG=true` the cause
(and the stack on panic) is also returned as `error.debug` in the response. Never in production.

## Make targets

| target | what |
|---|---|
| `make build` | `bin/api`, `bin/worker` |
| `make test` | full suite with `-race`; the Postgres suite needs Docker |
| `make test-short` | unit tests only (`-short` skips testcontainers) |
| `make lint` | `go vet` + `golangci-lint` |
| `make swagger` | regenerate `docs/` |
| `make mocks` | regenerate mockery mocks (committed; CI diffs them) |
| `make migrate` / `make migrate-down` | golang-migrate up / down 1 |
| `make config-check` | print the redacted config the binary sees |

## Request flow (introspect)

1. Reject structurally invalid tokens (empty, > 8 KiB, outside the b64token charset) before any I/O.
2. `MGET zit:introspect:<sha256> zit:inactive:<sha256>` (prefix from `CACHE_KEY_PREFIX`); inactive → `TOKEN_INACTIVE`, hit → return `meta.cached=true`.
3. `singleflight` per token hash, then Zitadel `/oidc/v1/userinfo`.
4. Issuer check, report built from claims and base64 metadata.
5. Detached (`context.WithoutCancel`, 500 ms budget): cache the report, notify the sync pipeline
   (deduped via `zit:sync:seen:<sub>` = payload hash).
6. `status_name != "Active"` → report plus `ACCOUNT_SUSPENDED`.

Invariants: Zitadel 5xx / timeout / breaker open writes nothing; only a 400/401 writes
`inactive:<hash>`; a 403 from upstream is `ZITADEL_UNAVAILABLE`, never inactive.

## Error codes

`TOKEN_MISSING` `TOKEN_INVALID` `TOKEN_INACTIVE` `ISSUER_MISMATCH` (401) ·
`ACCOUNT_SUSPENDED` `FORBIDDEN` (403) · `NOT_FOUND` (404) · `CONFLICT` (409) ·
`VALIDATION_ERROR` (422) · `RATE_LIMITED` (429) · `INTERNAL_ERROR` (500) ·
`ZITADEL_UNAVAILABLE` (503).

## Queue

Exchange `zit.events` (topic). Queue `user.sync` → DLX `zit.events.dlx` → `user.sync.dlq`.
Retries go through `user.sync.retry` with a per-message TTL (1 s → 5 s → 25 s), then the DLQ
with the original body and `x-failure-reason` header. `ErrUnsupportedVersion` and `ErrMalformed`
skip retries. Queue names are not versioned; the schema version lives in the envelope.

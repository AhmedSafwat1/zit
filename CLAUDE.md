# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`README.md` is the reference for the endpoint contract, error-code catalog, request flow and queue
topology. Read it for those details; this file covers commands, architecture and the rules that are
not visible from a single file.

## Commands

```bash
make up            # postgres 16 (:5434), redis 7 (:6380), rabbitmq 3-management (:5672) via docker compose
make migrate       # golang-migrate up; DB_* sourced from .env
make dev           # API with live reload (air, .air.toml); make run-api for a plain go run
make dev-worker    # worker with live reload (.air.worker.toml); make run-worker for plain
make test          # go test -race -count=1 ./... — the Postgres suite needs Docker
make test-short    # -short; skips the testcontainers suite
make lint          # go vet + golangci-lint
make mocks         # regenerate mockery mocks (committed; CI diffs them)
make swagger       # regenerate docs/ from handler annotations
make bench         # hot-path benchmarks (user services + pkg/cache)
make config-check  # print the redacted config the binary sees
```

Single test / single package:

```bash
go test -race -run TestIntrospect ./internal/modules/user/tests/
go test -short ./pkg/queue/            # no Docker
```

If `-race` fails at link time locally (broken cgo toolchain), run with `CGO_ENABLED=0` and drop
`-race`; `make build` already builds with `CGO_ENABLED=0`.

`go run ./internal/tools/publishsync -sub <sub> -parent <sub>` publishes a `user.sync` envelope so the
worker can be exercised without a real Zitadel token.

## Two binaries, one container

`cmd/api` and `cmd/worker` share `internal/app.Container` but ask for different dependencies through
`app.Option` values applied in order. Every option pings its target and fails fast; `Close` releases
in reverse construction order.

- `cmd/api`: Redis + RabbitMQ publisher + Zitadel. Postgres only when `DB_HOST` is set, and only for
  the read-only `/users` routes — **the introspect hot path never touches Postgres**.
- `cmd/worker`: Postgres + Redis. Deliberately no publisher and no Zitadel client. It also runs a
  second HTTP listener (`SERVER_WORKER_PORT`, 8082) for `/health` and `/ready` so both binaries fit on
  one host.

`internal/app` must stay free of module imports — that is what breaks the import cycle with
`internal/bootstrap`.

## Composition root

`internal/bootstrap` is the only package allowed to import module internals (`services`,
`repositories`, `usercache`, `handlers`, `routes`, `jobs`). Modules never build their own
dependencies; constructors receive already-built services.

- `shared.go` builds the instances modules share: `APIShared` (cmd/api) and `ConsumerShared`
  (cmd/worker), each built exactly once.
- `modules.go` lists `APIModules` / `WorkerModules` in dependency order.
- One file per module for its wiring (`user.go`).

Cross-module dependencies go through the `*Shared` interface accessors (`UserRepository()`,
`Authenticator()`), never the concrete fields: the accessors return a true nil interface when the
instance is absent, so a `dep == nil` check inside a module is honest.

A module is two independent types: `NewAPI(...)` implements `app.HTTPModule` (`RegisterRoutes`),
`NewWorker(...)` implements `app.ConsumerModule` (`RegisterConsumers`). A module may have only one
half. See the README's "Adding a module" for the full checklist.

### Module import rule

A module may import another module's `models` and `dto` **only**. Declare the interface you need in
your own `services` package (providers return concrete types) and register it in `.mockery.yaml`
under your package. Not-found across modules is `gorm.ErrRecordNotFound`. Transactions do not cross
modules — `EnsureUsers` is idempotent, so call it before your own transaction.

## Conventions that are easy to violate

- **One envelope.** Every response is `response.Envelope`; handlers return `*response.AppError` from
  the catalog in `internal/response/errors.go`. Codes are a public API contract — keep them stable.
  Never mutate a sentinel; derive with `.With(cause, details)`. `Fail` logs every 5xx with its cause
  and request id, and echoes the cause as `error.debug` only when `APP_DEBUG=true`.
- **Config is env-only, no validation layer** (`internal/config`): typed parsing plus `envDefault`
  tags are the contract. Add a field to the right section struct; secrets must be masked in
  `Redacted()`. A missing `.env` is not an error.
- **No metrics and no tracing by design.** Prometheus, queue trace/log middleware and trace-id
  propagation were deliberately removed. Do not reintroduce them unless asked.
- **Schema lives in `migrations/` only.** GORM tags are for reads and writes; never `AutoMigrate`.
- **Mocks are committed** under `internal/mocks` and `internal/modules/user/tests/mocks`. Change an
  interface → `make mocks`. Both directories are excluded from golangci-lint.
- **Module tests are black-box**: package `tests` in `internal/modules/user/tests`, driving the module
  through mocks and `testdata/` fixtures. Expected values are spelled out by hand so a mapper is never
  validated with itself.
- Handler annotation changes require `make swagger` (`docs/` is committed and imported by `cmd/api`).
- Extra linters beyond the standard set: `errorlint`, `gocritic`, `misspell`, `unconvert`, `unparam`.

## Hot path invariants (`POST /api/v1/users/introspect`)

`internal/modules/user/services/user_service.go` is written to be allocation-light and must stay that
way: `ValidateToken` rejects empty / >8 KiB / non-b64token tokens via a 256-entry byte table before any
I/O, and the rejection errors are pre-built package vars. Cache lookups are a single `MGET` of the
report and inactive keys; concurrent misses collapse through `singleflight` on the token hash.

Cache writes and the sync notification run **detached** — `context.WithoutCancel` with a
`WithSyncBudget` deadline (500 ms) — so the response is never held by them. Tests inject a synchronous
runner with `usersvc.WithSyncRunner`.

Never let a raw token past `services.TokenHash`; key naming lives in `internal/modules/user/usercache`
and the app-wide prefix (`CACHE_KEY_PREFIX`) is applied by the `cache.Store`, not by callers.

Upstream failure mapping is load-bearing: Zitadel 5xx / timeout / open breaker writes **nothing**; only
a 400/401 writes `inactive:<hash>`; a 403 is `ZITADEL_UNAVAILABLE`, never inactive.

## Sync pipeline

API side (`jobs/sync_user/publisher.go`): dedupe on `sync:seen:<sub>` = payload hash. Same hash →
skip; Redis error → publish anyway (the upsert is idempotent); publish failed → do **not** write the
key, so the next miss retries.

Worker side: `SyncService` upserts one `users` row per subject guarded by `identity_hash` (unchanged
identity → no write) and a staleness check, then reconciles `sub_accounts`. A parent referenced by a
sub-account gets a sub-only placeholder row (`synced_at IS NULL`, `User.IsPlaceholder()`).

`pkg/queue` owns the transport: one `Envelope` for every message (`OccurredAt` is set once by the
producer and never regenerated on retry; queue names are unversioned, the schema version lives in the
envelope), a confirm-mode publisher with reconnect, and a consumer with delayed retry (1 s → 5 s → 25 s)
then DLQ. `ErrUnsupportedVersion` and `ErrMalformed` skip retries. Idempotency is the handler's job.

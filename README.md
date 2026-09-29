# stats-service

GoCast statistics service: **likes, dislikes and views** per stream, backed by a
**separate Postgres database** (`stats`), with reaction activity events emitted
to the events-service feed.

Single read/write REST binary — no worker, no KEDA. Redis is used for two
unrelated purposes: the asynq **producer** client that feeds events-service, and
per-viewer **dedup** keys for view counting (separate logical DBs so the two
never collide).

## Architecture

Single REST binary (`stats-server`), deployment `stats-reader` (1 replica).
Schema migrations live in `services/db-migrate` (target `stats`, version table
`schema_migrations_stats`, dedicated Postgres database `stats`).

```
stats-service/
  config/            env config (SERVER_ADDR, DB_*, REDIS_*, rate limits, ...)
  cmd/server/        main, banner, wiring, graceful shutdown
  internal/
    database/        gorm pool (no AutoMigrate — db-migrate owns the schema)
    domain/models/   Reaction, StreamViews, Stats, reaction event payloads
    repository/      gorm repo: upsert/delete reaction, counts, views (+ mock)
    service/         StatsService: gates, reactions, views, events
                      TokenService: identity public-key JWT validation
    stream/          HTTPStatusClient for GET /stream/:id/status (+ mock)
    viewer/          RedisDeduper: 24h SETNX view dedup (+ mock)
    queue/           asynq producer for events-service (+ mock)
    metrics/         reactions_total / views_total
    delivery/http/   gin: RED metrics + CORS middleware, auth
                      (mandatory + optional), rate limit, routes, handler
deploy/k8s/          deployment.yaml, service.yaml
Dockerfile, Makefile (xomrkob/stats-service)
```

## REST API

All routes are served on `SERVER_ADDR` (default `:8080`).

| Method | Path | Auth | Purpose |
| --- | --- | --- | --- |
| `GET` | `/streams/:streamId/stats` | optional | counters + caller's own reaction |
| `POST` | `/streams/:streamId/views` | optional | register one view per viewer |
| `PUT` | `/streams/:streamId/reaction` | **required** | set/remove caller reaction |
| `GET` | `/health` | none | liveness + Postgres ping |
| `GET` | `/metrics` | none | Prometheus exposition |

- **`GET /streams/:streamId/stats`** → `200 {views, likes, dislikes, my_reaction?}`.
  `my_reaction` (`like` / `dislike`) is attached only when a valid Bearer token
  is sent; guests receive counters only.
- **`POST /streams/:streamId/views`** → `200 {"ok": true}`. Repeats inside the
  dedup window are accepted silently (no counter bump, no error).
- **`PUT /streams/:streamId/reaction`** body `{"kind": "like" | "dislike" | "none"}`
  → `200` with the updated counters. `like`/`dislike` upsert on
  `(stream_id, user_id)`; `none` removes the row. `my_reaction` is echoed for
  `like`/`dislike` and omitted for `none`.
- `kind` is lower-cased and trimmed before validation.

### CORS

`CORS_ALLOW_ORIGINS` allowlist; methods `GET`, `POST`, `PUT`, `DELETE`,
`OPTIONS`; headers `Content-Type`, `Authorization`; credentials allowed.

### Optional vs required auth

- `OptionalAuthMiddleware` (stats, views): a **missing or invalid** Bearer token
  is treated as anonymous — the request proceeds. A valid token also supplies
  the viewer identity used for view dedup.
- `AuthMiddleware` (reaction): missing token → `401 auth token required`;
  invalid token → `401 invalid token`; unparsable subject → `401 invalid user id`.
  Tokens are only accepted via the `Authorization: Bearer` header — no `?token=`
  query fallback.
- JWT validation uses the identity-service RSA public key, fetched **once at
  startup** (10s timeout) from `JWT_ACCESS_PUBLIC_KEY_URL`. A missing URL or
  failed fetch is fatal: the process refuses to start.

## Gates (fail-closed, mirrors comments-service)

Every read and write path calls `checkStreamCommentable`, which resolves
`GET {STREAM_SERVICE_URL}/stream/{id}/status` (5s client timeout) and requires
`status == "published"` **and** `visibility != "private"`.

| Condition | Service error | HTTP |
| --- | --- | --- |
| Status endpoint returns 404 | `ErrStreamNotFound` | `404 stream not found` |
| Stream not commentable | `ErrStreamNotPublished` | `403 stream is not published` |
| Transport error, unexpected status, undecodable body, no client configured | `ErrStreamUnavailable` | `503 internal server error` |

The status response also supplies `owner_id` and `title` for activity events.

Additional gate on reactions only: **`email_verified` is required**
(`email_verified` JWT claim), with an **admin bypass** (`role == "admin"`) —
the same rule stream-service applies to `CreateStream`.

> **Known inconsistency:** `GET /streams/:streamId/stats` is gated in the service
> layer, but its handler currently collapses *every* error into
> `500 internal server error` instead of using the shared `writeServiceError`
> mapper. Gated reads therefore surface as `500` rather than the `404`/`403`/`503`
> documented above. `PUT .../reaction` and `POST .../views` map gate errors
> correctly. This is a code issue, not a design choice — see the handler.

### Errors (write paths)

| Status | Body | When |
| --- | --- | --- |
| `400` | `invalid stream id` | `:streamId` is not a UUID |
| `400` | `invalid request body` | body is not valid JSON |
| `400` | `invalid reaction kind` | `kind` ∉ `like`/`dislike`/`none` |
| `401` | `auth token required` / `invalid token` / `invalid user id` | auth failures |
| `403` | `email not verified` | reaction by an unverified user (no admin bypass) |
| `403` | `stream is not published` | stream not commentable |
| `404` | `stream not found` | unknown stream |
| `429` | `too many requests` | rate limit exceeded |
| `503` | `internal server error` | stream-service unreachable, or view dedup unavailable |
| `500` | `internal server error` | anything else |

`503` bodies deliberately do not leak the upstream error; details go to
`slog.Error`.

## View dedup (anti-inflation)

The view counter is public, so it is protected by a **24h per-viewer dedup**
rather than by auth.

- Key: `view:{streamID}:{viewerKey}` in Redis, written with `SETNX` and a
  `24 * time.Hour` TTL (`viewer.ViewTTL`).
- `viewerKey` is `u:{userID}` for signed-in viewers and `ip:{ClientIP}` for
  guests (see *Trusted proxies* below).
- A `SETNX` miss (`false`) is a silent no-op: the request returns `200 {"ok": true}`
  without incrementing.
- **Fails closed.** If Redis errors, the service returns `ErrViewDedupUnavailable`
  → `503`, and the view is *not* counted. An outage therefore cannot degrade into
  unlimited inflation, which a fail-open deduper would allow.

The 24h window is the primary defense; the rate limiter below is only a backstop
against burst flooding.

### Trusted proxies (`ClientIP`)

`TRUSTED_PROXIES` lists the reverse proxies (traefik, …) whose forwarded headers
`gin` may honor when deriving the client IP.

- **Default is empty — nobody is trusted**, so `ClientIP()` is the direct TCP
  peer and a spoofed `X-Forwarded-For` is ignored. This is the honest default
  for guest dedup.
- Behind traefik with the default setting, all guests share the traefik pod IP
  as dedup key — so guest views collapse to one per stream per 24h. That is the
  deliberate trade: honest dedup over easily-inflated counts. Set
  `TRUSTED_PROXIES` to the ingress IPs when guest-level granularity is actually
  needed.
- If `SetTrustedProxies` rejects the configured value, the service logs the
  misconfiguration and **falls back to trusting nobody** rather than silently
  re-enabling spoofable IPs.

## Rate limiting

`middleware.RateLimitPerMin` is a dependency-free in-memory fixed-window
limiter (one window per key, per process, with opportunistic cleanup above 10k
keys). `limit <= 0` falls back to 300.

| Endpoint | Env | Default | Key |
| --- | --- | --- | --- |
| `POST /streams/:streamId/views` | `VIEWS_RATE_LIMIT` | `300`/min | `u:{userID}` if authenticated, else `ip:{ClientIP}` |
| `PUT /streams/:streamId/reaction` | `REACTION_RATE_LIMIT` | `30`/min | `u:{userID}` |

The views limiter runs **after** `OptionalAuthMiddleware` so signed-in callers
get their own budget instead of sharing the guest IP bucket. It is mounted
per-route, not on a group, so the reaction budget stays separate from the view
budget. Exceeding the limit → `429 too many requests`.

Because the limiter is per-process, the effective budget scales with replica
count (round-robin across pods). It is a coarse backstop, not a hard quota.

## Events

A reaction that **changes** the actor's previous reaction emits
`reaction.liked` / `reaction.disliked`. No event is emitted for:

- removals (`kind: "none"`),
- a repeat of the same kind (no actual change),
- self-reactions (`actor == owner`).

The event is addressed to the **stream owner** (`user_id` = `owner_id` from the
status endpoint), with payload `{actor_email, kind, title}` (`title` omitted
when empty). Actor identity comes from the `email` JWT claim.

Producer: asynq → Redis DB 3 (`REDIS_QUEUE_DB`), queue `events`, task
`event:activity` — the same wire contract as comments-service and
events-service. Emission is best-effort: a recorder failure logs a warning and
does not fail the reaction.

## Database

Dedicated Postgres database `stats` (default `DB_NAME=stats`), separate from
`database1` used by identity/stream/events/comments.

- Pool: max 10 open, 5 idle, 1h connection lifetime; 5s startup ping.
- GORM log level `Warn`.
- **No `AutoMigrate`** — the schema is owned by db-migrate.

### Schema (db-migrate target `stats`)

- `reactions(stream_id uuid, user_id uuid, kind text, created_at, updated_at)`,
  PK `(stream_id, user_id)`, `CHECK (kind IN ('like','dislike'))`, index
  `(stream_id, created_at DESC)`.
- `stream_views(stream_id uuid PK, count bigint DEFAULT 0, updated_at)`.

Likes/dislikes are derived by a single aggregate query over `reactions`; views
are an upsert counter (`ON CONFLICT ... DO UPDATE count = count + 1`).

## Configuration (env)

| Var | Default | Description |
| --- | --- | --- |
| `SERVER_ADDR` | `:8080` | HTTP listen address |
| `MODE` | `debug` | gin mode (`test`/`release` also honored) |
| `METRICS_ADDR` | `` | unused by this service; metrics ride the main server |
| `CORS_ALLOW_ORIGINS` | `http://localhost:5173,https://example.com,https://stats.example.com` | comma-separated allowlist |
| `TRUSTED_PROXIES` | `` (trust nobody) | proxies whose forwarded headers are honored |
| `DB_HOST` / `DB_PORT` | `localhost` / `5432` | Postgres |
| `DB_USER` / `DB_PASS` | `postgres` / `` | K8s secret `go-app-secret` |
| `DB_NAME` | `stats` | dedicated database |
| `REDIS_ADDR` | `localhost` | events producer + view dedup |
| `REDIS_PASS` | `` | K8s secret `casbin-redis` |
| `REDIS_QUEUE_DB` | `3` | events queue DB (asynq) |
| `REDIS_VIEWS_DB` | `4` | view dedup DB, kept separate from the queue DB |
| `VIEWS_RATE_LIMIT` | `300` | views requests/min per viewer key |
| `REACTION_RATE_LIMIT` | `30` | reaction requests/min per user |
| `JWT_ACCESS_PUBLIC_KEY_URL` | `` | identity public key; **required** |
| `STREAM_SERVICE_URL` | `http://stream-service:80` | stream status gate |

Numeric env values are parsed at startup; a malformed value is fatal with a
wrapped error (e.g. `views rate limit: parse VIEWS_RATE_LIMIT: ...`).
`DB_SSLMODE` and `DB_TIMEZONE` are hard-coded in the DSN (`disable`, `UTC`).

## Metrics

`GET /metrics` on the main HTTP server (no separate port):

- `reactions_total{kind}` — reaction mutations by kind.
- `views_total{status}` — registered views.
- RED middleware: `http_requests_total`, `http_request_duration_seconds` per
  method/endpoint/status.

`reactions_total` and `views_total` are lazy `CounterVec`s: a label combination
only appears in the exposition **after its first increment**, so an idle
deployment legitimately exposes no series for them.

## Health

`GET /health` pings Postgres and returns `200 {"status":"up"}`, or
`503 {"status":"down","error":...}` on ping failure. Used by both the liveness
and readiness probes.

## Timeouts

| Component | Value |
| --- | --- |
| HTTP server read / write | 15s / 15s |
| HTTP server idle | 60s |
| Graceful shutdown | 30s (`SIGINT`/`SIGTERM`) |
| Stream status client | 5s per request |
| Public-key fetch at startup | 10s |
| Postgres startup ping | 5s |

## Deployment

`deploy/k8s/deployment.yaml` — deployment `stats-reader`, 1 replica,
`imagePullPolicy: Always`, container port `8080` (named `http`), requests
`50m`/`128Mi`, limits `500m`/`256Mi`, liveness + readiness `httpGet /health:8080`
(5s initial delay, 10s period). Prometheus pod annotations scrape `:8080/metrics`.

`deploy/k8s/service.yaml` — ClusterIP `stats-reader`, port `8080` → target port
`http`.

Environment comes from ConfigMap `stats-service-config` (`envFrom`), with
`REDIS_PASS` from secret `casbin-redis` and `DB_USER` / `DB_PASS` from
`go-app-secret`.

Externally the service is exposed by ingress `stats-service` on
`stats.<STATS_DOMAIN>` → `stats-reader:8080` with TLS secret `stats-tls`.
ConfigMap, ingress, and the `db-migrate-stats` Job are generated in the
**gocast-infra** repo (`scripts/render-env.sh`, `make apply-db-migrate`).

## Makefile

```bash
make build   # docker build, tags xomrkob/stats-service and :$(git describe)
make push    # push both the version tag and :latest
make deploy  # kubectl set image + rollout status (needs KUBECONFIG)
make test    # go test ./...
```

`VERSION` comes from `git describe --tags --always`; `BUILD_DATE` is injected as
UTC. Docker builds run from the `services/` directory (build context `..`) and
are stamped into the binary via `-ldflags -X main.version/main.buildDate`,
surfaced in the startup log and banner.

## Tests

```bash
go test ./...
```

| File | Covers |
| --- | --- |
| `config/config_test.go` | env defaults and overrides |
| `internal/stream/status_client_test.go` | status endpoint 200/404/error, URL building |
| `internal/service/stats_service_impl_test.go` | counters, `my_reaction`, read gate (incl. nil client / network), reactions, view dedup fail-closed |
| `internal/delivery/http/handler/stats_handler_test.go` | public read, invalid stream id, reaction validation |
| `internal/delivery/http/handler/stats_handler_views_test.go` | view registration and dedup responses |
| `internal/delivery/http/middleware/auth_test.go` | required/optional Bearer handling |
| `internal/delivery/http/middleware/ratelimit_test.go` | per-key budget, per-user isolation |

Mocks are generated with `mockgen` (`//go:generate` in `viewer/`, `stream/`,
`repository/`, `queue/`).

## Related

- Stream status endpoint: `GET /stream/:id/status` in **stream-service**.
- Event contract and feed: **events-service** (`event:activity` → queue `events`).
- Migrations: **db-migrate** target `stats`.
- Consumer: **web-frontend** registers a view only after ~80% of the video was
  watched, so the counter tracks real engagement rather than page loads.

# stats-service

GoCast statistics service: **likes, dislikes and views** per stream, backed by
a **separate Postgres database** (`stats`), with activity events emitted to the
events-service feed.

## API (REST, `/health`, `/metrics`)

- **public** `GET /streams/:id/stats` — `{views, likes, dislikes, my_reaction?}`.
  `my_reaction` is attached only when a valid Bearer token is sent.
- **public** `POST /streams/:id/views` — bumps the anonymous per-stream view counter.
- **auth** `PUT /streams/:id/reaction` — body `{kind: "like" | "dislike" | "none"}`;
  upserts (or removes) the caller's reaction and returns the updated counters.

### Gates (fail-closed, mirrors comments-service)

- **Write (reactions):** verified email required (`email_verified` claim), admin
  bypass.
- **Stream:** reactions and views only on **published** streams with visibility
  **≠ private** — checked via the public stream-service endpoint
  `GET /stream/:id/status`. Stream not found → 404, not commentable → 403,
  network failure/no client → 503.
- **Read (`GET stats`):** public, no gate (like the comments read API).

### Events

Like/dislike that changes the actor's previous reaction emits
`reaction.liked` / `reaction.disliked` to the **stream owner** (`user_id` =
`owner_id` from the status endpoint). Payload: `{actor_email, kind, title}`.
Self-reactions (actor == owner) and removals (`none`) emit nothing.
Producer: asynq → Redis DB 3 (`REDIS_QUEUE_DB`), queue `events`, task
`event:activity` — same contract as comments-service/events-service.

## Architecture

Single REST binary (`stats-server`), deployment `stats-reader` (1 replica).
No worker/KEDA — Redis is used only as an asynq producer client. Schema
migrations live in `services/db-migrate` (target `stats`, table
`schema_migrations_stats`, dedicated Postgres database `stats`).

```
stats-service/
  config/            env config (SERVER_ADDR, DB_*, REDIS_*, STREAM_SERVICE_URL, ...)
  cmd/server/        main, banner, graceful shutdown
  internal/
    database/        gorm pool (no AutoMigrate)
    domain/models/   Reaction, StreamViews, Stats, event payloads
    repository/      gorm repo: upsert/delete reaction, counts, views (+ mock)
    service/         StatsService: gates, reactions, views, events (mock + tests)
    stream/          HTTPStatusClient for GET /stream/:id/status (mock)
    queue/           asynq producer for events-service (mock)
    delivery/http/   gin: metrics+CORS middleware, auth (mandatory+optional),
                      routes, handler
    metrics/         reactions_total / views_total + RED middleware
deploy/k8s/          deployment.yaml, service.yaml
Dockerfile, Makefile (xomrkob/stats-service)
```

## Configuration (env)

| Var | Default | Description |
| --- | --- | --- |
| `SERVER_ADDR` | `:8080` | HTTP listen address |
| `MODE` | `debug` | gin mode |
| `CORS_ALLOW_ORIGINS` | `http://localhost:5173,https://example.com,https://stats.example.com` | comma-separated allowlist |
| `DB_HOST` / `DB_PORT` | `localhost` / `5432` | Postgres |
| `DB_USER` / `DB_PASS` | `postgres` / `` | from K8s secret `go-app-secret` |
| `DB_NAME` | `stats` | dedicated database |
| `REDIS_ADDR` | `localhost` | asynq producer |
| `REDIS_PASS` | `` | from K8s secret `casbin-redis` |
| `REDIS_QUEUE_DB` | `3` | events queue DB |
| `JWT_ACCESS_PUBLIC_KEY_URL` | — | identity public key for Bearer validation |
| `STREAM_SERVICE_URL` | `http://stream-service:80` | stream status gate |

## DB schema (db-migrate target `stats`, own database)

- `reactions(stream_id, user_id, kind like|dislike, created_at, updated_at)`,
  PK `(stream_id, user_id)`.
- `stream_views(stream_id PK, count bigint default 0, updated_at)`.

## Local dev

```bash
go work sync          # from repo root (uses ../services symlinks)
go build ./...        # from service dir
go test ./...
```

Deployment wiring (ConfigMap `stats-service-config`, ingress `stats.<DOMAIN>`,
`make apply-db-migrate` job `db-migrate-stats`) lives in the gocast-infra repo.
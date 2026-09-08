# Architecture

How a request becomes a response, how work gets off the request path, and why
the boundaries fall where they do.

## Layers in the API

```
HTTP request
    │
    ▼
middleware      request id → recover → access log → CORS → timeout → rate limit → auth
    │
    ▼
handler         decode, delegate, encode. No business logic.
    │
    ▼
service         business rules, authorisation, cache invalidation, job enqueue
    │
    ▼
repository      every SQL query. Handlers never see *gorm.DB.
    │
    ▼
Postgres
```

The rule that keeps this honest: **a handler may only call a service, and a
service may only reach the database through a repository.** When a handler needs
two queries and a cache eviction, that logic belongs in a service, not in the
handler.

### Why services exist separately from handlers

Anything that touches more than one dependency — a write plus a cache eviction
plus an enqueued job — has to be somewhere it can be tested without an HTTP
request and reused from the worker. `MediaService.ConfirmUpload` is called by
both the HTTP handler and (indirectly) the reconciliation job; if it lived in
the handler, the worker would have to duplicate it.

## Errors

One error type crosses the handler boundary: `httpx.APIError`. It carries a
status, a stable machine-readable `code`, a human message, and an _unexported_
`cause`.

```go
return nil, httpx.NotFound("session")
return nil, httpx.Conflict("this session is full")
return nil, httpx.Internal(err).WithCause(err)   // cause is logged, never sent
```

`httpx.Error` maps anything else onto a generic 500. That is deliberate: an
unwrapped error from the driver could contain a connection string, and the
client has no business seeing it.

Clients get a consistent envelope:

```json
{ "error": { "code": "not_found", "message": "session was not found" } }
```

and successes get `{ "data": ..., "meta": { "total", "limit", "offset", "hasMore" } }`.

## Authentication

Dynamic owns the wallet flow entirely — signature challenges, embedded wallets,
account linking. The API's job is narrower than it looks:

1. Read the bearer token.
2. Verify it against Dynamic's JWKS, with the algorithm pinned to RS256 (an
   unpinned verifier is vulnerable to algorithm confusion).
3. Confirm the `environment_id` claim matches ours, so a token minted for a
   different Dynamic project cannot be replayed here.
4. Map `sub` onto a local `users` row, creating it on first sight.

Step 4 is a write transaction, so its result is cached in Redis for two minutes
keyed by the Dynamic user id. A page that fires six parallel requests costs one
provisioning transaction and five Redis reads.

`Optional` runs on every `/v1` route so public reads can be personalised;
`Required` sits on top for protected ones and reuses whatever `Optional` already
resolved rather than verifying the same token twice.

### The service token

`/v1/internal/*` is for machine callers — queue inspection, sync triggers,
notification fan-out. It authenticates with a shared token compared in constant
time, and **fails closed**: if `INTERNAL_SERVICE_TOKEN` is unset, the routes
return 403 rather than becoming open. nginx additionally restricts the prefix to
private address ranges.

## Uploads

File bytes never pass through the API server.

```
browser                     API                      Cloudflare R2
   │  POST /v1/media/uploads │                             │
   ├────────────────────────►│ validate type + size        │
   │                         │ write a `pending` row       │
   │◄────────────────────────┤ presigned PUT URL           │
   │                                                       │
   ├──────────────── PUT the file ────────────────────────►│
   │                                                       │
   │  POST /{id}/confirm     │                             │
   ├────────────────────────►│ HEAD the object ───────────►│
   │                         │◄─── real size and type ─────┤
   │                         │ mark uploaded, queue work   │
   │◄────────────────────────┤                             │
```

Two things matter here:

**The row is created before the object.** An upload the user abandons leaves a
`pending` row, which the hourly reaper can find and clean up. Creating the row
on confirmation instead would leave orphaned objects in the bucket that nothing
knows about.

**Confirmation verifies against R2, not the client.** `ConfirmUpload` calls
`HEAD` and records the size and content type R2 reports. A client claiming a
1 KB upload for a 1 GB file gets the real number recorded.

The content type is also signed into the presigned URL, so the client cannot
substitute a different one at PUT time.

## Realtime sessions

The Agora app certificate never leaves the server. Joining is an authorisation
decision the API makes:

1. Is the session still open? (`ended` and `cancelled` are refused.)
2. If it is org-scoped, is the caller a member?
3. Is there room? (The host is exempt; a rejoining participant already holds a
   slot.)
4. Mint an RTC token scoped to that channel and that uid, with the publisher
   role only for the host.

The Agora uid is derived deterministically from the user's UUID
(`agora.DeriveUID`) and persisted on the participant row, so a rejoin reuses the
same uid and the token stays bound to one identity.

Tokens are short-lived. The client schedules a renewal a minute before expiry
and also listens for Agora's `token-privilege-will-expire` event — belt and
braces, because a laptop that slept through the timer still needs to recover.

## Background work

asynq over the same Redis instance as the cache. Three queues with weights:

| Queue      | Weight | What goes there                   |
| ---------- | ------ | --------------------------------- |
| `critical` | 6      | notifications, session lifecycle  |
| `default`  | 3      | media processing, indexer sync    |
| `low`      | 1      | reaping, cache warming, retention |

**Deduplication.** Tasks that must not run twice carry a deterministic
`TaskID` — `media-process-<uuid>`. Enqueuing a duplicate returns
`ErrTaskIDConflict`, which the client treats as success, so a double-clicked
confirm button processes one image.

**Retries** use exponential backoff capped at ten minutes, so a dependency
outage does not turn into a hammering loop.

**Panics** are caught by middleware in the worker and converted into task
failures, so one bad payload cannot kill the processor and abandon everything
else in flight.

**Cron** lives in code (`jobs.PeriodicSchedule`) rather than in Redis, so the
schedule is reviewable in a diff. asynq elects a single active scheduler across
replicas; production still pins it to one container and scales pure workers
alongside it.

### Why `jobs` and `worker` are separate packages

`service` enqueues jobs, so it imports `jobs`. The handlers that _run_ those
jobs need `service`. Putting both in one package is an import cycle. `jobs`
holds the task definitions and the enqueue client; `internal/worker` holds the
handlers.

## Onchain data

Envio HyperIndex indexes contracts into its own Postgres and serves GraphQL. The
API does not query it on the request path. Instead a cron job mirrors new rows
into the application's database:

```
Envio Postgres ──GraphQL──► sync job ──► onchain_events + indexer_checkpoints
                                              │
                                              ▼
                                        API reads (fast, local)
```

This costs some staleness and buys three things: API reads do not depend on the
indexer being up, events can be joined against `users` in SQL (attributing a
transfer to an account), and a reorg is handled by rewinding a checkpoint rather
than by invalidating a cache.

Writes are idempotent — `ON CONFLICT (envio_id) DO UPDATE` — so re-running a
sync over the same range changes nothing, which is what makes rewinding safe.

## Caching

Redis, with three distinct uses that share one connection:

- **Read-through** (`cache.Remember`) for identity resolution and indexer
  status. A load failure is never cached.
- **Rate limiting**, a fixed window keyed by user id when authenticated and by
  client IP otherwise — one noisy user cannot exhaust a shared office IP's
  budget.
- **Distributed locks** (`cache.Lock`) for work that must not run concurrently
  across replicas.

Redis being unavailable degrades rather than fails: `Remember` falls through to
the origin, and the rate limiter fails open. The reasoning is that a cache
outage should not become an API outage — but note the trade-off, which is that a
Redis outage also removes rate limiting.

## The database

GORM with `AutoMigrate` driven from the model structs. Everything AutoMigrate
cannot express — partial indexes, expression indexes, extensions — lives in
`postMigrationStatements()` and must be idempotent.

Primary keys are **UUIDv7**, not v4. v7 embeds a timestamp in its high bits, so
keys are roughly time-ordered and inserts append to the right edge of the btree
instead of scattering across it.

Soft deletes (`gorm.DeletedAt`) are the default, with one exception: the media
reaper hard-deletes, because a tombstone would keep the unique constraint on
`key` occupied and block re-uploading the same path.

**Migrations must be additive.** Deploys run migrations before the new image
starts, so old and new code briefly share a schema. Removing a column takes two
releases: stop using it, then drop it.

# Development

## Setup

Requires Go 1.26+, Node 20+, pnpm and Docker.

```bash
make setup    # Go modules, pnpm workspace, air + golangci-lint
make env      # .env and apps/web/.env.local from the examples
make doctor   # confirm the toolchain is complete
make dev      # everything
```

`make dev` starts Postgres, Redis and Asynqmon in Docker, applies migrations,
then runs the API, worker and Next.js dev server in one terminal. Ctrl-C stops
all three.

Prefer separate terminals? Each piece has its own target:

```bash
make dev-infra     # Postgres + Redis + Asynqmon, then migrate
make dev-api       # API with hot reload (air)
make dev-worker    # background worker and cron scheduler
make dev-web       # Next.js
make indexer       # Envio
```

## Running without credentials

Everything works with an empty `.env` beyond the local database URLs. On boot
the API logs which integrations are missing:

```
WARN object storage disabled: R2 credentials are not set
WARN realtime video disabled: Agora credentials are not set
WARN authentication disabled: DYNAMIC_ENVIRONMENT_ID is not set
```

Endpoints that need a missing integration return `503 service_unavailable` with
a message naming it, rather than a confusing 500. The frontend shows a banner
when wallet auth is unconfigured instead of rendering a connect button that
cannot work.

This is deliberate: you can clone the repo and have a running stack before
signing up for anything.

## Adding a feature

Follow the layering. A new "projects" resource, end to end:

1. **Model** — `apps/api/internal/models/project.go`, add it to `models.All()`.
2. **Repository** — `internal/repository/project.go`, register it in
   `repository.New`.
3. **Service** — `internal/service/project.go`, add it to `service.Services`.
4. **Handler** — `internal/api/handlers/projects.go`.
5. **Route** — `internal/api/router.go`.
6. **Types** — `packages/shared/src/types.ts` and `routes.ts`.
7. **Hook** — `apps/web/src/hooks/use-api.ts`.

Then `make db-migrate && make check`.

Two things to keep straight:

**Declare each path prefix exactly once in the router.** Splitting `/projects`
across two `chi.Group` blocks mounts a second subrouter at the same pattern and
silently shadows the first. Nest public and authenticated routes inside one
`r.Route` instead:

```go
r.Route("/projects", func(r chi.Router) {
    r.Get("/", handler.List)              // public

    r.Group(func(r chi.Router) {
        r.Use(authenticator.Required)
        r.Post("/", handler.Create)       // authenticated
    })
})
```

**Migrations must be additive.** See [architecture.md](architecture.md#the-database).

## Adding a background job

1. Define the payload and constructor in `internal/jobs/tasks.go`.
2. Write the handler in `internal/worker/handlers.go`.
3. Register it in `Handlers.Register`.
4. For a recurring job, add an entry to `jobs.PeriodicSchedule`.

Give a task a deterministic `asynq.TaskID` when running it twice would be
wrong — that turns a duplicate enqueue into a no-op.

## Testing

```bash
make test            # everything
make test-api        # Go, with the race detector
make test-coverage   # Go coverage, opens the HTML report
```

The Go tests run with `-race`. Anything touching shared state across goroutines
— the cache, the worker — should be exercised under it.

## Database

```bash
make db-shell        # psql
make db-status       # which tables exist
make db-seed         # development data (idempotent)
make db-reset        # drop and re-migrate; asks first
```

`make db-seed` creates a demo user with a linked wallet, an organization, a
scheduled session and two notifications. It keys off fixed sentinel ids, so
running it repeatedly does not fork duplicate records.

## Queues

```bash
make asynqmon        # open http://localhost:8081
make queues          # queue depth as JSON (needs INTERNAL_SERVICE_TOKEN)
```

Asynqmon shows pending, active, retrying and archived tasks, and lets you retry
or delete individual ones — the fastest way to see why a job is not running.

## The indexer

```bash
make indexer-codegen   # generate types from config.yaml + schema.graphql
make indexer           # run it
make indexer-graphql   # open the playground
```

`indexer/generated/` does not exist in a fresh checkout, so `indexer/src/*.ts`
will not typecheck until you have run codegen once. That is why the CI job runs
codegen before typechecking.

Point `config.yaml` at your own contracts and start block. The default indexes
USDC on Ethereum and Base from a recent block — indexing from genesis takes
hours and is rarely what you want locally.

## Code style

```bash
make fmt          # format Go, TypeScript and CSS
make fmt-check    # verify without changing files
make lint         # golangci-lint and eslint
make check        # everything CI runs
```

Run `make check` before pushing; it is exactly what CI runs.

## Troubleshooting

**`make dev` fails immediately.** Docker is not running, or 5432/6379 is already
taken by a local Postgres or Redis. Either stop it, or override the port:
`make dev POSTGRES_PORT=5433`.

**`connection refused` from the API.** The database is not up yet. `make db-up`
waits for the health check; a plain `docker compose up -d` does not.

**Frontend cannot reach the API.** Check `NEXT_PUBLIC_API_URL` in
`apps/web/.env.local`, and that `CORS_ALLOWED_ORIGINS` in `.env` includes
`http://localhost:3000`. Next also reads `NEXT_PUBLIC_*` at _build_ time, so a
change needs a dev-server restart.

**Hot reload is not working.** `air` is not installed. `make setup-tools`, or
just live with `go run` — `make dev-api` falls back to it and says so.

**`Store not initialized` in the browser.** A Dynamic SDK hook is being called
outside `DynamicContextProvider`. Read auth state through
`useIsAuthenticated()` from `@/components/auth-context`, never from the SDK
hooks directly — that indirection is what lets the app prerender and run
signed-out.

**Jobs stay pending forever.** The worker is not running (`make dev-worker`), or
it points at a different Redis than the API.

**Envio types are missing.** Run `make indexer-codegen`.

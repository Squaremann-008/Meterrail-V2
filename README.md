# Meterrail

A production-shaped monorepo: a Next.js frontend, a Go API, background jobs,
wallet authentication, object storage, realtime video and an onchain indexer —
all startable with `make dev`.

```
┌──────────────┐         ┌───────────────┐        ┌──────────────┐
│  Next.js 16  │  HTTPS  │     nginx     │        │   Postgres   │
│   (Vercel)   ├────────►│ reverse proxy ├───────►│ Neon/Supabase│
└──────┬───────┘         └───────┬───────┘        └──────▲───────┘
       │                         │                       │
       │ Dynamic JWT             ▼                       │ GORM
       │                 ┌──────────────┐                │
       │                 │   Go API     ├────────────────┘
       │                 │   (chi)      │
       │                 └───┬──────┬───┘
       │                     │      │
       │  presigned PUT      │      │ asynq
       ▼                     ▼      ▼
┌──────────────┐     ┌──────────┐  ┌──────────┐     ┌──────────────┐
│ Cloudflare R2│     │  Redis   │  │  Worker  │────►│ Envio indexer│
│   (media)    │     │  cache   │  │  + cron  │     │  (GraphQL)   │
└──────────────┘     └──────────┘  └──────────┘     └──────────────┘
                           ▲
                           │
                     ┌──────────┐
                     │ Asynqmon │  queue dashboard
                     └──────────┘
```

## The stack

| Layer                 | Choice                                                 | Where                                                                                    |
| --------------------- | ------------------------------------------------------ | ---------------------------------------------------------------------------------------- |
| Frontend              | Next.js 16 (App Router, TypeScript, Tailwind v4)       | [`apps/web`](apps/web)                                                                   |
| Backend               | Go 1.26, chi router                                    | [`apps/api`](apps/api)                                                                   |
| ORM                   | GORM                                                   | [`apps/api/internal/models`](apps/api/internal/models)                                   |
| Database              | Postgres — Neon or Supabase                            | [`internal/database`](apps/api/internal/database)                                        |
| Cache & rate limiting | Redis                                                  | [`internal/cache`](apps/api/internal/cache)                                              |
| Object storage        | Cloudflare R2 (S3 API)                                 | [`internal/storage`](apps/api/internal/storage)                                          |
| Jobs & cron           | asynq, dashboard by Asynqmon                           | [`internal/jobs`](apps/api/internal/jobs), [`internal/worker`](apps/api/internal/worker) |
| Auth                  | Dynamic (web3 wallets)                                 | [`internal/auth`](apps/api/internal/auth)                                                |
| Realtime video        | Agora RTC + RTM                                        | [`internal/agora`](apps/api/internal/agora)                                              |
| Onchain indexing      | Envio HyperIndex                                       | [`indexer`](indexer)                                                                     |
| Reverse proxy         | nginx                                                  | [`deploy/nginx`](deploy/nginx)                                                           |
| CI/CD                 | GitHub Actions                                         | [`.github/workflows`](.github/workflows)                                                 |
| Deploy                | VPS (Hetzner) for the backend, Vercel for the frontend | [`deploy`](deploy)                                                                       |

## Quick start

Requires Go 1.26+, Node 20+, pnpm and Docker.

```bash
make setup      # install every dependency
make env        # create .env and apps/web/.env.local from the examples
make dev        # Postgres + Redis + Asynqmon + API + worker + web
```

That gives you:

|                 |                                    |
| --------------- | ---------------------------------- |
| Web             | http://localhost:3000              |
| API             | http://localhost:8080              |
| Queue dashboard | http://localhost:8081              |
| Health          | http://localhost:8080/health/ready |

`make dev` works with **no third-party credentials at all**. The API detects
which integrations are unconfigured, logs a warning for each, and returns a
`503 service_unavailable` from the endpoints that need them — so you can run
and develop the whole app before signing up for anything.

Add credentials to `.env` as you need them:

| Feature        | Variables                                                                | Where to get them                                                  |
| -------------- | ------------------------------------------------------------------------ | ------------------------------------------------------------------ |
| Wallet sign-in | `DYNAMIC_ENVIRONMENT_ID`, `NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID`           | [app.dynamic.xyz](https://app.dynamic.xyz) → Settings → Developers |
| File uploads   | `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET` | Cloudflare → R2 → Manage API Tokens                                |
| Video sessions | `AGORA_APP_ID`, `AGORA_APP_CERTIFICATE`                                  | [console.agora.io](https://console.agora.io)                       |
| Onchain data   | `ENVIO_GRAPHQL_URL`                                                      | `make indexer` serves it locally                                   |

## Common commands

`make help` lists all of them. The ones you will actually use:

```bash
make dev              # everything, one terminal
make dev-api          # just the API, with hot reload
make dev-web          # just the frontend
make indexer          # the Envio indexer

make db-migrate       # apply the schema
make db-seed          # populate development data
make db-shell         # psql
make db-reset         # drop and re-migrate (asks first)

make check            # format, lint, typecheck and test — what CI runs
make test             # every test suite
make build            # every artifact

make docker-up        # the whole stack in Docker
make asynqmon         # open the queue dashboard
make doctor           # check your toolchain
```

## How the pieces fit

**Authentication.** Dynamic runs the wallet signature challenge in the browser
and issues a JWT. The API verifies it against Dynamic's JWKS
([`internal/auth`](apps/api/internal/auth/dynamic.go)) and maps the `sub` claim
onto a local `users` row, provisioning it on first sight. The resolved identity
is cached in Redis for two minutes, so a page load costs one Redis read rather
than a write transaction.

**Uploads never touch the API.** The browser asks for a presigned PUT, uploads
straight to Cloudflare R2, then calls back to confirm. The API verifies the
object actually landed by asking R2 for its size — it does not trust the
client — and queues thumbnail generation. Abandoned uploads are reaped hourly.

**Video is tokenised server-side.** The Agora app certificate stays on the
server. Clients call `POST /v1/sessions/{id}/join`, which checks membership and
capacity, then mints a channel-scoped RTC token. The client renews it a minute
before expiry.

**Onchain data is mirrored, not proxied.** Envio indexes the chain into its own
Postgres. A cron job pulls new rows through GraphQL into the application's
database behind a per-chain checkpoint, so API reads stay fast and stay
available while the indexer resyncs or rewinds a reorg.

## Deployment

The backend runs on a VPS behind nginx; the frontend goes to Vercel.

```bash
# One-time: prepare the server (Docker, firewall, deploy user, SSH hardening)
make vps-bootstrap DEPLOY_HOST=root@your-server-ip

# One-time: TLS
make tls-cert DEPLOY_HOST=deploy@your-server-ip \
              DOMAIN=api.example.com EMAIL=you@example.com

# Every release (or let GitHub Actions do it on push to main)
make deploy-api DEPLOY_HOST=deploy@your-server-ip
make deploy-web
```

`deploy.sh` pulls the new image, runs migrations, rolls the containers with a
start-first update, verifies `/health/ready`, and rolls back automatically if
the new containers never become healthy.

See [`docs/deployment.md`](docs/deployment.md) for the full runbook, including
the GitHub secrets the workflows expect.

## Repository layout

```
apps/
  api/                  Go backend
    cmd/                server, worker, migrate, seed
    internal/
      api/              router, handlers, middleware
      service/          business logic
      repository/       data access (GORM)
      models/           schema
      auth/ agora/      Dynamic and Agora integrations
      storage/ cache/   R2 and Redis
      jobs/ worker/     task definitions and their handlers
  web/                  Next.js frontend
packages/shared/        TypeScript types mirroring the API contract
indexer/                Envio HyperIndex
deploy/                 nginx, compose, systemd, scripts
docs/                   architecture and runbooks
```

## Documentation

- [Architecture](docs/architecture.md) — how requests, jobs and data flow
- [Deployment](docs/deployment.md) — VPS runbook, CI/CD secrets, rollback
- [Development](docs/development.md) — local setup, testing, troubleshooting

## License

[MIT](LICENSE)

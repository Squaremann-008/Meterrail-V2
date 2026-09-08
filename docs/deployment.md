# Deployment

The backend runs in Docker on a VPS behind nginx. The frontend goes to Vercel.
Postgres is Neon or Supabase; Redis is the container on the VPS or a managed
instance.

## One-time server setup

Any Ubuntu 24.04 box works — Hetzner CX22 (2 vCPU, 4 GB) is comfortable for the
API, worker and Redis together.

```bash
make vps-bootstrap DEPLOY_HOST=root@your-server-ip
```

That script installs Docker, creates an unprivileged `deploy` user, configures
ufw (22, 80, 443 only), disables SSH password authentication and root password
login, enables fail2ban and unattended upgrades, and tunes a few kernel limits
for a proxy workload. It is idempotent.

Then copy the production environment file and the deploy directory:

```bash
scp .env.production deploy@your-server-ip:/opt/meterrail/.env.production
rsync -az deploy/ deploy@your-server-ip:/opt/meterrail/deploy/
```

`.env.production` is `.env.example` with real values. At minimum:

```bash
APP_ENV=production
LOG_FORMAT=json
DATABASE_URL=postgres://...        # Neon or Supabase, sslmode=require
REDIS_URL=redis://:PASSWORD@redis:6379/0
REDIS_PASSWORD=...                 # compose requires this
CORS_ALLOWED_ORIGINS=https://app.example.com
HTTP_TRUST_PROXY_HEADERS=true      # nginx is in front
INTERNAL_SERVICE_TOKEN=...         # openssl rand -hex 32
API_IMAGE=ghcr.io/OWNER/REPO-api:latest
DYNAMIC_ENVIRONMENT_ID=...
R2_ACCOUNT_ID=... R2_ACCESS_KEY_ID=... R2_SECRET_ACCESS_KEY=... R2_BUCKET=...
AGORA_APP_ID=... AGORA_APP_CERTIFICATE=...
```

The API refuses to start in production if `CORS_ALLOWED_ORIGINS` still points at
localhost or `DYNAMIC_ENVIRONMENT_ID` is missing. That check exists because both
mistakes are silent and severe.

## DNS and TLS

Point A records at the server:

| Record               | Purpose                           |
| -------------------- | --------------------------------- |
| `api.example.com`    | the Go API                        |
| `queues.example.com` | Asynqmon (basic auth)             |
| `app.example.com`    | only if self-hosting the frontend |

Edit `deploy/nginx/conf.d/meterrail.conf` and replace `api.example.com` with
your domain, then issue the certificate:

```bash
make tls-cert DEPLOY_HOST=deploy@your-server-ip \
              DOMAIN=api.example.com EMAIL=you@example.com
```

The `certbot` container renews automatically every twelve hours.

Asynqmon has **no authentication of its own**. Create the basic-auth file before
exposing it:

```bash
ssh deploy@your-server-ip 'htpasswd -Bc /opt/meterrail/deploy/nginx/.htpasswd admin'
```

## Deploying

```bash
make deploy-api DEPLOY_HOST=deploy@your-server-ip
```

`deploy/scripts/deploy.sh` runs, in order:

1. Record the currently running image, for rollback.
2. Pull the new image.
3. **Run migrations** in a one-off container. If they fail, the deploy stops
   here and nothing has changed.
4. Roll `api`, `worker`, `worker-extra` and `asynqmon` with a start-first
   update.
5. Reload nginx (reload, not restart — in-flight requests are not dropped).
6. Poll `/health/ready` for up to a minute.
7. If it never becomes ready, roll back to the recorded image and exit non-zero.

### Rolling back

```bash
make deploy-rollback DEPLOY_HOST=deploy@your-server-ip TAG=sha-abc1234
```

This rolls back **code only**. Because migrations are additive by policy, the
older code runs fine against the newer schema. A schema rollback is a manual,
deliberate operation.

## The frontend

Vercel builds from the monorepo root. In the Vercel project settings:

- Root directory: `apps/web`
- The included `vercel.json` sets the build command so the pnpm workspace
  resolves `@meterrail/shared`.

Set these as **environment variables** in the Vercel dashboard (they are inlined
into the browser bundle, so none of them may be secret):

```
NEXT_PUBLIC_API_URL=https://api.example.com
NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID=...
NEXT_PUBLIC_AGORA_APP_ID=...
NEXT_PUBLIC_CDN_HOSTNAME=cdn.example.com
NEXT_PUBLIC_APP_NAME=Meterrail
```

Then add the Vercel domain to `CORS_ALLOWED_ORIGINS` on the API and redeploy the
backend, or the browser will block every request.

## CI/CD

| Workflow              | Trigger                                        | What it does                                      |
| --------------------- | ---------------------------------------------- | ------------------------------------------------- |
| `ci.yml`              | every push and PR                              | format, vet, lint, test, build — per changed area |
| `deploy-backend.yml`  | push to main touching `apps/api` or `deploy`   | build and push to GHCR, then deploy over SSH      |
| `deploy-frontend.yml` | push to main touching `apps/web` or `packages` | build and deploy to Vercel                        |

`ci.yml` uses a paths filter so a frontend-only change does not spin up Postgres
and Redis, and ends with a single `CI` job you can set as the one required
status check in branch protection.

### Required secrets

Repository → Settings → Secrets and variables → Actions.

**Secrets:**

| Name                 | Value                                           |
| -------------------- | ----------------------------------------------- |
| `DEPLOY_SSH_KEY`     | private key authorised for the `deploy` user    |
| `DEPLOY_HOST`        | `deploy@your-server-ip`                         |
| `DEPLOY_KNOWN_HOSTS` | output of `ssh-keyscan your-server-ip`          |
| `VERCEL_TOKEN`       | Vercel account token                            |
| `VERCEL_ORG_ID`      | from `.vercel/project.json` after `vercel link` |
| `VERCEL_PROJECT_ID`  | same file                                       |

`DEPLOY_KNOWN_HOSTS` pins the host key rather than accepting it on first use, so
a hijacked DNS record cannot redirect a deploy to another machine.

**Variables** (not secrets — they end up in the bundle):
`NEXT_PUBLIC_API_URL`, `NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID`,
`NEXT_PUBLIC_AGORA_APP_ID`, `NEXT_PUBLIC_CDN_HOSTNAME`, `NEXT_PUBLIC_APP_NAME`.

## Operations

```bash
ssh deploy@HOST 'cd /opt/meterrail && docker compose -f deploy/docker-compose.prod.yml logs -f api'
ssh deploy@HOST 'cd /opt/meterrail && docker compose -f deploy/docker-compose.prod.yml ps'

curl https://api.example.com/health/ready | jq
```

`/health/ready` returns 503 when Postgres or Redis is down, and 200 with a
per-dependency breakdown otherwise. R2 and Envio are reported but do not fail
the check — they are optional integrations, and a 503 would take the whole API
out of the load balancer over a degraded upload path.

### Scaling

```bash
# In .env.production
API_REPLICAS=3
WORKER_REPLICAS=2
```

The API is stateless, so replicas scale freely; nginx round-robins via Docker's
embedded DNS. For workers, scale `worker-extra` (which runs with
`-scheduler=false`) and leave `worker` at one replica so exactly one process
owns the cron schedule.

### Backups

Neon and Supabase both provide point-in-time recovery. For a second copy you
control:

```bash
sudo cp deploy/systemd/meterrail-backup.{service,timer} /etc/systemd/system/
sudo systemctl enable --now meterrail-backup.timer
```

Nightly `pg_dump` to R2, seven days retained locally. The script refuses to
finish if the dump comes out suspiciously small, which is the usual signature of
a `pg_dump` that failed mid-stream.

### Systemd

If you would rather systemd owned the stack than Docker's restart policies:

```bash
sudo cp deploy/systemd/meterrail.service /etc/systemd/system/
sudo systemctl enable --now meterrail
```

## Troubleshooting

**The API will not start.** `docker compose logs api`. Config validation errors
name the exact variable. A `DATABASE_URL` that fails to dial is the most common
cause — the API pings at boot deliberately, so a bad URL fails immediately
instead of on the first request.

**Every browser request fails but curl works.** CORS. The origin must be listed
in `CORS_ALLOWED_ORIGINS` exactly, including scheme and port.

**All authenticated requests return 401.** Check `DYNAMIC_ENVIRONMENT_ID`
matches the frontend's `NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID`. The API rejects
tokens whose `environment_id` claim belongs to a different project.

**Jobs queue but never run.** The worker container is down, or it is pointed at a
different Redis than the API. Compare `REDIS_URL` in both, and check Asynqmon.

**Rate limiting is not working.** It fails open when Redis is unreachable, by
design. Check `docker compose logs api | grep 'rate limiter unavailable'`.

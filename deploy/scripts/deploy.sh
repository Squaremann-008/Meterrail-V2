#!/usr/bin/env bash
#
# Deploys the backend on the VPS. Run from the repository root on your machine:
#
#   make deploy-api DEPLOY_HOST=deploy@1.2.3.4
#
# or directly on the host, from /opt/meterrail:
#
#   ./deploy/scripts/deploy.sh
#
# The deploy is: pull the new image, run migrations, then roll the services.
# Migrations run before the new code so the old containers keep serving against
# a schema that is forward-compatible; every migration must therefore be
# additive (add columns and indexes, never drop or rename in the same release).

set -euo pipefail

APP_DIR="${APP_DIR:-/opt/meterrail}"
COMPOSE_FILE="${COMPOSE_FILE:-${APP_DIR}/deploy/docker-compose.prod.yml}"
ENV_FILE="${ENV_FILE:-${APP_DIR}/.env.production}"
IMAGE_TAG="${IMAGE_TAG:-latest}"

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mWARN:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31mERROR:\033[0m %s\n' "$*" >&2; exit 1; }

[[ -f "$COMPOSE_FILE" ]] || die "compose file not found: ${COMPOSE_FILE}"
[[ -f "$ENV_FILE" ]] || die "environment file not found: ${ENV_FILE}"

compose() {
    docker compose --file "$COMPOSE_FILE" --env-file "$ENV_FILE" "$@"
}

log "Deploying tag ${IMAGE_TAG}"

# Record what is currently running so a failed deploy can be rolled back.
PREVIOUS_IMAGE="$(docker compose --file "$COMPOSE_FILE" --env-file "$ENV_FILE" \
    images --quiet api 2>/dev/null | head -1 || true)"

log "Pulling images"
compose pull --quiet

log "Running database migrations"
# A one-off container on the same image and env as the API. If this fails the
# deploy stops here, before any traffic reaches new code.
if ! compose run --rm --no-deps --entrypoint migrate api -action=up; then
    die "migrations failed; nothing was deployed"
fi

log "Rolling application services"
compose up -d --remove-orphans --wait --wait-timeout 120 api worker worker-extra asynqmon

log "Reloading nginx"
# `reload` rather than `restart` so in-flight requests are not dropped.
compose exec -T nginx nginx -s reload 2>/dev/null \
    || compose up -d --force-recreate nginx

log "Verifying health"
HEALTH_OK=false
for attempt in $(seq 1 12); do
    if compose exec -T api wget -qO- http://127.0.0.1:8080/health/ready >/dev/null 2>&1; then
        HEALTH_OK=true
        break
    fi
    printf '  attempt %d/12 …\n' "$attempt"
    sleep 5
done

if [[ "$HEALTH_OK" != true ]]; then
    warn "the new containers did not become ready"
    if [[ -n "$PREVIOUS_IMAGE" ]]; then
        warn "rolling back to ${PREVIOUS_IMAGE}"
        API_IMAGE="$PREVIOUS_IMAGE" compose up -d --wait api worker worker-extra
    fi
    die "deploy failed and was rolled back; check 'docker compose logs api'"
fi

log "Pruning dangling images"
docker image prune -f --filter 'dangling=true' >/dev/null

log "Deploy complete"
compose ps

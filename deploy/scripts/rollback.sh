#!/usr/bin/env bash
#
# Rolls the backend back to a previously published image tag.
#
#   ./deploy/scripts/rollback.sh sha-abc1234
#
# Note this rolls back code only. Migrations are additive by policy, so the
# older code runs against the newer schema; if a release genuinely needs a
# schema rollback, do that by hand and deliberately.

set -euo pipefail

TAG="${1:?usage: rollback.sh IMAGE_TAG}"

APP_DIR="${APP_DIR:-/opt/meterrail}"
COMPOSE_FILE="${COMPOSE_FILE:-${APP_DIR}/deploy/docker-compose.prod.yml}"
ENV_FILE="${ENV_FILE:-${APP_DIR}/.env.production}"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }

# Reuse the registry path from the env file, swapping only the tag.
REGISTRY_IMAGE="$(grep -E '^API_IMAGE=' "$ENV_FILE" | cut -d= -f2- | cut -d: -f1)"
[[ -n "$REGISTRY_IMAGE" ]] || { echo "API_IMAGE not set in ${ENV_FILE}" >&2; exit 1; }

TARGET="${REGISTRY_IMAGE}:${TAG}"

log "Rolling back to ${TARGET}"
docker pull "$TARGET"

API_IMAGE="$TARGET" docker compose \
    --file "$COMPOSE_FILE" --env-file "$ENV_FILE" \
    up -d --wait --wait-timeout 120 api worker worker-extra

log "Rollback complete"
docker compose --file "$COMPOSE_FILE" --env-file "$ENV_FILE" ps

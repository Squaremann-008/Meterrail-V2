#!/usr/bin/env bash
#
# Issues the initial Let's Encrypt certificate. Run once per domain on the VPS;
# the certbot container in docker-compose.prod.yml handles renewals after that.
#
#   ./deploy/scripts/issue-cert.sh api.example.com you@example.com

set -euo pipefail

DOMAIN="${1:?usage: issue-cert.sh DOMAIN EMAIL [EXTRA_DOMAIN...]}"
EMAIL="${2:?usage: issue-cert.sh DOMAIN EMAIL [EXTRA_DOMAIN...]}"
shift 2

APP_DIR="${APP_DIR:-/opt/meterrail}"
COMPOSE_FILE="${COMPOSE_FILE:-${APP_DIR}/deploy/docker-compose.prod.yml}"
ENV_FILE="${ENV_FILE:-${APP_DIR}/.env.production}"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }

compose() { docker compose --file "$COMPOSE_FILE" --env-file "$ENV_FILE" "$@"; }

# Build the -d flags for the primary domain plus any extras.
DOMAIN_ARGS=(-d "$DOMAIN")
for extra in "$@"; do
    DOMAIN_ARGS+=(-d "$extra")
done

log "Starting nginx to serve the ACME challenge"
compose up -d nginx

log "Requesting a certificate for ${DOMAIN}"
# --webroot writes the challenge file where nginx already serves it, so there
# is no need to stop the proxy.
compose run --rm certbot certonly \
    --webroot --webroot-path /var/www/certbot \
    --email "$EMAIL" \
    --agree-tos --no-eff-email \
    --non-interactive \
    "${DOMAIN_ARGS[@]}"

log "Reloading nginx with the new certificate"
compose exec -T nginx nginx -s reload

log "Certificate issued for ${DOMAIN}"

# syntax=docker/dockerfile:1.7

# ---- build ----------------------------------------------------------------
FROM golang:1.26-alpine AS build

WORKDIR /src

# Cache the module graph separately from the source so a code-only change does
# not re-download every dependency.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none

# CGO is off so the result is a static binary that runs on a scratch-like base.
# -trimpath keeps build paths out of the binary; -s -w drops the symbol table.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed

# ---- runtime --------------------------------------------------------------
FROM alpine:3.21 AS runtime

# ca-certificates for TLS to Neon/Supabase/R2/Dynamic; tzdata for cron
# schedules that are not in UTC.
RUN apk add --no-cache ca-certificates tzdata wget && \
    adduser -D -u 10001 -h /app meterrail

WORKDIR /app

COPY --from=build /out/server /out/worker /out/migrate /out/seed /usr/local/bin/

USER meterrail

ENV PORT=8080 \
    HTTP_HOST=0.0.0.0 \
    APP_ENV=production \
    LOG_FORMAT=json

EXPOSE 8080

# The liveness probe touches no dependency, so a slow database cannot cause a
# restart loop.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:${PORT}/health/live" >/dev/null || exit 1

ENTRYPOINT ["server"]

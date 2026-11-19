# syntax=docker/dockerfile:1

# ---- build petd (Go, standard library only) ----------------------------------
FROM golang:1.24-alpine AS build
WORKDIR /src/petd
COPY petd/ ./
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/petd ./cmd/petd

# ---- runtime -------------------------------------------------------------------
FROM alpine:3.21
# python3: the bundled plugins are stdlib-only Python. tzdata: so TZ works
# (daily reminders and the "time" event use the local clock).
RUN apk add --no-cache python3 tzdata ca-certificates \
 && adduser -D -u 10001 pet \
 && mkdir /data && chown pet /data

COPY --from=build /out/petd /usr/local/bin/petd
COPY plugins /app/plugins
COPY skins /app/skins

ENV PET_DATA_DIR=/data \
    PET_PLUGINS_DIR=/app/plugins \
    PET_SKINS_DIR=/app/skins \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PET_PORT=23817 \
    PET_HTTP_PORT=23818

USER pet
WORKDIR /app
VOLUME /data
# PET_PORT: renderer + plugin protocol (NDJSON over TCP)   PET_HTTP_PORT: HTTP API
# (compose passes the values from .env; these are only the image defaults)
EXPOSE $PET_PORT $PET_HTTP_PORT
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget -qO- "http://127.0.0.1:${PET_HTTP_PORT}/healthz" >/dev/null || exit 1
ENTRYPOINT ["petd"]

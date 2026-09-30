# syntax=docker/dockerfile:1

ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH

FROM --platform=$BUILDPLATFORM node:22-alpine AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26.2-alpine AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=web-build /src/web/dist/ ./internal/webui/dist/
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/tgstrm ./cmd/tgstrm

# Backup and restore invoke PostgreSQL's official client programs. Keep clients
# for supported server majors side by side so each one can select an exact match.
FROM debian:bookworm-slim
ARG POSTGRES_CLIENT_MAJORS="14 15 16 17 18"
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && install -d /usr/share/keyrings \
    && curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc -o /usr/share/keyrings/postgresql.asc \
    && echo "deb [signed-by=/usr/share/keyrings/postgresql.asc] https://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" > /etc/apt/sources.list.d/pgdg.list \
    && apt-get update \
    && for major in ${POSTGRES_CLIENT_MAJORS}; do apt-get install -y --no-install-recommends "postgresql-client-${major}"; done \
    && apt-get purge -y --auto-remove curl \
    && rm -rf /var/lib/apt/lists/* \
    && for major in ${POSTGRES_CLIENT_MAJORS}; do test -x "/usr/lib/postgresql/${major}/bin/pg_dump" && test -x "/usr/lib/postgresql/${major}/bin/pg_restore"; done
RUN mkdir -p /data && chown 65532:65532 /data
LABEL org.opencontainers.image.title="TgStrm" \
      org.opencontainers.image.description="Telegram STRM catalog manager" \
      org.opencontainers.image.source="https://github.com/AnubisReal/TgStrm"
COPY --from=go-build /out/tgstrm /tgstrm
ENV TGSTRM_ADDR=:5151 \
    TGSTRM_ALLOW_REMOTE=1 \
    TGSTRM_DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 5151
USER 65532:65532
ENTRYPOINT ["/tgstrm"]

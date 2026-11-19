# syntax=docker/dockerfile:1

FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mailworker ./cmd/mailworker

FROM alpine:3.21 AS migrate
ARG TARGETARCH
COPY third_party/goose_linux_arm64 third_party/goose_linux_x86_64 /tmp/
RUN arch="${TARGETARCH:-}" \
  && if [ -z "$arch" ]; then arch="$(uname -m)"; fi \
  && case "$arch" in \
       amd64|x86_64) src=/tmp/goose_linux_x86_64 ;; \
       arm64|aarch64) src=/tmp/goose_linux_arm64 ;; \
       *) echo "unsupported arch: $arch" >&2; exit 1 ;; \
     esac \
  && mv "$src" /usr/local/bin/goose \
  && chmod +x /usr/local/bin/goose \
  && rm -f /tmp/goose_linux_*
COPY migrations /migrations
WORKDIR /migrations
ENTRYPOINT ["goose", "-dir", "/migrations", "postgres"]

FROM alpine:3.21
# BusyBox wget is enough for the healthcheck; avoid apk (needs network mirrors).
RUN adduser -D -H -u 10001 app \
 && mkdir -p /data/uploads \
 && chown -R app:app /data
WORKDIR /app
COPY --from=build /out/api /app/api
COPY --from=build /out/seed /app/seed
COPY --from=build /out/mailworker /app/mailworker
COPY migrations /app/migrations
ENV UPLOADS_DIR=/data/uploads
USER app
EXPOSE 8080
ENTRYPOINT ["/app/api"]

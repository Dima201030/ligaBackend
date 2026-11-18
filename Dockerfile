# syntax=docker/dockerfile:1

FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM golang:1.23-alpine AS goose
RUN apk add --no-cache git ca-certificates \
  && go install github.com/pressly/goose/v3/cmd/goose@v3.24.1

FROM alpine:3.21 AS migrate
RUN apk add --no-cache ca-certificates
COPY --from=goose /go/bin/goose /usr/local/bin/goose
COPY migrations /migrations
WORKDIR /migrations
ENTRYPOINT ["goose", "-dir", "/migrations", "postgres"]

FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget \
  && adduser -D -H -u 10001 app
WORKDIR /app
COPY --from=build /out/api /app/api
COPY migrations /app/migrations
USER app
EXPOSE 8080
ENTRYPOINT ["/app/api"]

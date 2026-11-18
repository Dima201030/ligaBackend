# ligaBackend

Go API для лиги Mr. Pushkin (D-017). Этап 2: Postgres, каркас HTTP, криптография.
Auth register/login и UI — этап 3.

## Стек

- Go 1.23+
- PostgreSQL 17
- chi, pgx, sqlc, goose
- Argon2id + AES-256-GCM (`internal/crypto`)

## Быстрый старт

```bash
cp .env.example .env
# сгенерировать ключ и вписать в APP_ENCRYPTION_KEY (не коммитить .env):
openssl rand -base64 32

./scripts/fetch-goose.sh   # linux-бинарники goose в third_party/ (не в git)
docker compose up -d --build
```

Compose поднимает `db` → `migrate` (goose up) → `api`.

- API: `http://127.0.0.1:3020`
- Postgres: только внутри сети Compose (порт на хост не публикуется)

### Health

```bash
curl -sS http://127.0.0.1:3020/api/health
```

Ожидается HTTP 200 и JSON без секретов.

### Повторный migrate

```bash
docker compose run --rm migrate
# локально (goose + DATABASE_URL на доступный Postgres):
goose -dir migrations postgres "$DATABASE_URL" up
goose -dir migrations postgres "$DATABASE_URL" status
```

## sqlc

После изменения `sql/schema.sql` или `sql/queries/*.sql`:

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
sqlc generate
```

## Тесты

```bash
go test ./...
```

Проверяются Argon2id verify (верный / неверный пароль), AES-256-GCM roundtrip
и `/api/health`. Пароль никогда не сравнивается через `==` / plaintext —
только `VerifyPassword` (constant-time).

## Локальный запуск API без Compose

```bash
export DATABASE_URL='postgres://liga:liga@127.0.0.1:5432/liga?sslmode=disable'
export APP_ENCRYPTION_KEY="$(openssl rand -base64 32)"
export HTTP_ADDR='127.0.0.1:3020'
goose -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/api
```

## Структура

```text
cmd/api/           точка входа
internal/config/   env
internal/crypto/   random, SHA-256 токенов, Argon2id, AES-GCM
internal/http/     router, /api/health
internal/db/       pgx pool + sqlc
migrations/        goose
sql/               schema + queries для sqlc
```

## Переменные окружения

См. `.env.example` (только имена; секреты не коммитить).

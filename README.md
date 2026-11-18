# ligaBackend

Go API для лиги Mr. Pushkin (D-017). Этап 4: турниры и заявки (вместимость/waitlist),
плюс auth этапа 3 (cookie-сессии, approve/reject).

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

### Auth API

| Метод | Путь | Описание |
|---|---|---|
| POST | `/api/auth/register` | Регистрация → `pending` + profile + outbox |
| POST | `/api/auth/login` | Вход (только `approved`; cookie-сессия) |
| POST | `/api/auth/logout` | Отзыв сессии |
| GET | `/api/auth/me` | Текущий пользователь |
| GET/POST | `/api/auth/confirm-email` | Подтверждение email по токену |
| GET | `/api/organizer/applications` | Список заявок (organizer+) |
| POST | `/api/organizer/applications/{id}/approve` | Approve + правка ФИО/уровня |
| POST | `/api/organizer/applications/{id}/reject` | Reject |
| GET | `/api/tournaments` | Публичный список (без draft) |
| GET | `/api/tournaments/{slug}` | Карточка + заявки |
| POST | `/api/tournaments/{slug}/registrations` | Заявка (approved) |
| DELETE | `/api/tournaments/{slug}/registrations/me` | Отмена своей заявки |
| GET | `/api/organizer/tournaments` | Все турниры (organizer+) |
| POST | `/api/organizer/tournaments` | Создать турнир + rule v1 |
| GET | `/api/organizer/tournaments/{id}` | Карточка организатора |
| PATCH | `/api/organizer/tournaments/{id}` | Редактировать / сменить статус |

Вместимость: `SELECT … FOR UPDATE` на турнир в одной транзакции; overflow → waitlist;
отмена confirmed/pending продвигает очередь.

Сессия: 32 random bytes в cookie `mpl_session` (prod: `__Host-mpl_session`),
HttpOnly + SameSite=Lax + Secure(prod); в БД только SHA-256(token).

### Seed организатора

Пароль только из env (не коммитить):

```bash
export SEED_ORGANIZER_EMAIL='organizer@example.com'
export SEED_ORGANIZER_PASSWORD='...' # минимум 8 символов
docker compose run --rm --entrypoint /app/seed \
  -e SEED_ORGANIZER_EMAIL -e SEED_ORGANIZER_PASSWORD \
  -e SEED_ORGANIZER_FIRST_NAME -e SEED_ORGANIZER_LAST_NAME \
  api
```

Локально (если Postgres доступен на хосте):

```bash
export DATABASE_URL='postgres://liga:liga@127.0.0.1:5432/liga?sslmode=disable'
export APP_ENCRYPTION_KEY='...' # тот же, что у api
export SEED_ORGANIZER_EMAIL='organizer@example.com'
export SEED_ORGANIZER_PASSWORD='...'
go run ./cmd/seed
```

### Health и статус

```bash
curl -sS http://127.0.0.1:3020/api/health
open http://127.0.0.1:3020/status
```

`/api/health` и `/status` не содержат секретов.

### Повторный migrate

```bash
docker compose run --rm migrate
goose -dir migrations postgres "$DATABASE_URL" up
```

## sqlc

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
sqlc generate
```

## Тесты

```bash
go test ./...
```

Пароль никогда не сравнивается через `==` / plaintext — только `VerifyPassword`.

## Структура

```text
cmd/api/           HTTP API
cmd/seed/          CLI seed approved organizer
internal/auth/     сессии, rate limit, уровни
internal/config/   env
internal/crypto/   random, SHA-256 токенов, Argon2id, AES-GCM
internal/http/     router, auth, organizer, tournaments
internal/tournaments  slug + registration capacity
internal/mail/     email_outbox enqueue
internal/db/       pgx pool + sqlc
migrations/        goose
sql/               schema + queries для sqlc
```

## Переменные окружения

См. `.env.example` (только имена; секреты не коммитить).

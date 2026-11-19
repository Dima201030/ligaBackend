# ligaBackend

Go API для лиги Mr. Pushkin (D-017). Этап 9 (частично): SMTP-worker из `email_outbox`,
сброс пароля и resend confirm; плюс контент (7), рейтинг (6), матчи (5), турниры (4), auth (3).

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
| POST | `/api/auth/resend-confirm` | Повтор письма подтверждения (анти-enumeration) |
| POST | `/api/auth/forgot-password` | Запрос сброса пароля → outbox |
| POST | `/api/auth/reset-password` | Новый пароль по токену (сессии отзываются) |
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
| GET | `/api/tournaments/{slug}/bracket` | Публичная сетка (только published) |
| GET | `/api/tournaments/{slug}/matches` | Публичные матчи турнира |
| GET | `/api/organizer/tournaments/{id}/bracket` | Сетка организатора (draft/published) |
| POST | `/api/organizer/tournaments/{id}/bracket/generate` | Генерация из confirmed |
| POST | `/api/organizer/tournaments/{id}/bracket/publish` | Публикация сетки |
| GET | `/api/organizer/tournaments/{id}/matches` | Матчи турнира |
| GET | `/api/organizer/matches` | Все матчи (кроме bye) |
| PATCH | `/api/organizer/matches/{id}` | Расписание / корт / live |
| POST | `/api/organizer/matches/{id}/score` | Счёт по сетам + advance |
| POST | `/api/organizer/matches/{id}/walkover` | Техническая победа |
| POST | `/api/organizer/matches/{id}/cancel` | Отмена матча |
| GET | `/api/players` | Публичный список игроков (`category`, `q`, `limit`, `offset`) |
| GET | `/api/players/{slug}` | Публичный профиль |
| GET | `/api/players/{slug}/rating-events` | История рейтинга |
| GET | `/api/rankings` | Таблица (`season`, `category`; live или snapshot) |
| GET | `/api/organizer/players` | Игроки для организатора |
| PATCH | `/api/organizer/players/{id}` | Правка профиля/категории/статуса |
| POST | `/api/organizer/ratings/manual` | Ручная корректировка (с причиной) |
| POST | `/api/organizer/ratings/snapshot` | Снимок таблицы `rankings` |
| POST | `/api/organizer/tournaments/{id}/ratings/apply` | Начисление очков за турнир |
| GET | `/api/news` | Публичные новости (`tag`, `limit`, `offset`) |
| GET | `/api/news/{slug}` | Карточка новости |
| GET | `/api/events` | Публичные события (`kind`, `limit`, `offset`) |
| GET | `/api/events/{slug}` | Карточка события |
| GET | `/api/pages` | Список статических страниц |
| GET | `/api/pages/{slug}` | Страница (club/contacts/rules…) |
| GET | `/api/organizer/news` | Все новости (вкл. draft) |
| POST | `/api/organizer/news` | Создать новость |
| PATCH | `/api/organizer/news/{id}` | Редактировать / publish/archive |
| GET | `/api/organizer/events` | Все события |
| POST | `/api/organizer/events` | Создать событие |
| PATCH | `/api/organizer/events/{id}` | Редактировать событие |
| PUT | `/api/organizer/pages/{slug}` | Upsert markdown-страницы |

Вместимость: `SELECT … FOR UPDATE` на турнир в одной транзакции; overflow → waitlist;
отмена confirmed/pending продвигает очередь.

Сетки: генерация только из `confirmed` (не waitlist). Повторная генерация запрещена,
если есть матчи со статусом `completed`/`walkover`. Публикация draft→published.
Счёт и walkover в одной транзакции с переносом победителя в `next_match`
(и проигравшего в матч за 3-е место при olympic+third_place). Оптимистичная
блокировка по `version`. Форматы: olympic, round_robin, groups_playoff,
double_elimination (порт `src/lib/bracket.ts`).

Рейтинг (этап 6): коэффициенты v1 из `docs/RATING.md` в `rating_configs`
(сезон 2026). Elo пишется в той же TX, что и счёт/walkover (walkover Elo не
двигает). Очки турнира — отдельным вызовом `ratings/apply` (идемпотентно).
Посев сетки `seeding=rating` читает `profiles.rating`. Снимок таблицы —
`POST /api/organizer/ratings/snapshot`.

Сессия: 32 random bytes в cookie `mpl_session` (prod: `__Host-mpl_session`),
HttpOnly + SameSite=Lax + Secure(prod); в БД только SHA-256(token).

### Mail worker (SMTP)

Письма всегда сначала попадают в `email_outbox`. Доставка — отдельный процесс:

```bash
# локально (нужны SMTP_* в env)
go run ./cmd/mailworker

# compose profile (не стартует без SMTP_HOST/SMTP_FROM)
docker compose --profile smtp up -d mailworker
```

Шаблоны: `confirm_email`, `registration_received`, `application_approved`,
`application_rejected`, `password_reset`. Без SMTP очередь копится, API не падает.
Повторные попытки с backoff; после `MAIL` max attempts статус `failed`.

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
cmd/mailworker/    SMTP drain of email_outbox
internal/auth/     сессии, rate limit, уровни
internal/config/   env
internal/crypto/   random, SHA-256 токенов, Argon2id, AES-GCM
internal/http/     router, auth, organizer, tournaments, matches, players, ratings, content
internal/tournaments  slug + registration capacity
internal/bracket/  pure draw generation (olympic/RR/groups/DE)
internal/matches/  generate/publish/score/advance (+ Elo hook)
internal/rating/   формула очков/Elo, slug игрока, apply/snapshot
internal/content/  reading time для новостей
internal/mail/     outbox enqueue, templates, SMTP sender, worker
internal/db/       pgx pool + sqlc
migrations/        goose
sql/               schema + queries для sqlc
```

## Переменные окружения

См. `.env.example` (только имена; секреты не коммитить).

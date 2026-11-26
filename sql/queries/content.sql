-- name: CreateNewsPost :one
INSERT INTO news_posts (
    club_id, slug, title, excerpt, body_json, cover_url, tag, author_name, author_id,
    status, published_at, reading_time
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
RETURNING *;

-- name: UpdateNewsPost :one
UPDATE news_posts
SET title = $2,
    excerpt = $3,
    body_json = $4,
    cover_url = $5,
    tag = $6,
    author_name = $7,
    status = $8,
    published_at = $9,
    reading_time = $10,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetNewsPostByID :one
SELECT * FROM news_posts WHERE id = $1;

-- name: GetNewsPostByClubAndID :one
SELECT * FROM news_posts WHERE club_id = $1 AND id = $2;

-- name: GetNewsPostBySlug :one
SELECT * FROM news_posts WHERE club_id = $1 AND slug = $2;

-- name: NewsSlugExists :one
SELECT EXISTS(
    SELECT 1 FROM news_posts WHERE club_id = $1 AND slug = $2
)::bool AS exists;

-- name: ListPublishedNews :many
SELECT *
FROM news_posts
WHERE club_id = sqlc.arg(club_id)
  AND status = 'published'
  AND (sqlc.narg(tag)::text IS NULL OR tag = sqlc.narg(tag))
ORDER BY published_at DESC NULLS LAST, created_at DESC
LIMIT sqlc.arg(limit_count) OFFSET sqlc.arg(offset_count);

-- name: ListOrganizerNews :many
SELECT *
FROM news_posts
WHERE club_id = sqlc.arg(club_id)
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::content_status)
ORDER BY updated_at DESC
LIMIT sqlc.arg(limit_count) OFFSET sqlc.arg(offset_count);

-- name: CreateClubEvent :one
INSERT INTO club_events (
    club_id, slug, title, kind, description, starts_at, location, cover_url, seats_left, status, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING *;

-- name: UpdateClubEvent :one
UPDATE club_events
SET title = $2,
    kind = $3,
    description = $4,
    starts_at = $5,
    location = $6,
    cover_url = $7,
    seats_left = $8,
    status = $9,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetClubEventByID :one
SELECT * FROM club_events WHERE id = $1;

-- name: GetClubEventByClubAndID :one
SELECT * FROM club_events WHERE club_id = $1 AND id = $2;

-- name: GetClubEventBySlug :one
SELECT * FROM club_events WHERE club_id = $1 AND slug = $2;

-- name: EventSlugExists :one
SELECT EXISTS(
    SELECT 1 FROM club_events WHERE club_id = $1 AND slug = $2
)::bool AS exists;

-- name: ListPublishedEvents :many
SELECT *
FROM club_events
WHERE club_id = sqlc.arg(club_id)
  AND status = 'published'
  AND (sqlc.narg(kind)::text IS NULL OR kind = sqlc.narg(kind)::club_event_kind)
ORDER BY starts_at ASC
LIMIT sqlc.arg(limit_count) OFFSET sqlc.arg(offset_count);

-- name: ListOrganizerEvents :many
SELECT *
FROM club_events
WHERE club_id = sqlc.arg(club_id)
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::content_status)
ORDER BY starts_at DESC
LIMIT sqlc.arg(limit_count) OFFSET sqlc.arg(offset_count);

-- name: UpsertPage :one
INSERT INTO pages (club_id, slug, title, body_markdown, updated_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (club_id, slug) DO UPDATE
SET title = EXCLUDED.title,
    body_markdown = EXCLUDED.body_markdown,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING *;

-- name: GetPageBySlug :one
SELECT * FROM pages WHERE club_id = $1 AND slug = $2;

-- name: ListPages :many
SELECT * FROM pages WHERE club_id = $1 ORDER BY slug ASC;

-- name: CountNewsCreatedSince :one
SELECT COUNT(*)::int AS count
FROM news_posts
WHERE club_id = $1
  AND created_at >= $2;

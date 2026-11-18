-- name: InsertAuditLog :one
INSERT INTO audit_log (actor_id, action, entity, entity_id, diff, ip, user_agent)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, actor_id, action, entity, entity_id, diff, ip, user_agent, created_at;

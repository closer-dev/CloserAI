-- Все запросы панели фильтруют по tenant_id: бот виден только своей компании.

-- name: CreateBot :one
INSERT INTO bots (tenant_id, tg_bot_id, username, token_enc, webhook_secret,
                  system_prompt, greeting, handoff_threshold, is_active)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListBots :many
SELECT * FROM bots
WHERE tenant_id = $1
ORDER BY created_at;

-- name: GetBot :one
SELECT * FROM bots
WHERE tenant_id = $1 AND id = $2;

-- name: GetBotForUpdate :one
SELECT * FROM bots
WHERE tenant_id = $1 AND id = $2
FOR UPDATE;

-- name: UpdateBot :one
UPDATE bots
SET username          = $3,
    system_prompt     = $4,
    greeting          = $5,
    handoff_threshold = $6,
    is_active         = $7
WHERE tenant_id = $1 AND id = $2
RETURNING *;

-- name: UpdateBotToken :execrows
UPDATE bots
SET token_enc = $3
WHERE tenant_id = $1 AND id = $2;

-- name: DeleteBot :execrows
DELETE FROM bots
WHERE tenant_id = $1 AND id = $2;

-- Системные запросы (без tenant_id) — для контекста telegram.

-- name: GetBotCredentials :one
SELECT id, tenant_id, token_enc, webhook_secret, is_active FROM bots
WHERE id = $1;

-- name: ListActiveBotCredentials :many
SELECT id, tenant_id, token_enc, webhook_secret, is_active FROM bots
WHERE is_active
ORDER BY created_at;

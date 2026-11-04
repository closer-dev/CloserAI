-- name: CreateEmailCode :one
INSERT INTO email_codes (user_id, purpose, code_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetLatestEmailCode :one
SELECT * FROM email_codes
WHERE user_id = $1 AND purpose = $2
ORDER BY created_at DESC
LIMIT 1;

-- Атомарно тратит попытку: строка вернётся, только если лимит ещё не исчерпан.
-- name: ConsumeEmailCodeAttempt :one
UPDATE email_codes SET attempts = attempts + 1
WHERE id = $1 AND attempts < sqlc.arg(max_attempts)::int
RETURNING *;

-- name: DeleteEmailCodes :exec
DELETE FROM email_codes
WHERE user_id = $1 AND purpose = $2;

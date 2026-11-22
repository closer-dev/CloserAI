-- Владелец таблиц customers, conversations, messages — контекст conversation.
-- Запросы панели фильтруют по tenant_id.

-- name: UpsertCustomer :one
-- Профиль в Telegram мог измениться — обновляем его при каждом сообщении.
INSERT INTO customers (tenant_id, tg_user_id, username, first_name, last_name)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (tenant_id, tg_user_id) DO UPDATE
SET username   = EXCLUDED.username,
    first_name = EXCLUDED.first_name,
    last_name  = EXCLUDED.last_name
RETURNING *;

-- name: LockOpenConversation :one
-- Незакрытый диалог клиента с ботом; строка блокируется до конца транзакции,
-- чтобы сообщения одного диалога обрабатывались по очереди.
SELECT c.*, cu.tg_user_id AS chat_id
FROM conversations c
JOIN customers cu ON cu.id = c.customer_id
WHERE c.tenant_id = $1 AND c.bot_id = $2 AND c.customer_id = $3 AND c.status <> 'closed'
FOR UPDATE OF c;

-- name: InsertConversation :execrows
-- Гонку двух первых сообщений решает частичный уникальный индекс
-- conversations_one_open_idx: проигравший ничего не вставит и перечитает диалог.
INSERT INTO conversations (tenant_id, bot_id, customer_id)
VALUES ($1, $2, $3)
ON CONFLICT (bot_id, customer_id) WHERE status <> 'closed' DO NOTHING;

-- name: GetConversation :one
SELECT c.*, cu.tg_user_id AS chat_id
FROM conversations c
JOIN customers cu ON cu.id = c.customer_id
WHERE c.tenant_id = $1 AND c.id = $2;

-- name: InsertMessage :one
-- Повтор того же сообщения Telegram (messages_tg_message_idx) ничего не
-- вставляет и не возвращает строк.
INSERT INTO messages (conversation_id, sender, user_id, text, tg_message_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (conversation_id, tg_message_id) WHERE tg_message_id IS NOT NULL DO NOTHING
RETURNING *;

-- name: TouchConversation :exec
UPDATE conversations
SET last_message_at = GREATEST(last_message_at, $3)
WHERE tenant_id = $1 AND id = $2;

-- name: GetMessage :one
SELECT m.* FROM messages m
JOIN conversations c ON c.id = m.conversation_id
WHERE c.tenant_id = $1 AND m.conversation_id = $2 AND m.id = $3;

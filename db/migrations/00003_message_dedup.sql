-- +goose Up
-- Telegram может прислать один и тот же апдейт повторно (ретрай вебхука,
-- повтор getUpdates после сбоя). id сообщения уникален в пределах чата,
-- поэтому повтор отбрасывается на вставке.
CREATE UNIQUE INDEX messages_tg_message_idx ON messages (conversation_id, tg_message_id)
    WHERE tg_message_id IS NOT NULL;

-- +goose Down
DROP INDEX messages_tg_message_idx;

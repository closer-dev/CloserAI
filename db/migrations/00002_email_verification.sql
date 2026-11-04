-- +goose Up
ALTER TABLE users ADD COLUMN email_verified_at timestamptz;
-- пользователи, созданные до появления подтверждения, считаются подтверждёнными
UPDATE users SET email_verified_at = created_at;

CREATE TABLE email_codes (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose    text NOT NULL CHECK (purpose IN ('verify_email')),
    code_hash  text NOT NULL,
    attempts   int NOT NULL DEFAULT 0,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX email_codes_user_idx ON email_codes (user_id, purpose, created_at DESC);

-- +goose Down
DROP TABLE email_codes;
ALTER TABLE users DROP COLUMN email_verified_at;

-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE tenants (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL,
    plan       text NOT NULL DEFAULT 'free',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    email         text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    name          text NOT NULL DEFAULT '',
    role          text NOT NULL CHECK (role IN ('owner', 'operator')),
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX users_tenant_idx ON users (tenant_id);

CREATE TABLE bots (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    tg_bot_id         bigint NOT NULL UNIQUE,
    username          text NOT NULL,
    token_enc         bytea NOT NULL,
    webhook_secret    text NOT NULL,
    system_prompt     text NOT NULL DEFAULT '',
    greeting          text NOT NULL DEFAULT '',
    handoff_threshold real NOT NULL DEFAULT 0.5,
    is_active         boolean NOT NULL DEFAULT true,
    created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX bots_tenant_idx ON bots (tenant_id);

CREATE TABLE customers (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    tg_user_id bigint NOT NULL,
    username   text NOT NULL DEFAULT '',
    first_name text NOT NULL DEFAULT '',
    last_name  text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, tg_user_id)
);

CREATE TABLE conversations (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    bot_id           uuid NOT NULL REFERENCES bots (id) ON DELETE CASCADE,
    customer_id      uuid NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    status           text NOT NULL DEFAULT 'bot'
                     CHECK (status IN ('bot', 'waiting_operator', 'operator', 'closed')),
    assigned_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    last_message_at  timestamptz NOT NULL DEFAULT now(),
    created_at       timestamptz NOT NULL DEFAULT now(),
    closed_at        timestamptz
);
CREATE INDEX conversations_list_idx ON conversations (tenant_id, status, last_message_at DESC);
-- у клиента с конкретным ботом может быть только один незакрытый диалог
CREATE UNIQUE INDEX conversations_one_open_idx ON conversations (bot_id, customer_id)
    WHERE status <> 'closed';

CREATE TABLE messages (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    sender          text NOT NULL CHECK (sender IN ('customer', 'bot', 'operator', 'system')),
    user_id         uuid REFERENCES users (id) ON DELETE SET NULL,
    text            text NOT NULL,
    tg_message_id   bigint,
    sources         jsonb,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX messages_conversation_idx ON messages (conversation_id, id);

CREATE TABLE documents (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    title       text NOT NULL,
    source_type text NOT NULL CHECK (source_type IN ('file', 'text', 'url')),
    status      text NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'indexing', 'ready', 'failed')),
    error       text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX documents_tenant_idx ON documents (tenant_id);

-- размерность 1024 соответствует bge-m3; при смене модели эмбеддингов поменять
CREATE TABLE chunks (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id   uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    chunk_index int NOT NULL,
    content     text NOT NULL,
    embedding   vector(1024) NOT NULL,
    tsv         tsvector GENERATED ALWAYS AS (to_tsvector('russian', content)) STORED
);
CREATE INDEX chunks_tenant_idx ON chunks (tenant_id);
CREATE INDEX chunks_embedding_idx ON chunks USING hnsw (embedding vector_cosine_ops);
CREATE INDEX chunks_tsv_idx ON chunks USING gin (tsv);

CREATE TABLE verdicts (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL UNIQUE REFERENCES conversations (id) ON DELETE CASCADE,
    resolved        boolean NOT NULL,
    category        text NOT NULL DEFAULT '',
    sentiment       text NOT NULL CHECK (sentiment IN ('positive', 'neutral', 'negative')),
    summary         text NOT NULL DEFAULT '',
    needs_followup  boolean NOT NULL DEFAULT false,
    raw             jsonb,
    created_at      timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE verdicts;
DROP TABLE chunks;
DROP TABLE documents;
DROP TABLE messages;
DROP TABLE conversations;
DROP TABLE customers;
DROP TABLE bots;
DROP TABLE users;
DROP TABLE tenants;

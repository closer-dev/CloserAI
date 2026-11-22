// Package postgres — реализация репозитория контекста bot на sqlc. Токены
// ботов шифруются здесь (AES-GCM, platform/crypto): в БД лежит только token_enc.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"closerai/internal/bot/domain"
	"closerai/internal/bot/infra/postgres/sqlcgen"
	"closerai/internal/platform/crypto"
	"closerai/internal/platform/db"
)

type Repository struct {
	pool   *pgxpool.Pool
	cipher *crypto.Cipher
}

var _ domain.Repository = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool, cipher *crypto.Cipher) *Repository {
	return &Repository{pool: pool, cipher: cipher}
}

func (r *Repository) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(db.Executor(ctx, r.pool))
}

func (r *Repository) Create(ctx context.Context, b domain.Bot, token string) (domain.Bot, error) {
	enc, err := r.cipher.Encrypt([]byte(token))
	if err != nil {
		return domain.Bot{}, fmt.Errorf("bot: encrypt token: %w", err)
	}
	row, err := r.q(ctx).CreateBot(ctx, sqlcgen.CreateBotParams{
		TenantID:         b.TenantID,
		TgBotID:          b.TgBotID,
		Username:         b.Username,
		TokenEnc:         enc,
		WebhookSecret:    b.WebhookSecret,
		SystemPrompt:     b.Settings.SystemPrompt,
		Greeting:         b.Settings.Greeting,
		HandoffThreshold: b.Settings.HandoffThreshold,
		IsActive:         b.IsActive,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique tg_bot_id
			return domain.Bot{}, domain.ErrAlreadyConnected
		}
		return domain.Bot{}, fmt.Errorf("bot: create: %w", err)
	}
	return toDomain(row), nil
}

func (r *Repository) List(ctx context.Context, tenantID uuid.UUID) ([]domain.Bot, error) {
	rows, err := r.q(ctx).ListBots(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("bot: list: %w", err)
	}
	out := make([]domain.Bot, len(rows))
	for i, row := range rows {
		out[i] = toDomain(row)
	}
	return out, nil
}

func (r *Repository) Get(ctx context.Context, tenantID, id uuid.UUID) (domain.Bot, error) {
	return one(r.q(ctx).GetBot(ctx, sqlcgen.GetBotParams{TenantID: tenantID, ID: id}))
}

func (r *Repository) GetForUpdate(ctx context.Context, tenantID, id uuid.UUID) (domain.Bot, error) {
	return one(r.q(ctx).GetBotForUpdate(ctx, sqlcgen.GetBotForUpdateParams{TenantID: tenantID, ID: id}))
}

func (r *Repository) Update(ctx context.Context, b domain.Bot) (domain.Bot, error) {
	return one(r.q(ctx).UpdateBot(ctx, sqlcgen.UpdateBotParams{
		TenantID:         b.TenantID,
		ID:               b.ID,
		Username:         b.Username,
		SystemPrompt:     b.Settings.SystemPrompt,
		Greeting:         b.Settings.Greeting,
		HandoffThreshold: b.Settings.HandoffThreshold,
		IsActive:         b.IsActive,
	}))
}

func (r *Repository) UpdateToken(ctx context.Context, tenantID, id uuid.UUID, token string) error {
	enc, err := r.cipher.Encrypt([]byte(token))
	if err != nil {
		return fmt.Errorf("bot: encrypt token: %w", err)
	}
	n, err := r.q(ctx).UpdateBotToken(ctx, sqlcgen.UpdateBotTokenParams{TenantID: tenantID, ID: id, TokenEnc: enc})
	if err != nil {
		return fmt.Errorf("bot: update token: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	n, err := r.q(ctx).DeleteBot(ctx, sqlcgen.DeleteBotParams{TenantID: tenantID, ID: id})
	if err != nil {
		return fmt.Errorf("bot: delete: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *Repository) Credentials(ctx context.Context, id uuid.UUID) (domain.Credentials, error) {
	row, err := r.q(ctx).GetBotCredentials(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Credentials{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Credentials{}, fmt.Errorf("bot: credentials: %w", err)
	}
	return r.credentials(row)
}

func (r *Repository) ActiveCredentials(ctx context.Context) ([]domain.Credentials, error) {
	rows, err := r.q(ctx).ListActiveBotCredentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("bot: active credentials: %w", err)
	}
	out := make([]domain.Credentials, 0, len(rows))
	for _, row := range rows {
		c, err := r.credentials(sqlcgen.GetBotCredentialsRow(row))
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (r *Repository) credentials(row sqlcgen.GetBotCredentialsRow) (domain.Credentials, error) {
	token, err := r.cipher.Decrypt(row.TokenEnc)
	if err != nil {
		// Обычно значит, что сменили ENCRYPTION_KEY: старые токены не расшифровать.
		return domain.Credentials{}, fmt.Errorf("bot %s: decrypt token (ENCRYPTION_KEY менялся?): %w", row.ID, err)
	}
	return domain.Credentials{
		BotID:         row.ID,
		TenantID:      row.TenantID,
		Token:         string(token),
		WebhookSecret: row.WebhookSecret,
		IsActive:      row.IsActive,
	}, nil
}

func one(row sqlcgen.Bot, err error) (domain.Bot, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Bot{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Bot{}, fmt.Errorf("bot: query: %w", err)
	}
	return toDomain(row), nil
}

func toDomain(row sqlcgen.Bot) domain.Bot {
	return domain.Bot{
		ID:            row.ID,
		TenantID:      row.TenantID,
		TgBotID:       row.TgBotID,
		Username:      row.Username,
		WebhookSecret: row.WebhookSecret,
		Settings: domain.Settings{
			SystemPrompt:     row.SystemPrompt,
			Greeting:         row.Greeting,
			HandoffThreshold: row.HandoffThreshold,
		},
		IsActive:  row.IsActive,
		CreatedAt: row.CreatedAt,
	}
}

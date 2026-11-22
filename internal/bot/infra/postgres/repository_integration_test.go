package postgres_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"closerai/internal/bot/domain"
	"closerai/internal/bot/infra/postgres"
	"closerai/internal/platform/crypto"
	"closerai/internal/platform/db"
)

// Нужен Postgres из `make deps-up`; запускается через `make test-int`.

const testKey = "ZGV2LWVuY3J5cHRpb24ta2V5LTMyLWJ5dGVzLWxvbmc="

func setup(t *testing.T) (*pgxpool.Pool, *postgres.Repository) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL не задан (make test-int)")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, db.Migrate(ctx, pool))
	cipher, err := crypto.New(testKey)
	require.NoError(t, err)
	return pool, postgres.NewRepository(pool, cipher)
}

func newTenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, pool.QueryRow(context.Background(),
		"INSERT INTO tenants (name) VALUES ('test') RETURNING id").Scan(&id))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", id) })
	return id
}

// уникальный tg_bot_id, чтобы повторные прогоны не конфликтовали
func tgID() int64 { return int64(uuid.New().ID())<<8 | 1 }

func TestRepositoryCRUDAndIsolation(t *testing.T) {
	pool, repo := setup(t)
	ctx := context.Background()
	tenantA, tenantB := newTenant(t, pool), newTenant(t, pool)
	token := "123456789:AASecretSecretSecretSecretSecret_-1"

	b, err := repo.Create(ctx, domain.Bot{
		TenantID: tenantA, TgBotID: tgID(), Username: "shop_bot", WebhookSecret: "sec",
		Settings: domain.Settings{SystemPrompt: "p", Greeting: "g", HandoffThreshold: 0.4}, IsActive: true,
	}, token)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, b.ID)
	require.Equal(t, float32(0.4), b.Settings.HandoffThreshold)

	// токен в БД зашифрован
	var enc []byte
	require.NoError(t, pool.QueryRow(ctx, "SELECT token_enc FROM bots WHERE id = $1", b.ID).Scan(&enc))
	require.False(t, bytes.Contains(enc, []byte("SecretSecret")), "токен лежит в БД открытым текстом")

	creds, err := repo.Credentials(ctx, b.ID)
	require.NoError(t, err)
	require.Equal(t, token, creds.Token)
	require.Equal(t, tenantA, creds.TenantID)

	// компания B не видит и не может менять бота компании A
	list, err := repo.List(ctx, tenantB)
	require.NoError(t, err)
	require.Empty(t, list)
	_, err = repo.Get(ctx, tenantB, b.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
	stolen := b
	stolen.TenantID = tenantB
	stolen.Settings.SystemPrompt = "взлом"
	_, err = repo.Update(ctx, stolen)
	require.ErrorIs(t, err, domain.ErrNotFound)
	require.ErrorIs(t, repo.UpdateToken(ctx, tenantB, b.ID, "x"), domain.ErrNotFound)
	require.ErrorIs(t, repo.Delete(ctx, tenantB, b.ID), domain.ErrNotFound)

	got, err := repo.Get(ctx, tenantA, b.ID)
	require.NoError(t, err)
	require.Equal(t, "p", got.Settings.SystemPrompt)

	// тот же бот повторно (в любой компании) — конфликт
	_, err = repo.Create(ctx, domain.Bot{TenantID: tenantB, TgBotID: b.TgBotID, Username: "x", WebhookSecret: "s"}, token)
	require.ErrorIs(t, err, domain.ErrAlreadyConnected)

	// обновление и смена токена
	got.Settings.Greeting = "Привет!"
	got.IsActive = false
	upd, err := repo.Update(ctx, got)
	require.NoError(t, err)
	require.Equal(t, "Привет!", upd.Settings.Greeting)
	require.False(t, upd.IsActive)

	newToken := "123456789:AANewNewNewNewNewNewNewNewNewNew_-12"
	require.NoError(t, repo.UpdateToken(ctx, tenantA, b.ID, newToken))
	creds, err = repo.Credentials(ctx, b.ID)
	require.NoError(t, err)
	require.Equal(t, newToken, creds.Token)

	// выключенный бот не попадает в активные
	active, err := repo.ActiveCredentials(ctx)
	require.NoError(t, err)
	for _, c := range active {
		require.NotEqual(t, b.ID, c.BotID)
	}

	require.NoError(t, repo.Delete(ctx, tenantA, b.ID))
	_, err = repo.Credentials(ctx, b.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestWrongKeyCannotDecrypt(t *testing.T) {
	pool, repo := setup(t)
	ctx := context.Background()
	tenant := newTenant(t, pool)
	b, err := repo.Create(ctx, domain.Bot{TenantID: tenant, TgBotID: tgID(), Username: "x", WebhookSecret: "s", IsActive: true},
		"123456789:AASecretSecretSecretSecretSecret_-1")
	require.NoError(t, err)

	other, err := crypto.New("b3RoZXIta2V5LW90aGVyLWtleS1vdGhlci1rZXktMzI=")
	require.NoError(t, err)
	_, err = postgres.NewRepository(pool, other).Credentials(ctx, b.ID)
	require.ErrorContains(t, err, "ENCRYPTION_KEY")
}

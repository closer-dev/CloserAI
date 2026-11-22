package postgres_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"closerai/internal/conversation/domain"
	"closerai/internal/conversation/infra/postgres"
	"closerai/internal/kernel"
	"closerai/internal/platform/db"
)

// Нужен Postgres из `make deps-up`; запускается через `make test-int`.

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
	return pool, postgres.NewRepository(pool)
}

// newBot создаёт компанию и бота в ней (таблицы чужих контекстов — прямым SQL).
func newBot(t *testing.T, pool *pgxpool.Pool) (tenantID, botID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, pool.QueryRow(ctx, "INSERT INTO tenants (name) VALUES ('test') RETURNING id").Scan(&tenantID))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", tenantID) })
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO bots (tenant_id, tg_bot_id, username, token_enc, webhook_secret)
		VALUES ($1, $2, 'test_bot', '\x00', 'secret') RETURNING id`,
		tenantID, int64(uuid.New().ID())<<8|2).Scan(&botID))
	return tenantID, botID
}

func TestConversationFlow(t *testing.T) {
	pool, repo := setup(t)
	ctx := context.Background()
	tenant, bot := newBot(t, pool)

	cust, err := repo.UpsertCustomer(ctx, domain.Customer{TenantID: tenant, TgUserID: 555, Username: "old", FirstName: "Иван"})
	require.NoError(t, err)
	again, err := repo.UpsertCustomer(ctx, domain.Customer{TenantID: tenant, TgUserID: 555, Username: "new", FirstName: "Иван"})
	require.NoError(t, err)
	require.Equal(t, cust.ID, again.ID, "тот же клиент")
	require.Equal(t, "new", again.Username, "профиль обновляется")

	conv, err := repo.OpenConversation(ctx, tenant, bot, cust.ID)
	require.NoError(t, err)
	require.Equal(t, kernel.StatusBot, conv.Status)
	require.Equal(t, int64(555), conv.ChatID)

	same, err := repo.OpenConversation(ctx, tenant, bot, cust.ID)
	require.NoError(t, err)
	require.Equal(t, conv.ID, same.ID, "незакрытый диалог переиспользуется")

	at := time.Now().Add(time.Minute).Truncate(time.Microsecond)
	m, added, err := repo.AddMessage(ctx, tenant, domain.Message{
		ConversationID: conv.ID, Sender: kernel.SenderCustomer, Text: "привет", TgMessageID: 10, CreatedAt: at,
	})
	require.NoError(t, err)
	require.True(t, added)
	require.NotZero(t, m.ID)

	_, added, err = repo.AddMessage(ctx, tenant, domain.Message{
		ConversationID: conv.ID, Sender: kernel.SenderCustomer, Text: "привет", TgMessageID: 10, CreatedAt: at,
	})
	require.NoError(t, err)
	require.False(t, added, "повтор апдейта отбрасывается")

	got, err := repo.GetMessage(ctx, tenant, conv.ID, m.ID)
	require.NoError(t, err)
	require.Equal(t, "привет", got.Text)
	require.Equal(t, int64(10), got.TgMessageID)

	c, err := repo.GetConversation(ctx, tenant, conv.ID)
	require.NoError(t, err)
	require.WithinDuration(t, at, c.LastMessageAt, time.Millisecond)

	// закрытый диалог не переиспользуется
	_, err = pool.Exec(ctx, "UPDATE conversations SET status = 'closed', closed_at = now() WHERE id = $1", conv.ID)
	require.NoError(t, err)
	fresh, err := repo.OpenConversation(ctx, tenant, bot, cust.ID)
	require.NoError(t, err)
	require.NotEqual(t, conv.ID, fresh.ID)
}

func TestTenantIsolation(t *testing.T) {
	pool, repo := setup(t)
	ctx := context.Background()
	tenantA, botA := newBot(t, pool)
	tenantB, _ := newBot(t, pool)

	cust, err := repo.UpsertCustomer(ctx, domain.Customer{TenantID: tenantA, TgUserID: 1})
	require.NoError(t, err)
	conv, err := repo.OpenConversation(ctx, tenantA, botA, cust.ID)
	require.NoError(t, err)
	m, _, err := repo.AddMessage(ctx, tenantA, domain.Message{
		ConversationID: conv.ID, Sender: kernel.SenderCustomer, Text: "секрет", CreatedAt: time.Now(),
	})
	require.NoError(t, err)

	_, err = repo.GetConversation(ctx, tenantB, conv.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = repo.GetMessage(ctx, tenantB, conv.ID, m.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

// Первые сообщения клиента, пришедшие одновременно, попадают в один диалог.
func TestOpenConversationConcurrent(t *testing.T) {
	pool, repo := setup(t)
	ctx := context.Background()
	tenant, bot := newBot(t, pool)
	cust, err := repo.UpsertCustomer(ctx, domain.Customer{TenantID: tenant, TgUserID: 77})
	require.NoError(t, err)

	const n = 8
	ids := make([]uuid.UUID, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			c, err := repo.OpenConversation(ctx, tenant, bot, cust.ID)
			require.NoError(t, err)
			ids[i] = c.ID
		})
	}
	wg.Wait()
	for _, id := range ids {
		require.Equal(t, ids[0], id)
	}
}

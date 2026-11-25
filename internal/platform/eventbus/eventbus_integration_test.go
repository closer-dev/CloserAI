package eventbus

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"closerai/internal/kernel"
	"closerai/internal/platform/db"
)

func TestPublishAfterCommit(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL не задан (make test-int)")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	pool, err := db.Connect(ctx, url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	tx := db.NewTxManager(pool)
	b := newBus()
	tenantID := uuid.New()
	first := kernel.MessageCreated{TenantID: tenantID, MessageID: 1}
	second := kernel.MessageCreated{TenantID: tenantID, MessageID: 2}
	var got []kernel.Event
	b.SubscribeAll(func(ctx context.Context, e kernel.Event) error {
		_, inTx := db.TxFromContext(ctx)
		require.False(t, inTx, "подписчик не должен получать уже закрытую транзакцию")
		got = append(got, e)
		return nil
	})

	rollback := errors.New("rollback")
	require.ErrorIs(t, tx.Do(ctx, func(ctx context.Context) error {
		b.Publish(ctx, first)
		return rollback
	}), rollback)
	require.Empty(t, got)

	require.NoError(t, tx.Do(ctx, func(ctx context.Context) error {
		batch := []kernel.Event{first}
		b.Publish(ctx, batch...)
		batch[0] = kernel.MessageCreated{TenantID: tenantID, MessageID: 99}

		require.NoError(t, tx.Do(ctx, func(ctx context.Context) error {
			b.Publish(ctx, second)
			return nil
		}))
		require.Empty(t, got, "вложенный Do не должен доставлять события до коммита")
		return nil
	}))
	require.Equal(t, []kernel.Event{first, second}, got)
}

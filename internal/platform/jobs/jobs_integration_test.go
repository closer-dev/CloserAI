package jobs_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"

	"closerai/internal/kernel"
	"closerai/internal/platform/db"
	"closerai/internal/platform/eventbus"
	"closerai/internal/platform/jobs"
)

// Интеграционные тесты: нужен Postgres из `make deps-up` и TEST_DATABASE_URL
// (`make test-int` создаёт отдельную базу closer_ai_test и задаёт переменную).

func testPool(t *testing.T) *pgxpool.Pool {
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
	require.NoError(t, jobs.Migrate(ctx, pool))
	return pool
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type pingArgs struct {
	ID string `json:"id"`
}

func (pingArgs) Kind() string { return "test_ping" }

type pingWorker struct {
	river.WorkerDefaults[pingArgs]
	done chan string
}

func (w *pingWorker) Work(_ context.Context, j *river.Job[pingArgs]) error {
	w.done <- j.Args.ID
	return nil
}

func TestEnqueueFollowsTransaction(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx := db.NewTxManager(pool)

	q := jobs.New(pool, quiet, jobs.Config{})
	w := &pingWorker{done: make(chan string, 10)}
	jobs.Register(q, w)
	require.ErrorIs(t, q.Enqueue(ctx, pingArgs{ID: "early"}, nil), jobs.ErrNotStarted)
	require.NoError(t, q.Start(ctx))
	t.Cleanup(func() { _ = q.Stop(context.Background()) })

	rolledBack, committed := uuid.NewString(), uuid.NewString()

	// откат: задача не должна выполниться
	errBoom := errors.New("boom")
	err := tx.Do(ctx, func(ctx context.Context) error {
		require.NoError(t, q.Enqueue(ctx, pingArgs{ID: rolledBack}, nil))
		return errBoom
	})
	require.ErrorIs(t, err, errBoom)

	// коммит: задача выполняется
	require.NoError(t, tx.Do(ctx, func(ctx context.Context) error {
		return q.Enqueue(ctx, pingArgs{ID: committed}, nil)
	}))
	// вне транзакции тоже работает
	plain := uuid.NewString()
	require.NoError(t, q.Enqueue(ctx, pingArgs{ID: plain}, nil))

	seen := map[string]bool{}
	timeout := time.After(15 * time.Second)
	for !(seen[committed] && seen[plain]) {
		select {
		case id := <-w.done:
			seen[id] = true
		case <-timeout:
			t.Fatalf("задачи не выполнились, получено: %v", seen)
		}
	}
	require.False(t, seen[rolledBack], "задача из откаченной транзакции выполнилась")
}

func TestEventsDeliveredAfterCommitOnly(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx := db.NewTxManager(pool)
	bus := eventbus.New(quiet)

	var got []uuid.UUID
	eventbus.Subscribe(bus, func(ctx context.Context, e kernel.ConversationClosed) error {
		// обработчик работает уже вне транзакции и может ходить в БД через пул
		_, inTx := db.TxFromContext(ctx)
		require.False(t, inTx)
		var one int
		require.NoError(t, db.Executor(ctx, pool).QueryRow(ctx, "SELECT 1").Scan(&one))
		got = append(got, e.ConversationID)
		return nil
	})

	rolledBack, committed := uuid.New(), uuid.New()

	_ = tx.Do(ctx, func(ctx context.Context) error {
		bus.Publish(ctx, kernel.ConversationClosed{ConversationID: rolledBack})
		return errors.New("rollback")
	})
	require.Empty(t, got)

	require.NoError(t, tx.Do(ctx, func(ctx context.Context) error {
		bus.Publish(ctx, kernel.ConversationClosed{ConversationID: committed})
		require.Empty(t, got, "событие доставлено до коммита")
		return nil
	}))
	require.Equal(t, []uuid.UUID{committed}, got)
}

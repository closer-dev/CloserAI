package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX — то, что умеют и пул, и транзакция. Совпадает с интерфейсом,
// который ждёт код, сгенерированный sqlc.
type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type txKey struct{}

// TxManager — unit of work. Прикладной слой открывает транзакцию через Do, а
// репозитории любых контекстов внутри fn автоматически работают в ней (берут
// её из ctx через Executor). Так use case может атомарно изменить агрегаты
// разных контекстов, не зная про pgx.
type TxManager struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManager { return &TxManager{pool: pool} }

// Do выполняет fn в транзакции. Если транзакция уже открыта выше по стеку,
// fn выполняется в ней же (вложенные Do не создают новых транзакций).
func (m *TxManager) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	return pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		return fn(context.WithValue(ctx, txKey{}, tx))
	})
}

// Executor возвращает текущую транзакцию из ctx, а если её нет — пул.
// Используется репозиториями в слое infra.
func Executor(ctx context.Context, pool *pgxpool.Pool) DBTX {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}

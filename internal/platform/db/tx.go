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

// txState — открытая транзакция и действия, отложенные до её коммита.
type txState struct {
	tx          pgx.Tx
	afterCommit []func(context.Context)
}

func stateFrom(ctx context.Context) *txState {
	st, _ := ctx.Value(txKey{}).(*txState)
	return st
}

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
//
// После успешного коммита выполняются действия, зарегистрированные через
// AfterCommit (например, рассылка доменных событий). При откате они
// отбрасываются.
func (m *TxManager) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if stateFrom(ctx) != nil {
		return fn(ctx)
	}
	st := &txState{}
	err := pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		st.tx = tx
		return fn(context.WithValue(ctx, txKey{}, st))
	})
	if err != nil {
		return err
	}
	// Хуки получают исходный ctx без транзакции: она уже закрыта, и запросы
	// внутри хуков должны идти через пул.
	for _, h := range st.afterCommit {
		h(ctx)
	}
	return nil
}

// AfterCommit откладывает fn до коммита транзакции из ctx. Если транзакции
// нет, fn выполняется сразу.
func AfterCommit(ctx context.Context, fn func(ctx context.Context)) {
	if st := stateFrom(ctx); st != nil {
		st.afterCommit = append(st.afterCommit, fn)
		return
	}
	fn(ctx)
}

// TxFromContext возвращает транзакцию, открытую через TxManager.Do, если она есть.
// Нужна адаптерам, которые сами работают с pgx.Tx (например, очередь задач).
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	if st := stateFrom(ctx); st != nil {
		return st.tx, true
	}
	return nil, false
}

// Executor возвращает текущую транзакцию из ctx, а если её нет — пул.
// Используется репозиториями в слое infra.
func Executor(ctx context.Context, pool *pgxpool.Pool) DBTX {
	if st := stateFrom(ctx); st != nil {
		return st.tx
	}
	return pool
}

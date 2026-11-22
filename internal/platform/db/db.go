// Package db — пул соединений PostgreSQL и применение миграций.
// Запросы каждый контекст держит у себя: internal/<контекст>/infra/postgres
// (sqlc генерирует код в sqlcgen, см. sqlc.yaml). Транзакции — tx.go.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
	"github.com/pressly/goose/v3"

	"closerai/db/migrations"
)

func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("db: parse config: %w", err)
	}
	if err := ensureVectorExtension(ctx, cfg.ConnConfig); err != nil {
		return nil, err
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if err := pgxvec.RegisterTypes(ctx, conn); err != nil {
			return fmt.Errorf("db: register vector types: %w", err)
		}
		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}

func ensureVectorExtension(ctx context.Context, cfg *pgx.ConnConfig) error {
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("db: connect for vector extension: %w", err)
	}
	defer conn.Close(context.Background())

	if _, err := conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("db: create vector extension: %w", err)
	}
	return nil
}

// Migrate применяет встроенные в бинарник goose-миграции из db/migrations.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		return fmt.Errorf("db: migrate: %w", err)
	}
	return nil
}

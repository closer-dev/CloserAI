// Package postgres — реализация репозиториев контекста tenant на sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"closerai/internal/platform/db"
	"closerai/internal/tenant/domain"
	"closerai/internal/tenant/infra/postgres/sqlcgen"
)

type Repository struct {
	pool *pgxpool.Pool
}

var _ domain.Repository = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(db.Executor(ctx, r.pool))
}

func (r *Repository) Create(ctx context.Context, name string) (domain.Tenant, error) {
	t, err := r.q(ctx).CreateTenant(ctx, name)
	if err != nil {
		return domain.Tenant{}, fmt.Errorf("tenant: create: %w", err)
	}
	return toDomain(t), nil
}

func (r *Repository) ByID(ctx context.Context, id uuid.UUID) (domain.Tenant, error) {
	t, err := r.q(ctx).GetTenant(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Tenant{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Tenant{}, fmt.Errorf("tenant: get: %w", err)
	}
	return toDomain(t), nil
}

func toDomain(t sqlcgen.Tenant) domain.Tenant {
	return domain.Tenant{ID: t.ID, Name: t.Name, Plan: t.Plan, CreatedAt: t.CreatedAt}
}

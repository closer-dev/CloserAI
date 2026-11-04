// Package tenant — компании-клиенты (арендаторы) и их тарифы.
package tenant

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"closerai/internal/platform/db/store"
	"closerai/internal/platform/httpx"
)

type Tenant struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Plan      string    `json:"plan"`
	CreatedAt time.Time `json:"created_at"`
}

func fromStore(t store.Tenant) Tenant {
	return Tenant{ID: t.ID, Name: t.Name, Plan: t.Plan, CreatedAt: t.CreatedAt}
}

type Service struct {
	q *store.Queries
}

func NewService(db store.DBTX) *Service {
	return &Service{q: store.New(db)}
}

// CreateTx создаёт компанию внутри внешней транзакции — её открывает вызывающий
// модуль (например, auth при регистрации создаёт компанию и владельца атомарно).
func (s *Service) CreateTx(ctx context.Context, tx pgx.Tx, name string) (Tenant, error) {
	t, err := s.q.WithTx(tx).CreateTenant(ctx, name)
	if err != nil {
		return Tenant{}, fmt.Errorf("tenant: create: %w", err)
	}
	return fromStore(t), nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Tenant, error) {
	t, err := s.q.GetTenant(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, httpx.ErrNotFound
	}
	if err != nil {
		return Tenant{}, fmt.Errorf("tenant: get: %w", err)
	}
	return fromStore(t), nil
}

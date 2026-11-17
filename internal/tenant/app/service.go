// Package app — прикладной слой контекста tenant: сценарии использования.
// Это публичный API контекста для других контекстов (через интерфейсы).
package app

import (
	"context"

	"github.com/google/uuid"

	"closerai/internal/tenant/domain"
)

type Service struct {
	repo domain.Repository
}

func NewService(repo domain.Repository) *Service { return &Service{repo: repo} }

// Create создаёт компанию. Если вызывающий открыл транзакцию (TxManager.Do),
// компания создаётся в ней — так auth при регистрации создаёт компанию и
// владельца атомарно.
func (s *Service) Create(ctx context.Context, name string) (domain.Tenant, error) {
	name, err := domain.NormalizeName(name)
	if err != nil {
		return domain.Tenant{}, err
	}
	return s.repo.Create(ctx, name)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.Tenant, error) {
	return s.repo.ByID(ctx, id)
}

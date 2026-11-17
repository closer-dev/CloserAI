// Package domain — доменная модель контекста tenant: компания-клиент
// (арендатор) и её тариф. Не зависит от HTTP, БД и других контекстов.
package domain

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound  = errors.New("tenant: not found")
	ErrEmptyName = errors.New("tenant: name is empty")
)

const PlanFree = "free"

// Tenant — корень агрегата.
type Tenant struct {
	ID        uuid.UUID
	Name      string
	Plan      string
	CreatedAt time.Time
}

// NormalizeName приводит название к хранимому виду и проверяет инварианты.
func NormalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrEmptyName
	}
	return name, nil
}

// Repository — порт хранения. Реализация — в infra/postgres.
// Методы работают в транзакции, если она открыта в ctx (см. platform/db.TxManager).
type Repository interface {
	Create(ctx context.Context, name string) (Tenant, error)
	ByID(ctx context.Context, id uuid.UUID) (Tenant, error)
}

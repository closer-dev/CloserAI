// Package app — прикладной слой контекста auth: сценарии использования
// (регистрация, вход, подтверждение email, управление сотрудниками).
// Зависит только от domain и портов ниже; реализации портов — в infra.
package app

import (
	"context"

	"github.com/google/uuid"

	"closerai/internal/auth/domain"
	"closerai/internal/kernel"
)

// TxManager — unit of work (реализация: platform/db.TxManager).
type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// TenantCreator — то, что auth нужно от контекста tenant. Auth не импортирует
// tenant: адаптер подставляется при сборке в main.
type TenantCreator interface {
	CreateTenant(ctx context.Context, name string) (uuid.UUID, error)
}

// TenantCreatorFunc позволяет передать функцию как TenantCreator.
type TenantCreatorFunc func(ctx context.Context, name string) (uuid.UUID, error)

func (f TenantCreatorFunc) CreateTenant(ctx context.Context, name string) (uuid.UUID, error) {
	return f(ctx, name)
}

type PasswordHasher interface {
	Hash(password string) (string, error)
	Matches(hash, password string) bool
}

// Tokens выпускает и проверяет сессионные токены.
type Tokens interface {
	Issue(id kernel.Identity) (string, error)
	Parse(raw string) (kernel.Identity, error)
}

// Codes генерирует одноразовые коды и хэширует их.
type Codes interface {
	Generate() (string, error)
	Hash(code string) string
	Equal(code, hash string) bool
}

// Notifier доставляет пользователю код подтверждения (почта, лог и т.п.).
type Notifier interface {
	SendVerifyCode(ctx context.Context, u domain.User, code string) error
}

// Package domain — доменная модель контекста auth: пользователь (сотрудник
// компании) и коды подтверждения email. Не зависит от HTTP, БД, JWT и SMTP.
package domain

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"closerai/internal/kernel"
)

var (
	ErrUserNotFound       = errors.New("auth: user not found")
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrEmailTaken         = errors.New("auth: email taken")
	ErrEmailNotVerified   = errors.New("auth: email not verified")
	ErrAlreadyVerified    = errors.New("auth: email already verified")
	ErrInvalidRole        = errors.New("auth: invalid role")
	ErrEmptyCompanyName   = errors.New("auth: empty company name")
)

// User — корень агрегата «Пользователь».
type User struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	Email           string
	PasswordHash    string
	Name            string
	Role            kernel.Role
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
}

func (u User) EmailVerified() bool { return u.EmailVerifiedAt != nil }

func (u User) Identity() kernel.Identity {
	return kernel.Identity{UserID: u.ID, TenantID: u.TenantID, Role: u.Role, EmailVerified: u.EmailVerified()}
}

// NewUser — фабрика нового (ещё не сохранённого) пользователя. Нормализует
// поля и проверяет инварианты; ID и даты проставит хранилище.
func NewUser(tenantID uuid.UUID, email, name string, role kernel.Role, passwordHash string) (User, error) {
	if !role.Valid() {
		return User{}, ErrInvalidRole
	}
	return User{
		TenantID:     tenantID,
		Email:        NormalizeEmail(email),
		Name:         strings.TrimSpace(name),
		Role:         role,
		PasswordHash: passwordHash,
	}, nil
}

func NormalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// UserRepository — порт хранения пользователей. Методы работают в транзакции,
// если она открыта в ctx (см. platform/db.TxManager).
type UserRepository interface {
	// Create возвращает ErrEmailTaken, если email уже занят.
	Create(ctx context.Context, u User) (User, error)
	// ByEmail ищет по всем компаниям (email уникален глобально), ErrUserNotFound если нет.
	ByEmail(ctx context.Context, email string) (User, error)
	// ByID ищет только внутри tenant, ErrUserNotFound если нет.
	ByID(ctx context.Context, tenantID, id uuid.UUID) (User, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]User, error)
	MarkEmailVerified(ctx context.Context, id uuid.UUID) (User, error)
}

package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"closerai/internal/auth/domain"
	"closerai/internal/kernel"
)

type Deps struct {
	Tx      TxManager
	Users   domain.UserRepository
	Codes   domain.EmailCodeRepository
	Tenants TenantCreator

	Hasher   PasswordHasher
	Tokens   Tokens
	CodeGen  Codes
	Notifier Notifier
}

type Service struct {
	Deps
	now func() time.Time
	// dummyHash сравнивается при входе с несуществующим email, чтобы время
	// ответа не выдавало, зарегистрирован ли адрес.
	dummyHash string
}

func NewService(d Deps) (*Service, error) {
	dummy, err := d.Hasher.Hash("dummy-password")
	if err != nil {
		return nil, fmt.Errorf("auth: dummy hash: %w", err)
	}
	return &Service{Deps: d, now: time.Now, dummyHash: dummy}, nil
}

type RegisterCmd struct {
	CompanyName string
	Name        string
	Email       string
	Password    string
}

// Register создаёт компанию и её владельца в одной транзакции и сразу выдаёт токен.
func (s *Service) Register(ctx context.Context, cmd RegisterCmd) (domain.User, string, error) {
	if strings.TrimSpace(cmd.CompanyName) == "" {
		return domain.User{}, "", domain.ErrEmptyCompanyName
	}
	hash, err := s.Hasher.Hash(cmd.Password)
	if err != nil {
		return domain.User{}, "", err
	}

	var u domain.User
	err = s.Tx.Do(ctx, func(ctx context.Context) error {
		tenantID, err := s.Tenants.CreateTenant(ctx, cmd.CompanyName)
		if err != nil {
			return err
		}
		nu, err := domain.NewUser(tenantID, cmd.Email, cmd.Name, kernel.RoleOwner, hash)
		if err != nil {
			return err
		}
		u, err = s.Users.Create(ctx, nu)
		return err
	})
	if err != nil {
		return domain.User{}, "", err
	}

	// Письмо не должно ломать регистрацию: если отправка не удалась,
	// пользователь запросит код повторно с экрана подтверждения.
	if err := s.sendVerifyCode(ctx, u); err != nil {
		slog.ErrorContext(ctx, "send verify code", "user_id", u.ID, "err", err)
	}
	tok, err := s.Tokens.Issue(u.Identity())
	return u, tok, err
}

type LoginCmd struct {
	Email    string
	Password string
}

func (s *Service) Login(ctx context.Context, cmd LoginCmd) (domain.User, string, error) {
	u, err := s.Users.ByEmail(ctx, domain.NormalizeEmail(cmd.Email))
	if errors.Is(err, domain.ErrUserNotFound) {
		_ = s.Hasher.Matches(s.dummyHash, cmd.Password)
		return domain.User{}, "", domain.ErrInvalidCredentials
	}
	if err != nil {
		return domain.User{}, "", err
	}
	if !s.Hasher.Matches(u.PasswordHash, cmd.Password) {
		return domain.User{}, "", domain.ErrInvalidCredentials
	}

	if !u.EmailVerified() {
		// Например, сотрудник, которого добавил владелец, входит впервые.
		if err := s.ensureVerifyCode(ctx, u); err != nil {
			slog.ErrorContext(ctx, "send verify code", "user_id", u.ID, "err", err)
		}
	}
	tok, err := s.Tokens.Issue(u.Identity())
	return u, tok, err
}

// Authenticate проверяет токен. Используется middleware.
func (s *Service) Authenticate(raw string) (kernel.Identity, error) {
	return s.Tokens.Parse(raw)
}

func (s *Service) Me(ctx context.Context, id kernel.Identity) (domain.User, error) {
	return s.Users.ByID(ctx, id.TenantID, id.UserID)
}

func (s *Service) ListUsers(ctx context.Context, tenantID uuid.UUID) ([]domain.User, error) {
	return s.Users.ListByTenant(ctx, tenantID)
}

type CreateUserCmd struct {
	Name     string
	Email    string
	Password string
	Role     kernel.Role
}

// CreateUser добавляет сотрудника в компанию. Пока без писем-приглашений:
// владелец задаёт временный пароль и передаёт его сотруднику сам.
func (s *Service) CreateUser(ctx context.Context, tenantID uuid.UUID, cmd CreateUserCmd) (domain.User, error) {
	hash, err := s.Hasher.Hash(cmd.Password)
	if err != nil {
		return domain.User{}, err
	}
	nu, err := domain.NewUser(tenantID, cmd.Email, cmd.Name, cmd.Role, hash)
	if err != nil {
		return domain.User{}, err
	}
	return s.Users.Create(ctx, nu)
}

package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"closerai/internal/platform/db/store"
	"closerai/internal/platform/httpx"
	"closerai/internal/platform/mailer"
)

var (
	ErrInvalidCredentials = httpx.NewError(http.StatusUnauthorized, "неверный email или пароль")
	ErrEmailTaken         = httpx.NewError(http.StatusConflict, "пользователь с таким email уже существует")
)

// CreateTenantFunc создаёт компанию в переданной транзакции. Модуль auth не
// импортирует tenant напрямую — зависимость передаётся при сборке в main.
type CreateTenantFunc func(ctx context.Context, tx pgx.Tx, name string) (uuid.UUID, error)

type User struct {
	ID            uuid.UUID `json:"id"`
	TenantID      uuid.UUID `json:"tenant_id"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	Role          Role      `json:"role"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
}

func fromStore(u store.User) User {
	return User{
		ID: u.ID, TenantID: u.TenantID, Email: u.Email, Name: u.Name, Role: Role(u.Role),
		EmailVerified: u.EmailVerifiedAt != nil, CreatedAt: u.CreatedAt,
	}
}

func (u User) identity() Identity {
	return Identity{UserID: u.ID, TenantID: u.TenantID, Role: u.Role, EmailVerified: u.EmailVerified}
}

type Service struct {
	pool         *pgxpool.Pool
	q            *store.Queries
	createTenant CreateTenantFunc
	tokens       *tokens
	codes        *codes
	mail         mailer.Sender
	// dummyHash сравнивается при входе с несуществующим email, чтобы время
	// ответа не выдавало, зарегистрирован ли адрес.
	dummyHash []byte
}

func NewService(pool *pgxpool.Pool, createTenant CreateTenantFunc, jwtSecret string, mail mailer.Sender) *Service {
	dummy, _ := bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)
	return &Service{
		pool:         pool,
		q:            store.New(pool),
		createTenant: createTenant,
		tokens:       newTokens(jwtSecret),
		codes:        newCodes(jwtSecret),
		mail:         mail,
		dummyHash:    dummy,
	}
}

type RegisterInput struct {
	CompanyName string `json:"company_name" binding:"required,max=200"`
	Name        string `json:"name" binding:"required,max=200"`
	Email       string `json:"email" binding:"required,email,max=254"`
	Password    string `json:"password" binding:"required,min=8,max=72"`
}

// Register создаёт компанию и её владельца в одной транзакции и сразу выдаёт токен.
func (s *Service) Register(ctx context.Context, in RegisterInput) (User, string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, "", fmt.Errorf("auth: hash: %w", err)
	}

	var u store.User
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tenantID, err := s.createTenant(ctx, tx, strings.TrimSpace(in.CompanyName))
		if err != nil {
			return err
		}
		u, err = s.q.WithTx(tx).CreateUser(ctx, store.CreateUserParams{
			TenantID:     tenantID,
			Email:        normalizeEmail(in.Email),
			PasswordHash: string(hash),
			Name:         strings.TrimSpace(in.Name),
			Role:         string(RoleOwner),
		})
		return mapUserErr(err)
	})
	if err != nil {
		return User{}, "", err
	}

	user := fromStore(u)
	// Письмо не должно ломать регистрацию: если отправка не удалась,
	// пользователь запросит код повторно с экрана подтверждения.
	if err := s.sendVerifyCode(ctx, user); err != nil {
		slog.ErrorContext(ctx, "send verify code", "user_id", user.ID, "err", err)
	}
	tok, err := s.tokens.issue(user.identity())
	return user, tok, err
}

type LoginInput struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func (s *Service) Login(ctx context.Context, in LoginInput) (User, string, error) {
	u, err := s.q.GetUserByEmail(ctx, normalizeEmail(in.Email))
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(in.Password))
		return User{}, "", ErrInvalidCredentials
	}
	if err != nil {
		return User{}, "", fmt.Errorf("auth: get user: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
		return User{}, "", ErrInvalidCredentials
	}

	user := fromStore(u)
	if !user.EmailVerified {
		// Например, сотрудник, которого добавил владелец, входит впервые.
		if err := s.ensureVerifyCode(ctx, user); err != nil {
			slog.ErrorContext(ctx, "send verify code", "user_id", user.ID, "err", err)
		}
	}
	tok, err := s.tokens.issue(user.identity())
	return user, tok, err
}

// Authenticate проверяет токен. Используется middleware.
func (s *Service) Authenticate(raw string) (Identity, error) {
	return s.tokens.parse(raw)
}

func (s *Service) Me(ctx context.Context, id Identity) (User, error) {
	u, err := s.q.GetUser(ctx, store.GetUserParams{TenantID: id.TenantID, ID: id.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, httpx.ErrUnauthorized
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: get user: %w", err)
	}
	return fromStore(u), nil
}

func (s *Service) ListUsers(ctx context.Context, tenantID uuid.UUID) ([]User, error) {
	rows, err := s.q.ListUsers(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("auth: list users: %w", err)
	}
	out := make([]User, len(rows))
	for i, r := range rows {
		out[i] = fromStore(r)
	}
	return out, nil
}

type CreateUserInput struct {
	Name     string `json:"name" binding:"required,max=200"`
	Email    string `json:"email" binding:"required,email,max=254"`
	Password string `json:"password" binding:"required,min=8,max=72"`
	Role     Role   `json:"role" binding:"required,oneof=owner operator"`
}

// CreateUser добавляет сотрудника в компанию. Пока без писем-приглашений:
// владелец задаёт временный пароль и передаёт его сотруднику сам.
func (s *Service) CreateUser(ctx context.Context, tenantID uuid.UUID, in CreateUserInput) (User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, fmt.Errorf("auth: hash: %w", err)
	}
	u, err := s.q.CreateUser(ctx, store.CreateUserParams{
		TenantID:     tenantID,
		Email:        normalizeEmail(in.Email),
		PasswordHash: string(hash),
		Name:         strings.TrimSpace(in.Name),
		Role:         string(in.Role),
	})
	if err := mapUserErr(err); err != nil {
		return User{}, err
	}
	return fromStore(u), nil
}

func normalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

func mapUserErr(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrEmailTaken
	}
	return fmt.Errorf("auth: create user: %w", err)
}

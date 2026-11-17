// Package postgres — реализация репозиториев контекста auth на sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"closerai/internal/auth/domain"
	"closerai/internal/auth/infra/postgres/sqlcgen"
	"closerai/internal/kernel"
	"closerai/internal/platform/db"
)

type base struct {
	pool *pgxpool.Pool
}

func (b base) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(db.Executor(ctx, b.pool))
}

// ---------- users ----------

type UserRepository struct{ base }

var _ domain.UserRepository = (*UserRepository)(nil)

func NewUserRepository(pool *pgxpool.Pool) *UserRepository { return &UserRepository{base{pool}} }

func (r *UserRepository) Create(ctx context.Context, u domain.User) (domain.User, error) {
	row, err := r.q(ctx).CreateUser(ctx, sqlcgen.CreateUserParams{
		TenantID:     u.TenantID,
		Email:        u.Email,
		PasswordHash: u.PasswordHash,
		Name:         u.Name,
		Role:         string(u.Role),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.User{}, domain.ErrEmailTaken
		}
		return domain.User{}, fmt.Errorf("auth: create user: %w", err)
	}
	return userToDomain(row), nil
}

func (r *UserRepository) ByEmail(ctx context.Context, email string) (domain.User, error) {
	return oneUser(r.q(ctx).GetUserByEmail(ctx, email))
}

func (r *UserRepository) ByID(ctx context.Context, tenantID, id uuid.UUID) (domain.User, error) {
	return oneUser(r.q(ctx).GetUser(ctx, sqlcgen.GetUserParams{TenantID: tenantID, ID: id}))
}

func (r *UserRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]domain.User, error) {
	rows, err := r.q(ctx).ListUsers(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("auth: list users: %w", err)
	}
	out := make([]domain.User, len(rows))
	for i, row := range rows {
		out[i] = userToDomain(row)
	}
	return out, nil
}

func (r *UserRepository) MarkEmailVerified(ctx context.Context, id uuid.UUID) (domain.User, error) {
	return oneUser(r.q(ctx).MarkEmailVerified(ctx, id))
}

func oneUser(row sqlcgen.User, err error) (domain.User, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrUserNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("auth: get user: %w", err)
	}
	return userToDomain(row), nil
}

func userToDomain(u sqlcgen.User) domain.User {
	return domain.User{
		ID: u.ID, TenantID: u.TenantID, Email: u.Email, PasswordHash: u.PasswordHash,
		Name: u.Name, Role: kernel.Role(u.Role), EmailVerifiedAt: u.EmailVerifiedAt, CreatedAt: u.CreatedAt,
	}
}

// ---------- email codes ----------

type EmailCodeRepository struct{ base }

var _ domain.EmailCodeRepository = (*EmailCodeRepository)(nil)

func NewEmailCodeRepository(pool *pgxpool.Pool) *EmailCodeRepository {
	return &EmailCodeRepository{base{pool}}
}

func (r *EmailCodeRepository) Create(ctx context.Context, c domain.EmailCode) (domain.EmailCode, error) {
	row, err := r.q(ctx).CreateEmailCode(ctx, sqlcgen.CreateEmailCodeParams{
		UserID: c.UserID, Purpose: c.Purpose, CodeHash: c.CodeHash, ExpiresAt: c.ExpiresAt,
	})
	if err != nil {
		return domain.EmailCode{}, fmt.Errorf("auth: save code: %w", err)
	}
	return codeToDomain(row), nil
}

func (r *EmailCodeRepository) Latest(ctx context.Context, userID uuid.UUID, purpose string) (domain.EmailCode, error) {
	row, err := r.q(ctx).GetLatestEmailCode(ctx, sqlcgen.GetLatestEmailCodeParams{UserID: userID, Purpose: purpose})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EmailCode{}, domain.ErrCodeNotFound
	}
	if err != nil {
		return domain.EmailCode{}, fmt.Errorf("auth: get code: %w", err)
	}
	return codeToDomain(row), nil
}

func (r *EmailCodeRepository) ConsumeAttempt(ctx context.Context, id int64, maxAttempts int) error {
	_, err := r.q(ctx).ConsumeEmailCodeAttempt(ctx, sqlcgen.ConsumeEmailCodeAttemptParams{ID: id, MaxAttempts: int32(maxAttempts)})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrCodeAttempts
	}
	if err != nil {
		return fmt.Errorf("auth: consume attempt: %w", err)
	}
	return nil
}

func (r *EmailCodeRepository) DeleteAll(ctx context.Context, userID uuid.UUID, purpose string) error {
	if err := r.q(ctx).DeleteEmailCodes(ctx, sqlcgen.DeleteEmailCodesParams{UserID: userID, Purpose: purpose}); err != nil {
		return fmt.Errorf("auth: delete codes: %w", err)
	}
	return nil
}

func codeToDomain(c sqlcgen.EmailCode) domain.EmailCode {
	return domain.EmailCode{
		ID: c.ID, UserID: c.UserID, Purpose: c.Purpose, CodeHash: c.CodeHash,
		Attempts: int(c.Attempts), ExpiresAt: c.ExpiresAt, CreatedAt: c.CreatedAt,
	}
}

package domain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	PurposeVerifyEmail = "verify_email"

	CodeTTL            = 15 * time.Minute
	CodeMaxAttempts    = 5
	CodeResendCooldown = 60 * time.Second
)

var (
	ErrCodeNotFound = errors.New("auth: code not found")
	ErrCodeInvalid  = errors.New("auth: code invalid")
	ErrCodeExpired  = errors.New("auth: code expired")
	ErrCodeAttempts = errors.New("auth: too many attempts")
)

// ResendTooEarlyError — повторная отправка раньше, чем через CodeResendCooldown.
type ResendTooEarlyError struct {
	Wait time.Duration
}

func (e *ResendTooEarlyError) Error() string {
	return fmt.Sprintf("auth: resend too early, wait %s", e.Wait)
}

// EmailCode — одноразовый код подтверждения. Хранится только хэш кода.
type EmailCode struct {
	ID        int64
	UserID    uuid.UUID
	Purpose   string
	CodeHash  string
	Attempts  int
	ExpiresAt time.Time
	CreatedAt time.Time
}

func NewEmailCode(userID uuid.UUID, purpose, hash string, now time.Time) EmailCode {
	return EmailCode{UserID: userID, Purpose: purpose, CodeHash: hash, ExpiresAt: now.Add(CodeTTL)}
}

func (c EmailCode) Expired(now time.Time) bool { return now.After(c.ExpiresAt) }

// Usable — код ещё можно ввести: не истёк и попытки не исчерпаны.
func (c EmailCode) Usable(now time.Time) bool {
	return now.Before(c.ExpiresAt) && c.Attempts < CodeMaxAttempts
}

// ResendWait — сколько ещё ждать до разрешённой повторной отправки (0 — можно).
func (c EmailCode) ResendWait(now time.Time) time.Duration {
	return max(0, CodeResendCooldown-now.Sub(c.CreatedAt))
}

// EmailCodeRepository — порт хранения кодов.
type EmailCodeRepository interface {
	Create(ctx context.Context, c EmailCode) (EmailCode, error)
	// Latest возвращает самый свежий код, ErrCodeNotFound если кодов нет.
	Latest(ctx context.Context, userID uuid.UUID, purpose string) (EmailCode, error)
	// ConsumeAttempt атомарно тратит попытку; ErrCodeAttempts, если лимит исчерпан.
	ConsumeAttempt(ctx context.Context, id int64, maxAttempts int) error
	DeleteAll(ctx context.Context, userID uuid.UUID, purpose string) error
}

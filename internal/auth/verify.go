package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"closerai/internal/platform/db/store"
	"closerai/internal/platform/httpx"
	"closerai/internal/platform/mailer"
)

const (
	purposeVerifyEmail = "verify_email"
	codeTTL            = 15 * time.Minute
	codeMaxAttempts    = 5
	codeResendCooldown = 60 * time.Second
)

var (
	ErrEmailNotVerified = httpx.NewError(http.StatusForbidden, "email не подтверждён")
	ErrAlreadyVerified  = httpx.NewError(http.StatusConflict, "email уже подтверждён")
	ErrCodeInvalid      = httpx.NewError(http.StatusBadRequest, "неверный код")
	ErrCodeExpired      = httpx.NewError(http.StatusBadRequest, "код истёк или не запрашивался — отправьте новый")
	ErrCodeAttempts     = httpx.NewError(http.StatusTooManyRequests, "слишком много попыток — отправьте новый код")
)

// codes генерирует 6-значные коды и хэширует их HMAC-ом. Простой sha256 от
// 6 цифр перебирается за миллисекунды, а без серверного ключа HMAC не подобрать.
type codes struct {
	key []byte
}

func newCodes(secret string) *codes {
	k := sha256.Sum256([]byte("email-code:" + secret))
	return &codes{key: k[:]}
}

func (c *codes) generate() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("auth: generate code: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func (c *codes) hash(code string) string {
	m := hmac.New(sha256.New, c.key)
	m.Write([]byte(code))
	return hex.EncodeToString(m.Sum(nil))
}

func (c *codes) equal(code, hash string) bool {
	return hmac.Equal([]byte(c.hash(code)), []byte(hash))
}

// sendVerifyCode заменяет все прежние коды пользователя новым и отправляет его.
func (s *Service) sendVerifyCode(ctx context.Context, u User) error {
	code, err := s.codes.generate()
	if err != nil {
		return err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.DeleteEmailCodes(ctx, store.DeleteEmailCodesParams{UserID: u.ID, Purpose: purposeVerifyEmail}); err != nil {
			return err
		}
		_, err := q.CreateEmailCode(ctx, store.CreateEmailCodeParams{
			UserID:    u.ID,
			Purpose:   purposeVerifyEmail,
			CodeHash:  s.codes.hash(code),
			ExpiresAt: time.Now().Add(codeTTL),
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("auth: save code: %w", err)
	}
	return s.mail.Send(ctx, mailer.Message{
		To:      u.Email,
		Subject: "Код подтверждения CloserAI: " + code,
		Text: fmt.Sprintf("Здравствуйте, %s!\n\nВаш код подтверждения: %s\n\n"+
			"Код действует %d минут. Если вы не регистрировались в CloserAI, просто проигнорируйте это письмо.\n",
			u.Name, code, int(codeTTL/time.Minute)),
	})
}

// ensureVerifyCode отправляет код, только если действующего ещё нет.
func (s *Service) ensureVerifyCode(ctx context.Context, u User) error {
	c, err := s.q.GetLatestEmailCode(ctx, store.GetLatestEmailCodeParams{UserID: u.ID, Purpose: purposeVerifyEmail})
	if err == nil && time.Now().Before(c.ExpiresAt) && c.Attempts < codeMaxAttempts {
		return nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("auth: get code: %w", err)
	}
	return s.sendVerifyCode(ctx, u)
}

// ResendVerifyCode — повторная отправка по кнопке, не чаще раза в codeResendCooldown.
func (s *Service) ResendVerifyCode(ctx context.Context, id Identity) error {
	u, err := s.Me(ctx, id)
	if err != nil {
		return err
	}
	if u.EmailVerified {
		return ErrAlreadyVerified
	}
	c, err := s.q.GetLatestEmailCode(ctx, store.GetLatestEmailCodeParams{UserID: u.ID, Purpose: purposeVerifyEmail})
	if err == nil {
		if wait := codeResendCooldown - time.Since(c.CreatedAt); wait > 0 {
			return httpx.NewError(http.StatusTooManyRequests,
				fmt.Sprintf("повторно отправить код можно через %d с", int(wait.Seconds())+1))
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("auth: get code: %w", err)
	}
	return s.sendVerifyCode(ctx, u)
}

type VerifyInput struct {
	Code string `json:"code" binding:"required,len=6,numeric"`
}

// VerifyEmail проверяет код и возвращает пользователя с новым токеном.
func (s *Service) VerifyEmail(ctx context.Context, id Identity, in VerifyInput) (User, string, error) {
	u, err := s.Me(ctx, id)
	if err != nil {
		return User{}, "", err
	}
	if u.EmailVerified {
		return User{}, "", ErrAlreadyVerified
	}

	c, err := s.q.GetLatestEmailCode(ctx, store.GetLatestEmailCodeParams{UserID: u.ID, Purpose: purposeVerifyEmail})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", ErrCodeExpired
	}
	if err != nil {
		return User{}, "", fmt.Errorf("auth: get code: %w", err)
	}
	if time.Now().After(c.ExpiresAt) {
		return User{}, "", ErrCodeExpired
	}
	// Попытка списывается до сравнения и атомарно, поэтому параллельные
	// запросы не дают перебрать больше codeMaxAttempts вариантов.
	if _, err := s.q.ConsumeEmailCodeAttempt(ctx, store.ConsumeEmailCodeAttemptParams{ID: c.ID, MaxAttempts: codeMaxAttempts}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, "", ErrCodeAttempts
		}
		return User{}, "", fmt.Errorf("auth: consume attempt: %w", err)
	}
	if !s.codes.equal(in.Code, c.CodeHash) {
		return User{}, "", ErrCodeInvalid
	}

	var su store.User
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		var err error
		if su, err = q.MarkEmailVerified(ctx, u.ID); err != nil {
			return err
		}
		return q.DeleteEmailCodes(ctx, store.DeleteEmailCodesParams{UserID: u.ID, Purpose: purposeVerifyEmail})
	})
	if err != nil {
		return User{}, "", fmt.Errorf("auth: mark verified: %w", err)
	}

	user := fromStore(su)
	tok, err := s.tokens.issue(user.identity())
	return user, tok, err
}

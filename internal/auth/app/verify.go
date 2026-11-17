package app

import (
	"context"
	"errors"

	"closerai/internal/auth/domain"
	"closerai/internal/kernel"
)

// sendVerifyCode заменяет все прежние коды пользователя новым и отправляет его.
func (s *Service) sendVerifyCode(ctx context.Context, u domain.User) error {
	code, err := s.CodeGen.Generate()
	if err != nil {
		return err
	}
	err = s.Tx.Do(ctx, func(ctx context.Context) error {
		if err := s.Codes.DeleteAll(ctx, u.ID, domain.PurposeVerifyEmail); err != nil {
			return err
		}
		_, err := s.Codes.Create(ctx, domain.NewEmailCode(u.ID, domain.PurposeVerifyEmail, s.CodeGen.Hash(code), s.now()))
		return err
	})
	if err != nil {
		return err
	}
	return s.Notifier.SendVerifyCode(ctx, u, code)
}

// ensureVerifyCode отправляет код, только если действующего ещё нет.
func (s *Service) ensureVerifyCode(ctx context.Context, u domain.User) error {
	c, err := s.Codes.Latest(ctx, u.ID, domain.PurposeVerifyEmail)
	if err == nil && c.Usable(s.now()) {
		return nil
	}
	if err != nil && !errors.Is(err, domain.ErrCodeNotFound) {
		return err
	}
	return s.sendVerifyCode(ctx, u)
}

// ResendVerifyCode — повторная отправка по кнопке, не чаще раза в CodeResendCooldown.
func (s *Service) ResendVerifyCode(ctx context.Context, id kernel.Identity) error {
	u, err := s.Me(ctx, id)
	if err != nil {
		return err
	}
	if u.EmailVerified() {
		return domain.ErrAlreadyVerified
	}
	c, err := s.Codes.Latest(ctx, u.ID, domain.PurposeVerifyEmail)
	switch {
	case err == nil:
		if wait := c.ResendWait(s.now()); wait > 0 {
			return &domain.ResendTooEarlyError{Wait: wait}
		}
	case !errors.Is(err, domain.ErrCodeNotFound):
		return err
	}
	return s.sendVerifyCode(ctx, u)
}

// VerifyEmail проверяет код и возвращает пользователя с новым токеном.
func (s *Service) VerifyEmail(ctx context.Context, id kernel.Identity, code string) (domain.User, string, error) {
	u, err := s.Me(ctx, id)
	if err != nil {
		return domain.User{}, "", err
	}
	if u.EmailVerified() {
		return domain.User{}, "", domain.ErrAlreadyVerified
	}

	c, err := s.Codes.Latest(ctx, u.ID, domain.PurposeVerifyEmail)
	if errors.Is(err, domain.ErrCodeNotFound) {
		return domain.User{}, "", domain.ErrCodeExpired
	}
	if err != nil {
		return domain.User{}, "", err
	}
	if c.Expired(s.now()) {
		return domain.User{}, "", domain.ErrCodeExpired
	}
	// Попытка списывается до сравнения и атомарно, поэтому параллельные
	// запросы не дают перебрать больше CodeMaxAttempts вариантов.
	if err := s.Codes.ConsumeAttempt(ctx, c.ID, domain.CodeMaxAttempts); err != nil {
		return domain.User{}, "", err
	}
	if !s.CodeGen.Equal(code, c.CodeHash) {
		return domain.User{}, "", domain.ErrCodeInvalid
	}

	err = s.Tx.Do(ctx, func(ctx context.Context) error {
		var err error
		if u, err = s.Users.MarkEmailVerified(ctx, u.ID); err != nil {
			return err
		}
		return s.Codes.DeleteAll(ctx, u.ID, domain.PurposeVerifyEmail)
	})
	if err != nil {
		return domain.User{}, "", err
	}

	tok, err := s.Tokens.Issue(u.Identity())
	return u, tok, err
}

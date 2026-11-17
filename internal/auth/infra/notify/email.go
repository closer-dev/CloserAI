// Package notify — доставка кодов подтверждения пользователю по почте.
package notify

import (
	"context"
	"fmt"
	"time"

	"closerai/internal/auth/domain"
	"closerai/internal/platform/mailer"
)

type Email struct {
	mail mailer.Sender
}

func NewEmail(m mailer.Sender) *Email { return &Email{mail: m} }

func (e *Email) SendVerifyCode(ctx context.Context, u domain.User, code string) error {
	return e.mail.Send(ctx, mailer.Message{
		To:      u.Email,
		Subject: "Код подтверждения CloserAI: " + code,
		Text: fmt.Sprintf("Здравствуйте, %s!\n\nВаш код подтверждения: %s\n\n"+
			"Код действует %d минут. Если вы не регистрировались в CloserAI, просто проигнорируйте это письмо.\n",
			u.Name, code, int(domain.CodeTTL/time.Minute)),
	})
}

// Package notify — доставка кодов подтверждения пользователю по почте.
package notify

import (
	"context"
	"embed"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io/fs"
	"strings"
	texttemplate "text/template"
	"time"

	"closerai/internal/auth/domain"
	"closerai/internal/platform/mailer"
)

const (
	verifyText = "templates/verify_email.txt"
	verifyHTML = "templates/verify_email.html"
)

// Дизайн письма можно добавить как verify_email.html. До этого используем текст.
//
//go:embed templates/verify_email.*
var templates embed.FS

type verifyData struct {
	Name       string
	Code       string
	TTLMinutes int
}

type Email struct {
	mail mailer.Sender
	text *texttemplate.Template
	html *htmltemplate.Template
}

func NewEmail(m mailer.Sender) (*Email, error) { return newEmail(m, templates) }

func newEmail(m mailer.Sender, files fs.FS) (*Email, error) {
	text, err := texttemplate.ParseFS(files, verifyText)
	if err != nil {
		return nil, fmt.Errorf("notify: parse %s: %w", verifyText, err)
	}
	e := &Email{mail: m, text: text}
	markup, err := fs.ReadFile(files, verifyHTML)
	if errors.Is(err, fs.ErrNotExist) {
		return e, nil
	}
	if err != nil {
		return nil, fmt.Errorf("notify: read %s: %w", verifyHTML, err)
	}
	e.html, err = htmltemplate.New(verifyHTML).Parse(string(markup))
	if err != nil {
		return nil, fmt.Errorf("notify: parse %s: %w", verifyHTML, err)
	}
	return e, nil
}

func (e *Email) SendVerifyCode(ctx context.Context, u domain.User, code string) error {
	data := verifyData{Name: u.Name, Code: code, TTLMinutes: int(domain.CodeTTL / time.Minute)}
	var text, html strings.Builder
	if err := e.text.Execute(&text, data); err != nil {
		return fmt.Errorf("notify: render %s: %w", verifyText, err)
	}
	if e.html != nil {
		if err := e.html.Execute(&html, data); err != nil {
			return fmt.Errorf("notify: render %s: %w", verifyHTML, err)
		}
	}
	return e.mail.Send(ctx, mailer.Message{
		To:      u.Email,
		Subject: "Код подтверждения CloserAI",
		Text:    text.String(),
		HTML:    html.String(),
	})
}

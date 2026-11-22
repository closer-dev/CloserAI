package main

// Адаптеры между ограниченными контекстами. Каждый контекст объявляет свои
// порты в своих терминах, а здесь они связываются с публичными сервисами
// других контекстов: ни один контекст не импортирует пакеты другого.

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	botapp "closerai/internal/bot/app"
	botdomain "closerai/internal/bot/domain"
	convapp "closerai/internal/conversation/app"
	convdomain "closerai/internal/conversation/domain"
	tgapp "closerai/internal/telegram/app"
	tgdomain "closerai/internal/telegram/domain"
)

// ---------- telegram → bot: токены ботов ----------

type botDirectory struct{ bots *botapp.Service }

var _ tgapp.BotDirectory = botDirectory{}

func (d botDirectory) Credentials(ctx context.Context, id uuid.UUID) (tgdomain.Credentials, error) {
	c, err := d.bots.Credentials(ctx, id)
	if errors.Is(err, botdomain.ErrNotFound) {
		return tgdomain.Credentials{}, tgdomain.ErrBotNotFound
	}
	if err != nil {
		return tgdomain.Credentials{}, err
	}
	return tgCredentials(c), nil
}

func (d botDirectory) Active(ctx context.Context) ([]tgdomain.Credentials, error) {
	list, err := d.bots.ActiveCredentials(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]tgdomain.Credentials, len(list))
	for i, c := range list {
		out[i] = tgCredentials(c)
	}
	return out, nil
}

func tgCredentials(c botdomain.Credentials) tgdomain.Credentials {
	return tgdomain.Credentials{
		BotID: c.BotID, TenantID: c.TenantID, Token: c.Token,
		WebhookSecret: c.WebhookSecret, IsActive: c.IsActive,
	}
}

// ---------- telegram → conversation: входящие сообщения ----------

// inbox ссылается на сервис диалогов через указатель: telegram и
// conversation зависят друг от друга, поэтому одного из них ещё нет,
// когда собирается другой.
type inbox struct{ conv **convapp.Service }

var _ tgapp.Inbox = inbox{}

func (i inbox) Deliver(ctx context.Context, m tgdomain.Inbound) error {
	in := convapp.Incoming{
		TenantID:    m.TenantID,
		BotID:       m.BotID,
		Text:        m.Message.Text,
		TgMessageID: m.Message.ID,
		SentAt:      m.Message.Date,
	}
	if f := m.Message.From; f != nil {
		in.Customer = convapp.CustomerProfile{
			TgUserID: f.ID, Username: f.Username, FirstName: f.FirstName, LastName: f.LastName,
		}
	}
	return (*i.conv).HandleIncoming(ctx, in)
}

// ---------- conversation → telegram: отправка ----------

type sender struct{ tg *tgapp.Service }

var _ convapp.Sender = sender{}

func (s sender) Typing(ctx context.Context, botID uuid.UUID, chatID int64) {
	s.tg.Typing(ctx, botID, chatID)
}

func (s sender) Send(ctx context.Context, botID uuid.UUID, chatID int64, text string) (int64, error) {
	id, err := s.tg.Send(ctx, botID, chatID, text)
	if errors.Is(err, tgdomain.ErrChatUnavailable) || errors.Is(err, tgdomain.ErrBotUnavailable) {
		return 0, fmt.Errorf("%w: %w", convdomain.ErrChatUnavailable, err)
	}
	return id, err
}

// ---------- conversation → bot: настройки бота ----------

type botProfile struct{ bots *botapp.Service }

var _ convapp.BotProfile = botProfile{}

func (p botProfile) Greeting(ctx context.Context, tenantID, botID uuid.UUID) (string, error) {
	b, err := p.bots.Get(ctx, tenantID, botID)
	if err != nil {
		return "", err
	}
	return b.Settings.Greeting, nil
}

// ---------- conversation → rag: ответ бота ----------

// echoAnswers — временный AnswerProvider до этапа 4 (RAG): повторяет вопрос.
type echoAnswers struct{}

var _ convapp.AnswerProvider = echoAnswers{}

func (echoAnswers) Answer(_ context.Context, r convapp.AnswerRequest) (string, error) {
	return "Вы написали: " + r.Question, nil
}

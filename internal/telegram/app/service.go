// Package app — прикладной слой контекста telegram: приём апдейтов (вебхук
// и polling) и отправка сообщений клиентам от имени ботов компаний.
package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"closerai/internal/telegram/domain"
)

// ---------- порты ----------

// BotDirectory — откуда брать токены ботов (контекст bot).
type BotDirectory interface {
	// Credentials: domain.ErrBotNotFound, если бота нет.
	Credentials(ctx context.Context, botID uuid.UUID) (domain.Credentials, error)
	Active(ctx context.Context) ([]domain.Credentials, error)
}

// Inbox принимает сообщения клиентов (контекст conversation).
type Inbox interface {
	Deliver(ctx context.Context, m domain.Inbound) error
}

// API — вызовы Bot API от имени бота.
type API interface {
	// SendMessage: domain.ErrChatUnavailable, domain.ErrBotUnavailable (токен
	// отозван), domain.ErrTelegramUnavailable (можно повторить).
	SendMessage(ctx context.Context, token string, chatID int64, text string) (messageID int64, err error)
	SendTyping(ctx context.Context, token string, chatID int64) error
}

type Deps struct {
	Bots  BotDirectory
	Inbox Inbox
	API   API
	Log   *slog.Logger
}

type Service struct {
	bots  BotDirectory
	inbox Inbox
	api   API
	log   *slog.Logger
}

func NewService(d Deps) (*Service, error) {
	if d.Bots == nil || d.Inbox == nil || d.API == nil {
		return nil, errors.New("telegram: не все зависимости переданы")
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Service{bots: d.Bots, inbox: d.Inbox, api: d.API, log: d.Log}, nil
}

// ---------- приём ----------

// HandleWebhook обрабатывает апдейт, пришедший на вебхук бота botID.
// domain.ErrBotNotFound — бота нет, domain.ErrBadSecret — запрос не от Telegram.
// Апдейты выключенного бота молча отбрасываются.
func (s *Service) HandleWebhook(ctx context.Context, botID uuid.UUID, secret string, u domain.Update) error {
	creds, err := s.bots.Credentials(ctx, botID)
	if err != nil {
		return err
	}
	if !creds.SecretMatches(secret) {
		return domain.ErrBadSecret
	}
	if !creds.IsActive {
		return nil
	}
	return s.HandleUpdate(ctx, creds, u)
}

// HandleUpdate передаёт сообщение клиента в диалоги; остальные апдейты
// пропускает. Ошибка значит, что апдейт стоит получить повторно.
func (s *Service) HandleUpdate(ctx context.Context, bot domain.Credentials, u domain.Update) error {
	msg, ok := u.CustomerMessage()
	if !ok {
		return nil
	}
	return s.inbox.Deliver(ctx, domain.Inbound{TenantID: bot.TenantID, BotID: bot.BotID, Message: msg})
}

// Credentials нужен приёмнику апдейтов в режиме polling.
func (s *Service) Credentials(ctx context.Context, botID uuid.UUID) (domain.Credentials, error) {
	return s.bots.Credentials(ctx, botID)
}

// ActiveBots — все включённые боты (запуск polling при старте).
func (s *Service) ActiveBots(ctx context.Context) ([]domain.Credentials, error) {
	return s.bots.Active(ctx)
}

// ---------- отправка ----------

// Send отправляет текст в чат от имени бота и возвращает id сообщения.
// domain.ErrBotUnavailable — бот удалён или выключен.
func (s *Service) Send(ctx context.Context, botID uuid.UUID, chatID int64, text string) (int64, error) {
	creds, err := s.activeBot(ctx, botID)
	if err != nil {
		return 0, err
	}
	return s.api.SendMessage(ctx, creds.Token, chatID, text)
}

// Typing показывает клиенту «печатает…» (по возможности, ошибки только в лог).
func (s *Service) Typing(ctx context.Context, botID uuid.UUID, chatID int64) {
	creds, err := s.activeBot(ctx, botID)
	if err == nil {
		err = s.api.SendTyping(ctx, creds.Token, chatID)
	}
	if err != nil {
		s.log.DebugContext(ctx, "telegram: не удалось отправить typing", "bot_id", botID, "err", err)
	}
}

func (s *Service) activeBot(ctx context.Context, botID uuid.UUID) (domain.Credentials, error) {
	creds, err := s.bots.Credentials(ctx, botID)
	if errors.Is(err, domain.ErrBotNotFound) {
		return domain.Credentials{}, domain.ErrBotUnavailable
	}
	if err != nil {
		return domain.Credentials{}, err
	}
	if !creds.IsActive {
		return domain.Credentials{}, domain.ErrBotUnavailable
	}
	return creds, nil
}

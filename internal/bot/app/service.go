// Package app — прикладной слой контекста bot: подключение, настройка,
// включение/выключение и удаление ботов. Публичный API контекста.
package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"closerai/internal/bot/domain"
	"closerai/internal/kernel"
)

// TxManager — unit of work (реализация: platform/db.TxManager).
type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Events публикует доменные события (реализация: platform/eventbus.Bus).
type Events interface {
	Publish(ctx context.Context, events ...kernel.Event)
}

// TelegramAPI — проверка токена в Telegram.
type TelegramAPI interface {
	// GetMe возвращает данные бота. domain.ErrTokenRejected — токен недействителен,
	// domain.ErrTelegramUnavailable — Telegram не ответил.
	GetMe(ctx context.Context, token string) (domain.TelegramIdentity, error)
}

// Webhooks направляет апдейты бота к нам. В режиме webhook — setWebhook и
// deleteWebhook, в режиме polling ничего не делает (апдейты забирает telegram).
type Webhooks interface {
	// Register: *domain.WebhookError — Telegram отказал, domain.ErrTelegramUnavailable — не ответил.
	Register(ctx context.Context, b domain.Bot, token string) error
	// Unregister вызывается по возможности: ошибку адаптер логирует сам.
	Unregister(ctx context.Context, token string)
}

type Deps struct {
	Repo     domain.Repository
	Tx       TxManager
	Telegram TelegramAPI
	Webhooks Webhooks
	Events   Events
}

type Service struct {
	repo     domain.Repository
	tx       TxManager
	tg       TelegramAPI
	webhooks Webhooks
	events   Events
}

func NewService(d Deps) (*Service, error) {
	if d.Repo == nil || d.Tx == nil || d.Telegram == nil || d.Webhooks == nil || d.Events == nil {
		return nil, errors.New("bot: не все зависимости переданы")
	}
	return &Service{repo: d.Repo, tx: d.Tx, tg: d.Telegram, webhooks: d.Webhooks, events: d.Events}, nil
}

// ---------- сценарии для панели (в рамках компании) ----------

func (s *Service) List(ctx context.Context, tenantID uuid.UUID) ([]domain.Bot, error) {
	return s.repo.List(ctx, tenantID)
}

func (s *Service) Get(ctx context.Context, tenantID, id uuid.UUID) (domain.Bot, error) {
	return s.repo.Get(ctx, tenantID, id)
}

type ConnectInput struct {
	Token    string
	Settings domain.SettingsPatch
}

// Connect подключает бота по токену от @BotFather: проверяет токен в Telegram,
// сохраняет его зашифрованным и направляет апдейты к нам. Если вебхук
// поставить не удалось, бот не сохраняется.
func (s *Service) Connect(ctx context.Context, tenantID uuid.UUID, in ConnectInput) (domain.Bot, error) {
	token, err := domain.NormalizeToken(in.Token)
	if err != nil {
		return domain.Bot{}, err
	}
	settings, err := domain.DefaultSettings().Apply(in.Settings)
	if err != nil {
		return domain.Bot{}, err
	}
	me, err := s.tg.GetMe(ctx, token)
	if err != nil {
		return domain.Bot{}, err
	}
	secret, err := domain.NewWebhookSecret()
	if err != nil {
		return domain.Bot{}, err
	}

	var saved domain.Bot
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		// Сначала вставка: уникальность tg_bot_id проверяется до обращения к
		// Telegram, поэтому чужой вебхук перезаписать нельзя.
		saved, err = s.repo.Create(ctx, domain.Bot{
			TenantID:      tenantID,
			TgBotID:       me.ID,
			Username:      me.Username,
			WebhookSecret: secret,
			Settings:      settings,
			IsActive:      true,
		}, token)
		if err != nil {
			return err
		}
		if err := s.webhooks.Register(ctx, saved, token); err != nil {
			return err
		}
		s.events.Publish(ctx, kernel.BotActivated{TenantID: tenantID, BotID: saved.ID})
		return nil
	})
	return saved, err
}

type UpdateInput struct {
	Token    *string // новый токен того же бота (после /revoke в @BotFather)
	Settings domain.SettingsPatch
	IsActive *bool
}

// Update меняет настройки, токен и/или включает-выключает бота.
func (s *Service) Update(ctx context.Context, tenantID, id uuid.UUID, in UpdateInput) (domain.Bot, error) {
	var (
		newToken string
		me       domain.TelegramIdentity
	)
	if in.Token != nil {
		t, err := domain.NormalizeToken(*in.Token)
		if err != nil {
			return domain.Bot{}, err
		}
		// Сетевой вызов — до транзакции, чтобы не держать блокировку строки.
		if me, err = s.tg.GetMe(ctx, t); err != nil {
			return domain.Bot{}, err
		}
		newToken = t
	}

	var (
		updated     domain.Bot
		deactivated bool
		oldToken    string
	)
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		b, err := s.repo.GetForUpdate(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if newToken != "" {
			if me.ID != b.TgBotID {
				return domain.ErrTokenMismatch
			}
			b.Username = me.Username
			if err := s.repo.UpdateToken(ctx, tenantID, id, newToken); err != nil {
				return err
			}
		}
		if b.Settings, err = b.Settings.Apply(in.Settings); err != nil {
			return err
		}
		wasActive := b.IsActive
		if in.IsActive != nil {
			b.IsActive = *in.IsActive
		}
		if updated, err = s.repo.Update(ctx, b); err != nil {
			return err
		}

		switch {
		case b.IsActive && (!wasActive || newToken != ""):
			token := newToken
			if token == "" {
				if token, err = s.token(ctx, id); err != nil {
					return err
				}
			}
			if err := s.webhooks.Register(ctx, updated, token); err != nil {
				return err
			}
			s.events.Publish(ctx, kernel.BotActivated{TenantID: tenantID, BotID: id})
		case !b.IsActive && wasActive:
			if oldToken, err = s.token(ctx, id); err != nil {
				return err
			}
			deactivated = true
			s.events.Publish(ctx, kernel.BotDeactivated{TenantID: tenantID, BotID: id})
		}
		return nil
	})
	if err != nil {
		return domain.Bot{}, err
	}
	if deactivated {
		// После коммита и по возможности: выключенного бота telegram всё равно
		// игнорирует, вебхук снимается, чтобы Telegram не слал апдейты зря.
		s.webhooks.Unregister(ctx, oldToken)
	}
	return updated, nil
}

// Delete удаляет бота. Диалоги и клиенты этого бота удаляются каскадом.
func (s *Service) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	var token string
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		b, err := s.repo.GetForUpdate(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if token, err = s.token(ctx, id); err != nil {
			return err
		}
		if err := s.repo.Delete(ctx, tenantID, id); err != nil {
			return err
		}
		if b.IsActive {
			s.events.Publish(ctx, kernel.BotDeactivated{TenantID: tenantID, BotID: id})
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.webhooks.Unregister(ctx, token)
	return nil
}

func (s *Service) token(ctx context.Context, id uuid.UUID) (string, error) {
	c, err := s.repo.Credentials(ctx, id)
	if err != nil {
		return "", fmt.Errorf("bot: credentials: %w", err)
	}
	return c.Token, nil
}

// ---------- системные сценарии (для контекста telegram) ----------

// Credentials — токен и секрет вебхука бота по его id (без проверки компании:
// вызывающий узнаёт бота из URL вебхука и сам проверяет секрет).
func (s *Service) Credentials(ctx context.Context, id uuid.UUID) (domain.Credentials, error) {
	return s.repo.Credentials(ctx, id)
}

// ActiveCredentials — все включённые боты (для запуска polling при старте).
func (s *Service) ActiveCredentials(ctx context.Context) ([]domain.Credentials, error) {
	return s.repo.ActiveCredentials(ctx)
}

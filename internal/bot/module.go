// Package bot — ограниченный контекст «Боты»: подключение Telegram-бота
// компании по токену, его настройки (промпт, приветствие, порог передачи
// оператору), включение/выключение и вебхук.
//
// Слои: domain (модель и порты) → app (сценарии) → infra (БД с шифрованием
// токена, Telegram API) и api (HTTP). Этот файл — composition root контекста.
package bot

import (
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"closerai/internal/bot/api"
	"closerai/internal/bot/app"
	"closerai/internal/bot/infra/postgres"
	"closerai/internal/bot/infra/telegram"
	"closerai/internal/platform/crypto"
)

type Deps struct {
	Pool   *pgxpool.Pool
	Tx     app.TxManager
	Events app.Events
	Cipher *crypto.Cipher // шифрует токены ботов (ENCRYPTION_KEY)
	Log    *slog.Logger

	Mode      string // TG_MODE: webhook | polling
	PublicURL string // PUBLIC_URL, база адреса вебхука
	// TelegramURL — адрес Bot API; пусто — https://api.telegram.org (подменяется в тестах).
	TelegramURL string
}

type Module struct {
	// Service — публичный API контекста; Credentials/ActiveCredentials нужны telegram.
	Service *app.Service
	*api.Handler
}

func New(d Deps) (*Module, error) {
	if d.Cipher == nil {
		return nil, errors.New("bot: не передан Cipher")
	}
	client := telegram.NewClient(d.TelegramURL)

	var webhooks app.Webhooks = telegram.NoWebhooks{}
	if d.Mode == "webhook" {
		webhooks = telegram.NewWebhookRegistrar(client, d.PublicURL, d.Log)
	}

	svc, err := app.NewService(app.Deps{
		Repo:     postgres.NewRepository(d.Pool, d.Cipher),
		Tx:       d.Tx,
		Telegram: client,
		Webhooks: webhooks,
		Events:   d.Events,
	})
	if err != nil {
		return nil, err
	}
	return &Module{Service: svc, Handler: api.NewHandler(svc)}, nil
}

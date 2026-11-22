// Package telegram — ограниченный контекст «Telegram»: приём апдейтов ботов
// компаний (вебхук или long polling) и отправка сообщений клиентам.
//
// Слои: domain (апдейты и правила) → app (сценарии) → infra (Bot API,
// polling) и api (вебхук). Этот файл — composition root контекста. Токены
// ботов (BotDirectory) и приём сообщений (Inbox) приходят из main.go
// адаптерами к контекстам bot и conversation.
package telegram

import (
	"context"
	"log/slog"

	"closerai/internal/kernel"
	"closerai/internal/platform/eventbus"
	"closerai/internal/telegram/api"
	"closerai/internal/telegram/app"
	"closerai/internal/telegram/infra/polling"
	"closerai/internal/telegram/infra/tgapi"
)

type Deps struct {
	Bots   app.BotDirectory
	Inbox  app.Inbox
	Events *eventbus.Bus
	Log    *slog.Logger

	Mode string // TG_MODE: webhook | polling
	// APIURL — адрес Bot API; пусто — https://api.telegram.org.
	APIURL string
}

type Module struct {
	// Service — публичный API контекста; Send/Typing нужны conversation.
	Service *app.Service
	*api.Handler

	poller *polling.Manager // nil в режиме webhook
}

func New(d Deps) (*Module, error) {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	client := tgapi.NewClient(d.APIURL)
	svc, err := app.NewService(app.Deps{Bots: d.Bots, Inbox: d.Inbox, API: client, Log: d.Log})
	if err != nil {
		return nil, err
	}
	m := &Module{Service: svc, Handler: api.NewHandler(svc)}

	if d.Mode == "polling" {
		m.poller = polling.NewManager(svc, client, d.Log)
		eventbus.Subscribe(d.Events, func(ctx context.Context, e kernel.BotActivated) error {
			return m.poller.Activate(ctx, e.BotID)
		})
		eventbus.Subscribe(d.Events, func(_ context.Context, e kernel.BotDeactivated) error {
			m.poller.Deactivate(e.BotID)
			return nil
		})
	}
	return m, nil
}

// Start запускает приём апдейтов (в режиме polling; вебхуки принимает HTTP-сервер).
func (m *Module) Start(ctx context.Context) error {
	if m.poller == nil {
		return nil
	}
	return m.poller.Start(ctx)
}

// Stop останавливает приём апдейтов и ждёт завершения обработки.
func (m *Module) Stop() {
	if m.poller != nil {
		m.poller.Stop()
	}
}

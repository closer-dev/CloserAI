// Package conversation — ограниченный контекст «Диалоги»: клиенты бота,
// диалоги и сообщения, статусы bot / waiting_operator / operator / closed,
// ответы бота клиенту.
//
// Слои: domain (модель и порты) → app (сценарии) → infra (БД, задачи river).
// Этот файл — composition root контекста. Ответ бота (AnswerProvider),
// отправка в Telegram (Sender) и настройки бота (BotProfile) приходят
// из main.go адаптерами к другим контекстам.
package conversation

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"closerai/internal/conversation/app"
	convjobs "closerai/internal/conversation/infra/jobs"
	"closerai/internal/conversation/infra/postgres"
	"closerai/internal/platform/jobs"
)

type Deps struct {
	Pool    *pgxpool.Pool
	Tx      app.TxManager
	Events  app.Events
	Jobs    *jobs.Queue
	Answers app.AnswerProvider
	Bots    app.BotProfile
	Sender  app.Sender
}

type Module struct {
	// Service — публичный API контекста; HandleIncoming вызывает telegram.
	Service *app.Service
}

func New(d Deps) (*Module, error) {
	svc, err := app.NewService(app.Deps{
		Repo:    postgres.NewRepository(d.Pool),
		Tx:      d.Tx,
		Events:  d.Events,
		Replies: convjobs.ReplyScheduler{Queue: d.Jobs},
		Answers: d.Answers,
		Bots:    d.Bots,
		Sender:  d.Sender,
	})
	if err != nil {
		return nil, err
	}
	jobs.Register(d.Jobs, &convjobs.ReplyWorker{Service: svc})
	return &Module{Service: svc}, nil
}

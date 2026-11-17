// Package tenant — ограниченный контекст «Компании-клиенты»: арендаторы и тарифы.
//
// Слои: domain (модель и порты) → app (сценарии) → infra (БД) и api (HTTP).
// Этот файл — composition root контекста: собирает слои и отдаёт наружу
// только то, что нужно main и другим контекстам.
package tenant

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"closerai/internal/tenant/api"
	"closerai/internal/tenant/app"
	"closerai/internal/tenant/infra/postgres"
)

type Module struct {
	// Service — публичный API контекста для других контекстов.
	Service *app.Service
	*api.Handler
}

func New(pool *pgxpool.Pool) *Module {
	svc := app.NewService(postgres.NewRepository(pool))
	return &Module{Service: svc, Handler: api.NewHandler(svc)}
}

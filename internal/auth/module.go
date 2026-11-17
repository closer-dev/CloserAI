// Package auth — ограниченный контекст «Доступ»: регистрация компании, вход,
// JWT в httpOnly cookie, роли, подтверждение email, сотрудники компании.
//
// Слои: domain (модель и порты) → app (сценарии) → infra (БД, JWT, почта)
// и api (HTTP). Этот файл — composition root контекста.
package auth

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"closerai/internal/auth/api"
	"closerai/internal/auth/app"
	"closerai/internal/auth/infra/notify"
	"closerai/internal/auth/infra/postgres"
	"closerai/internal/auth/infra/security"
	"closerai/internal/platform/mailer"
)

type Deps struct {
	Pool         *pgxpool.Pool
	Tx           app.TxManager
	Tenants      app.TenantCreator
	Mailer       mailer.Sender
	JWTSecret    string
	SecureCookie bool
}

type Module struct {
	Service *app.Service
	// Handler даёт Routes, Middleware; FromContext и RequireRole — в пакете api.
	*api.Handler
}

func New(d Deps) (*Module, error) {
	svc, err := app.NewService(app.Deps{
		Tx:       d.Tx,
		Users:    postgres.NewUserRepository(d.Pool),
		Codes:    postgres.NewEmailCodeRepository(d.Pool),
		Tenants:  d.Tenants,
		Hasher:   security.Bcrypt{},
		Tokens:   security.NewJWT(d.JWTSecret),
		CodeGen:  security.NewHMACCodes(d.JWTSecret),
		Notifier: notify.NewEmail(d.Mailer),
	})
	if err != nil {
		return nil, err
	}
	h := api.NewHandler(svc, api.Config{SecureCookie: d.SecureCookie, SessionTTL: security.TokenTTL})
	return &Module{Service: svc, Handler: h}, nil
}

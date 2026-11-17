package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"closerai/internal/auth"
	authapp "closerai/internal/auth/app"
	"closerai/internal/platform/config"
	"closerai/internal/platform/db"
	"closerai/internal/platform/httpx"
	"closerai/internal/platform/logger"
	"closerai/internal/platform/mailer"
	"closerai/internal/tenant"
	"closerai/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("app stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(cfg.IsDev())
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}

	mail, err := newMailer(cfg)
	if err != nil {
		return err
	}

	router, err := newRouter(cfg, pool, mail)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http server started", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		return err
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func newRouter(cfg config.Config, pool *pgxpool.Pool, mail mailer.Sender) (*gin.Engine, error) {
	if !cfg.IsDev() {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		if err := pool.Ping(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "db unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Сборка ограниченных контекстов. Контексты общаются только через
	// публичные сервисы (app) и порты; адаптеры между ними собираются здесь.
	tx := db.NewTxManager(pool)

	tenantMod := tenant.New(pool)
	authMod, err := auth.New(auth.Deps{
		Pool: pool,
		Tx:   tx,
		// auth создаёт компанию в своей транзакции регистрации (TxManager)
		Tenants: authapp.TenantCreatorFunc(func(ctx context.Context, name string) (uuid.UUID, error) {
			t, err := tenantMod.Service.Create(ctx, name)
			return t.ID, err
		}),
		Mailer:       mail,
		JWTSecret:    cfg.JWTSecret,
		SecureCookie: strings.HasPrefix(cfg.PublicURL, "https://"),
	})
	if err != nil {
		return nil, err
	}

	api := r.Group("/api")
	authMod.Routes(api) // /auth/* публичные, остальное auth-модуль защищает сам

	// Всё, что ниже, доступно только авторизованным; tenant_id берётся из токена.
	private := api.Group("", authMod.Middleware())
	tenantMod.Routes(private)

	r.NoRoute(httpx.SPA(web.Dist()))
	return r, nil
}

func newMailer(cfg config.Config) (mailer.Sender, error) {
	if cfg.SMTPHost == "" {
		if !cfg.IsDev() {
			slog.Warn("SMTP_HOST не задан: письма с кодами не отправляются, а пишутся в лог")
		}
		return mailer.LogSender{}, nil
	}
	return mailer.NewSMTP(mailer.SMTPConfig{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort, User: cfg.SMTPUser,
		Password: cfg.SMTPPassword, From: cfg.SMTPFrom,
	})
}

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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"closerai/internal/auth"
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

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           newRouter(cfg, pool, mail),
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

func newRouter(cfg config.Config, pool *pgxpool.Pool, mail mailer.Sender) *gin.Engine {
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

	// Сборка модулей. Модули общаются через сервисы, а не через чужие таблицы.
	tenantSvc := tenant.NewService(pool)
	authSvc := auth.NewService(pool, func(ctx context.Context, tx pgx.Tx, name string) (uuid.UUID, error) {
		t, err := tenantSvc.CreateTx(ctx, tx, name)
		return t.ID, err
	}, cfg.JWTSecret, mail)
	authMod := auth.NewModule(authSvc, strings.HasPrefix(cfg.PublicURL, "https://"))

	api := r.Group("/api")
	authMod.Routes(api) // /auth/* публичные, остальное auth-модуль защищает сам

	// Всё, что ниже, доступно только авторизованным; tenant_id берётся из токена.
	private := api.Group("", authMod.Middleware())
	tenant.NewModule(tenantSvc).Routes(private)

	r.NoRoute(httpx.SPA(web.Dist()))
	return r
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

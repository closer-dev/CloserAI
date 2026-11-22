package main

import (
	"context"
	"errors"
	"fmt"
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
	"closerai/internal/bot"
	"closerai/internal/conversation"
	convapp "closerai/internal/conversation/app"
	"closerai/internal/platform/config"
	"closerai/internal/platform/crypto"
	"closerai/internal/platform/db"
	"closerai/internal/platform/eventbus"
	"closerai/internal/platform/httpx"
	"closerai/internal/platform/jobs"
	"closerai/internal/platform/llm"
	"closerai/internal/platform/llm/ollama"
	"closerai/internal/platform/logger"
	"closerai/internal/platform/mailer"
	"closerai/internal/telegram"
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
	if err := jobs.Migrate(ctx, pool); err != nil {
		return err
	}

	p, err := newPlatform(ctx, cfg, log, pool)
	if err != nil {
		return err
	}

	// Модули регистрируют воркеры в p.Jobs внутри newRouter, поэтому очередь
	// запускается после него, но до приёма HTTP-запросов.
	router, mods, err := newRouter(cfg, p)
	if err != nil {
		return err
	}
	if err := p.Jobs.Start(ctx); err != nil {
		return err
	}
	// Приём апдейтов — после очереди: на каждое сообщение ставится задача ответа.
	if err := mods.Telegram.Start(ctx); err != nil {
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
	// Сначала приём (HTTP и polling) — новые задачи больше не ставятся,
	// потом очередь задач.
	httpErr := srv.Shutdown(shutdownCtx)
	mods.Telegram.Stop()
	jobsErr := p.Jobs.Stop(shutdownCtx)
	return errors.Join(httpErr, jobsErr)
}

// platform — общая инфраструктура, которую main раздаёт контекстам.
type platform struct {
	Pool   *pgxpool.Pool
	Tx     *db.TxManager
	Events *eventbus.Bus // доменные события (kernel.Event)
	Jobs   *jobs.Queue   // фоновые задачи (river)
	LLM    llm.Provider  // Ollama; контексты получают его через свои порты в infra
	Mail   mailer.Sender
	Cipher *crypto.Cipher // шифрование секретов (токены ботов)
}

func newPlatform(ctx context.Context, cfg config.Config, log *slog.Logger, pool *pgxpool.Pool) (*platform, error) {
	mail, err := newMailer(cfg)
	if err != nil {
		return nil, err
	}
	cipher, err := crypto.New(cfg.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("ENCRYPTION_KEY: %w", err)
	}
	ai, err := ollama.New(ollama.Config{
		URL: cfg.OllamaURL, ChatModel: cfg.LLMChatModel, EmbedModel: cfg.LLMEmbedModel,
	})
	if err != nil {
		return nil, err
	}
	// Недоступная Ollama не мешает старту: панель и Telegram работают и без
	// модели, а вызовы LLM вернут llm.ErrUnavailable.
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := ai.Ping(pingCtx); err != nil {
		log.Warn("Ollama недоступна, ответы бота и индексация работать не будут", "url", cfg.OllamaURL, "err", err)
	}
	return &platform{
		Pool:   pool,
		Tx:     db.NewTxManager(pool),
		Events: eventbus.New(log),
		Jobs:   jobs.New(pool, log, jobs.Config{MaxWorkers: cfg.JobsMaxWorkers}),
		LLM:    ai,
		Mail:   mail,
		Cipher: cipher,
	}, nil
}

// modules — контексты, которым main управляет после сборки (запуск/остановка).
type modules struct {
	Telegram *telegram.Module
}

func newRouter(cfg config.Config, p *platform) (*gin.Engine, *modules, error) {
	pool := p.Pool
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
	tenantMod := tenant.New(pool)
	authMod, err := auth.New(auth.Deps{
		Pool: pool,
		Tx:   p.Tx,
		// auth создаёт компанию в своей транзакции регистрации (TxManager)
		Tenants: authapp.TenantCreatorFunc(func(ctx context.Context, name string) (uuid.UUID, error) {
			t, err := tenantMod.Service.Create(ctx, name)
			return t.ID, err
		}),
		Mailer:       p.Mail,
		JWTSecret:    cfg.JWTSecret,
		SecureCookie: strings.HasPrefix(cfg.PublicURL, "https://"),
	})
	if err != nil {
		return nil, nil, err
	}

	botMod, err := bot.New(bot.Deps{
		Pool:      pool,
		Tx:        p.Tx,
		Events:    p.Events,
		Cipher:    p.Cipher,
		Log:       slog.Default(),
		Mode:      cfg.TelegramMode,
		PublicURL: cfg.PublicURL,

		TelegramURL: cfg.TelegramAPIURL,
	})
	if err != nil {
		return nil, nil, err
	}

	// telegram и conversation зависят друг от друга (входящие ↔ отправка),
	// поэтому telegram получает ссылку на сервис диалогов, который
	// появится строкой ниже.
	var convSvc *convapp.Service
	tgMod, err := telegram.New(telegram.Deps{
		Bots:   botDirectory{bots: botMod.Service},
		Inbox:  inbox{conv: &convSvc},
		Events: p.Events,
		Log:    slog.Default(),
		Mode:   cfg.TelegramMode,
		APIURL: cfg.TelegramAPIURL,
	})
	if err != nil {
		return nil, nil, err
	}
	convMod, err := conversation.New(conversation.Deps{
		Pool:    pool,
		Tx:      p.Tx,
		Events:  p.Events,
		Jobs:    p.Jobs,
		Answers: echoAnswers{}, // этап 4: rag
		Bots:    botProfile{bots: botMod.Service},
		Sender:  sender{tg: tgMod.Service},
	})
	if err != nil {
		return nil, nil, err
	}
	convSvc = convMod.Service

	tgMod.Routes(r) // /tg/webhook/:botID — публичный, проверка по секрету

	api := r.Group("/api")
	authMod.Routes(api) // /auth/* публичные, остальное auth-модуль защищает сам

	// Всё, что ниже, доступно только авторизованным; tenant_id берётся из токена.
	private := api.Group("", authMod.Middleware())
	tenantMod.Routes(private)
	botMod.Routes(private)

	r.NoRoute(httpx.SPA(web.Dist()))
	return r, &modules{Telegram: tgMod}, nil
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

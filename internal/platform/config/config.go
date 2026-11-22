// Package config загружает настройки приложения из переменных окружения.
package config

import (
	"fmt"
	"strings"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Env       string `env:"APP_ENV" envDefault:"dev"`
	HTTPAddr  string `env:"HTTP_ADDR" envDefault:":8080"`
	PublicURL string `env:"PUBLIC_URL" envDefault:"http://localhost:8080"`

	DatabaseURL string `env:"DATABASE_URL,required"`

	JWTSecret     string `env:"JWT_SECRET,required"`
	EncryptionKey string `env:"ENCRYPTION_KEY,required"` // base64, 32 байта (AES-256)

	// Почта. Если SMTP_HOST пуст, письма не отправляются, а пишутся в лог (для разработки).
	SMTPHost     string `env:"SMTP_HOST"`
	SMTPPort     int    `env:"SMTP_PORT" envDefault:"587"`
	SMTPUser     string `env:"SMTP_USER"`
	SMTPPassword string `env:"SMTP_PASSWORD"`
	SMTPFrom     string `env:"SMTP_FROM" envDefault:"CloserAI <noreply@localhost>"`

	TelegramMode string `env:"TG_MODE" envDefault:"polling"` // webhook (нужен HTTPS) | polling
	// TG_API_URL — свой сервер Bot API; пусто — https://api.telegram.org.
	TelegramAPIURL string `env:"TG_API_URL"`

	OllamaURL     string `env:"OLLAMA_URL" envDefault:"http://localhost:11434"`
	LLMChatModel  string `env:"LLM_CHAT_MODEL" envDefault:"qwen2.5:3b-instruct"`
	LLMEmbedModel string `env:"LLM_EMBED_MODEL" envDefault:"bge-m3"`

	// Фоновые задачи (river): сколько выполняется одновременно.
	JobsMaxWorkers int `env:"JOBS_MAX_WORKERS" envDefault:"4"`
}

func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if cfg.TelegramMode != "webhook" && cfg.TelegramMode != "polling" {
		return Config{}, fmt.Errorf("config: TG_MODE must be webhook or polling, got %q", cfg.TelegramMode)
	}
	// Telegram принимает вебхуки только на HTTPS-адрес.
	if cfg.TelegramMode == "webhook" && !strings.HasPrefix(cfg.PublicURL, "https://") {
		return Config{}, fmt.Errorf("config: TG_MODE=webhook требует PUBLIC_URL с https://, сейчас %q "+
			"(для локальной разработки используйте TG_MODE=polling)", cfg.PublicURL)
	}
	return cfg, nil
}

func (c Config) IsDev() bool { return c.Env == "dev" }

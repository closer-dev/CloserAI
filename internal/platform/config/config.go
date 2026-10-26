// Package config загружает настройки приложения из переменных окружения.
package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Env       string `env:"APP_ENV" envDefault:"dev"`
	HTTPAddr  string `env:"HTTP_ADDR" envDefault:":8080"`
	PublicURL string `env:"PUBLIC_URL" envDefault:"http://localhost:8080"`

	DatabaseURL string `env:"DATABASE_URL,required"`

	JWTSecret     string `env:"JWT_SECRET,required"`
	EncryptionKey string `env:"ENCRYPTION_KEY,required"` // base64, 32 байта (AES-256)

	TelegramMode string `env:"TG_MODE" envDefault:"webhook"` // webhook | polling

	OllamaURL     string `env:"OLLAMA_URL" envDefault:"http://localhost:11434"`
	LLMChatModel  string `env:"LLM_CHAT_MODEL" envDefault:"qwen2.5:3b-instruct"`
	LLMEmbedModel string `env:"LLM_EMBED_MODEL" envDefault:"bge-m3"`
}

func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if cfg.TelegramMode != "webhook" && cfg.TelegramMode != "polling" {
		return Config{}, fmt.Errorf("config: TG_MODE must be webhook or polling, got %q", cfg.TelegramMode)
	}
	return cfg, nil
}

func (c Config) IsDev() bool { return c.Env == "dev" }

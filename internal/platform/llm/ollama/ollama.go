// Package ollama — реализация llm.Provider поверх официального клиента Ollama.
package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"github.com/ollama/ollama/api"

	"closerai/internal/platform/llm"
)

// DefaultTemperature — низкая температура, чтобы небольшая модель меньше
// выдумывала (см. риски в техплане).
const DefaultTemperature = 0.2

type Config struct {
	URL        string // OLLAMA_URL, например http://ollama:11434
	ChatModel  string // LLM_CHAT_MODEL
	EmbedModel string // LLM_EMBED_MODEL
}

type Provider struct {
	client *api.Client
	cfg    Config
}

var _ llm.Provider = (*Provider)(nil)

func New(cfg Config) (*Provider, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("llm: некорректный OLLAMA_URL %q", cfg.URL)
	}
	if cfg.ChatModel == "" || cfg.EmbedModel == "" {
		return nil, errors.New("llm: не заданы LLM_CHAT_MODEL / LLM_EMBED_MODEL")
	}
	// Без общего таймаута: на CPU ответ может идти десятки секунд,
	// длительность ограничивает ctx вызывающего кода.
	return &Provider{client: api.NewClient(u, &http.Client{}), cfg: cfg}, nil
}

func (p *Provider) Chat(ctx context.Context, msgs []llm.Message, opts llm.ChatOptions) (string, error) {
	req := &api.ChatRequest{
		Model:    p.cfg.ChatModel,
		Messages: make([]api.Message, len(msgs)),
		Stream:   new(false),
		Options:  map[string]any{"temperature": DefaultTemperature},
	}
	for i, m := range msgs {
		req.Messages[i] = api.Message{Role: string(m.Role), Content: m.Content}
	}
	if opts.Temperature > 0 {
		req.Options["temperature"] = opts.Temperature
	}
	if opts.MaxTokens > 0 {
		req.Options["num_predict"] = opts.MaxTokens
	}
	if opts.JSONSchema != nil {
		schema, err := json.Marshal(opts.JSONSchema)
		if err != nil {
			return "", fmt.Errorf("llm: json-схема: %w", err)
		}
		req.Format = schema
	}

	var answer string
	err := p.client.Chat(ctx, req, func(r api.ChatResponse) error {
		answer += r.Message.Content
		return nil
	})
	if err != nil {
		return "", wrap("chat", p.cfg.ChatModel, err)
	}
	return answer, nil
}

func (p *Provider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	resp, err := p.client.Embed(ctx, &api.EmbedRequest{Model: p.cfg.EmbedModel, Input: texts})
	if err != nil {
		return nil, wrap("embed", p.cfg.EmbedModel, err)
	}
	if len(resp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("llm: ollama embed: ожидалось %d векторов, получено %d",
			len(texts), len(resp.Embeddings))
	}
	return resp.Embeddings, nil
}

// Ping проверяет, что сервер Ollama отвечает. Наличие моделей не проверяет.
func (p *Provider) Ping(ctx context.Context) error {
	if err := p.client.Heartbeat(ctx); err != nil {
		return wrap("ping", "", err)
	}
	return nil
}

// wrap приводит ошибки клиента к llm.ErrUnavailable, когда дело в недоступности
// сервера или модели, чтобы вызывающий код мог на это отреагировать.
func wrap(op, model string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("llm: ollama %s: %w", op, err)
	}
	var se api.StatusError
	if errors.As(err, &se) {
		if se.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: модель %q не найдена в Ollama (make models): %s",
				llm.ErrUnavailable, model, se.ErrorMessage)
		}
		if se.StatusCode >= 500 {
			return fmt.Errorf("%w: ollama %s: %d %s", llm.ErrUnavailable, op, se.StatusCode, se.ErrorMessage)
		}
		return fmt.Errorf("llm: ollama %s: %w", op, err)
	}
	var ue *url.Error
	var oe *net.OpError
	if errors.As(err, &ue) || errors.As(err, &oe) {
		return fmt.Errorf("%w: ollama %s: %v", llm.ErrUnavailable, op, err)
	}
	return fmt.Errorf("llm: ollama %s: %w", op, err)
}

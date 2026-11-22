// Package llm — порт к языковой модели. Остальной код работает только с
// интерфейсом Provider и не знает, что внутри Ollama (реализация — llm/ollama),
// поэтому модель или провайдера можно заменить, не трогая контексты.
//
// По правилам DDD слои app не импортируют platform: контекст объявляет у себя
// узкий порт (например, knowledge/app.Embedder), а адаптер в его infra
// реализует этот порт через llm.Provider.
package llm

import (
	"context"
	"errors"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role
	Content string
}

type ChatOptions struct {
	// Temperature: 0 — значение провайдера по умолчанию (для Ollama — DefaultTemperature).
	Temperature float32
	// MaxTokens ограничивает длину ответа; 0 — без ограничения.
	MaxTokens int
	// JSONSchema, если задана, заставляет модель вернуть JSON по схеме (нужно
	// для вердиктов). Любое значение, которое json.Marshal превращает в
	// JSON-схему: map[string]any, структура, json.RawMessage.
	JSONSchema any
}

// ErrUnavailable — модель недоступна (сервер не отвечает или модель не скачана).
// Вызывающий код может, например, передать диалог оператору.
var ErrUnavailable = errors.New("llm: модель недоступна")

// Provider — единственная точка доступа к модели.
type Provider interface {
	// Chat возвращает ответ модели целиком (без стриминга).
	Chat(ctx context.Context, msgs []Message, opts ChatOptions) (string, error)
	// Embed возвращает по одному вектору на каждый текст, в том же порядке.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

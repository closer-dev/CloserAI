package llm

import "context"

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
	Temperature float32
	// JSONSchema, если задана, заставляет модель вернуть JSON по схеме (нужно для вердиктов).
	JSONSchema any
}

// Provider — единственная точка доступа к модели. Остальной код не знает,
// что внутри Ollama, поэтому модель или провайдера можно заменить здесь.
type Provider interface {
	Chat(ctx context.Context, msgs []Message, opts ChatOptions) (string, error)
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

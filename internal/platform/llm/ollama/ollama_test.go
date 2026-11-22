package ollama

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"closerai/internal/platform/llm"
)

// fakeOllama запоминает последний запрос и отвечает заготовленным JSON.
func fakeOllama(t *testing.T, status int, reply any) (*Provider, *map[string]any) {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = map[string]any{"path": r.URL.Path}
		if len(body) > 0 {
			require.NoError(t, json.Unmarshal(body, &got))
			got["path"] = r.URL.Path
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(reply)
	}))
	t.Cleanup(srv.Close)
	p, err := New(Config{URL: srv.URL, ChatModel: "chat-m", EmbedModel: "embed-m"})
	require.NoError(t, err)
	return p, &got
}

func TestChat(t *testing.T) {
	p, got := fakeOllama(t, 200, map[string]any{
		"model": "chat-m", "done": true,
		"message": map[string]any{"role": "assistant", "content": `{"resolved":true}`},
	})

	schema := map[string]any{"type": "object"}
	answer, err := p.Chat(context.Background(), []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "привет"},
	}, llm.ChatOptions{Temperature: 0.1, MaxTokens: 100, JSONSchema: schema})
	require.NoError(t, err)
	require.Equal(t, `{"resolved":true}`, answer)

	req := *got
	require.Equal(t, "/api/chat", req["path"])
	require.Equal(t, "chat-m", req["model"])
	require.Equal(t, false, req["stream"])
	require.Equal(t, schema, req["format"])
	require.InDelta(t, 0.1, req["options"].(map[string]any)["temperature"], 1e-6)
	require.EqualValues(t, 100, req["options"].(map[string]any)["num_predict"])
	require.Len(t, req["messages"], 2)
	require.Equal(t, "system", req["messages"].([]any)[0].(map[string]any)["role"])
}

func TestChatDefaults(t *testing.T) {
	p, got := fakeOllama(t, 200, map[string]any{"done": true, "message": map[string]any{"content": "ok"}})
	_, err := p.Chat(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "?"}}, llm.ChatOptions{})
	require.NoError(t, err)

	opts := (*got)["options"].(map[string]any)
	require.InDelta(t, DefaultTemperature, opts["temperature"], 1e-6)
	require.NotContains(t, opts, "num_predict")
	require.NotContains(t, *got, "format")
}

func TestEmbed(t *testing.T) {
	p, got := fakeOllama(t, 200, map[string]any{"embeddings": [][]float32{{1, 2}, {3, 4}}})
	vecs, err := p.Embed(context.Background(), []string{"а", "б"})
	require.NoError(t, err)
	require.Equal(t, [][]float32{{1, 2}, {3, 4}}, vecs)
	require.Equal(t, "/api/embed", (*got)["path"])
	require.Equal(t, "embed-m", (*got)["model"])
	require.Equal(t, []any{"а", "б"}, (*got)["input"])
}

func TestEmbedCountMismatch(t *testing.T) {
	p, _ := fakeOllama(t, 200, map[string]any{"embeddings": [][]float32{{1}}})
	_, err := p.Embed(context.Background(), []string{"а", "б"})
	require.ErrorContains(t, err, "ожидалось 2 векторов")
}

func TestEmbedEmpty(t *testing.T) {
	p, err := New(Config{URL: "http://127.0.0.1:1", ChatModel: "c", EmbedModel: "e"})
	require.NoError(t, err)
	vecs, err := p.Embed(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, vecs)
}

func TestModelNotFound(t *testing.T) {
	p, _ := fakeOllama(t, 404, map[string]any{"error": `model "embed-m" not found`})
	_, err := p.Embed(context.Background(), []string{"а"})
	require.ErrorIs(t, err, llm.ErrUnavailable)
	require.ErrorContains(t, err, "make models")
}

func TestServerDown(t *testing.T) {
	p, err := New(Config{URL: "http://127.0.0.1:1", ChatModel: "c", EmbedModel: "e"})
	require.NoError(t, err)
	_, err = p.Chat(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "?"}}, llm.ChatOptions{})
	require.ErrorIs(t, err, llm.ErrUnavailable)
	require.ErrorIs(t, p.Ping(context.Background()), llm.ErrUnavailable)
}

func TestBadConfig(t *testing.T) {
	_, err := New(Config{URL: "localhost", ChatModel: "c", EmbedModel: "e"})
	require.Error(t, err)
	_, err = New(Config{URL: "http://x:1"})
	require.Error(t, err)
}

// TestLiveOllama ходит в настоящую Ollama, если задан OLLAMA_TEST_URL
// (и скачаны модели из LLM_CHAT_MODEL / LLM_EMBED_MODEL).
func TestLiveOllama(t *testing.T) {
	u := os.Getenv("OLLAMA_TEST_URL")
	if u == "" {
		t.Skip("OLLAMA_TEST_URL не задан")
	}
	p, err := New(Config{URL: u, ChatModel: os.Getenv("LLM_CHAT_MODEL"), EmbedModel: os.Getenv("LLM_EMBED_MODEL")})
	require.NoError(t, err)
	require.NoError(t, p.Ping(context.Background()))

	vecs, err := p.Embed(context.Background(), []string{"Сколько стоит доставка?"})
	require.NoError(t, err)
	require.Len(t, vecs, 1)
	t.Logf("размерность эмбеддинга: %d", len(vecs[0]))

	answer, err := p.Chat(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "Ответь одним словом: столица России?"}},
		llm.ChatOptions{MaxTokens: 20})
	require.NoError(t, err)
	require.NotEmpty(t, answer)
}

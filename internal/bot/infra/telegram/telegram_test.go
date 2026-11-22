package telegram

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"closerai/internal/bot/domain"
)

const goodToken = "123456789:AAFakeTokenFakeTokenFakeToken_-12345"

// fakeTelegram — минимальный Bot API: принимает goodToken, остальные отклоняет.
type fakeTelegram struct {
	webhook     map[string]any // параметры последнего setWebhook
	deleted     bool
	webhookFail string // если не пусто — setWebhook отвечает 400 с этим текстом
}

func (f *fakeTelegram) serve(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, method, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/bot"), "/")
		w.Header().Set("Content-Type", "application/json")
		if tok != goodToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
			return
		}
		switch method {
		case "getMe":
			_, _ = io.WriteString(w, `{"ok":true,"result":{"id":123456789,"is_bot":true,"first_name":"Shop","username":"shop_bot"}}`)
		case "setWebhook":
			if f.webhookFail != "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"ok":false,"error_code":400,"description":"`+f.webhookFail+`"}`)
				return
			}
			f.webhook = parseParams(t, r)
			_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
		case "deleteWebhook":
			f.deleted = true
			_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":404,"description":"Not Found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// parseParams читает параметры запроса (библиотека шлёт multipart-форму).
func parseParams(t *testing.T, r *http.Request) map[string]any {
	require.NoError(t, r.ParseMultipartForm(1<<20))
	out := map[string]any{}
	for k, v := range r.MultipartForm.Value {
		var decoded any
		if json.Unmarshal([]byte(v[0]), &decoded) == nil && k == "allowed_updates" {
			out[k] = decoded
		} else {
			out[k] = v[0]
		}
	}
	return out
}

func TestGetMe(t *testing.T) {
	srv := (&fakeTelegram{}).serve(t)
	c := NewClient(srv.URL)

	me, err := c.GetMe(context.Background(), goodToken)
	require.NoError(t, err)
	require.Equal(t, domain.TelegramIdentity{ID: 123456789, Username: "shop_bot"}, me)

	_, err = c.GetMe(context.Background(), "111111111:AAWrongWrongWrongWrongWrongWrong_12")
	require.ErrorIs(t, err, domain.ErrTokenRejected)
}

func TestNetworkErrorDoesNotLeakToken(t *testing.T) {
	c := NewClient("http://127.0.0.1:1") // никто не слушает
	_, err := c.GetMe(context.Background(), goodToken)
	require.ErrorIs(t, err, domain.ErrTelegramUnavailable)
	require.NotContains(t, err.Error(), goodToken)
	require.NotContains(t, err.Error(), "FakeToken")
}

func TestWebhookRegistrar(t *testing.T) {
	fake := &fakeTelegram{}
	srv := fake.serve(t)
	var logs strings.Builder
	w := NewWebhookRegistrar(NewClient(srv.URL), "https://closer.example/", slog.New(slog.NewTextHandler(&logs, nil)))
	b := domain.Bot{ID: uuid.MustParse("6f9c2b1e-0000-4000-8000-000000000001"), WebhookSecret: "s3cr3t"}

	require.NoError(t, w.Register(context.Background(), b, goodToken))
	require.Equal(t, "https://closer.example/tg/webhook/6f9c2b1e-0000-4000-8000-000000000001", fake.webhook["url"])
	require.Equal(t, "s3cr3t", fake.webhook["secret_token"])
	require.Equal(t, []any{"message"}, fake.webhook["allowed_updates"])

	w.Unregister(context.Background(), goodToken)
	require.True(t, fake.deleted)

	fake.webhookFail = "Bad Request: bad webhook: HTTPS url must be provided for webhook"
	err := w.Register(context.Background(), b, goodToken)
	var we *domain.WebhookError
	require.ErrorAs(t, err, &we)
	require.Equal(t, "Bad Request: bad webhook: HTTPS url must be provided for webhook", we.Reason)

	// неудачное снятие вебхука только логируется, токен в лог не попадает
	NewWebhookRegistrar(NewClient("http://127.0.0.1:1"), "https://x", slog.New(slog.NewTextHandler(&logs, nil))).
		Unregister(context.Background(), goodToken)
	require.Contains(t, logs.String(), "не удалось снять вебхук")
	require.NotContains(t, logs.String(), goodToken)
}

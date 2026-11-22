package tgapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"closerai/internal/telegram/domain"
)

const token = "123456789:AAFakeTokenFakeTokenFakeToken_-12345"

// serve поднимает фейковый Bot API: handler получает метод и JSON-параметры.
func serve(t *testing.T, handler func(method string, params map[string]any) (int, string)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, method, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/bot"), "/")
		require.Equal(t, token, tok)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var params map[string]any
		body, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(body, &params))
		code, resp := handler(method, params)
		w.WriteHeader(code)
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL)
}

func TestSendMessage(t *testing.T) {
	c := serve(t, func(method string, p map[string]any) (int, string) {
		require.Equal(t, "sendMessage", method)
		require.Equal(t, float64(42), p["chat_id"])
		require.Equal(t, "привет", p["text"])
		return 200, `{"ok":true,"result":{"message_id":777,"date":0,"chat":{"id":42,"type":"private"}}}`
	})
	id, err := c.SendMessage(context.Background(), token, 42, "привет")
	require.NoError(t, err)
	require.Equal(t, int64(777), id)
}

func TestSendMessageErrors(t *testing.T) {
	cases := []struct {
		code int
		desc string
		want error
	}{
		{403, "Forbidden: bot was blocked by the user", domain.ErrChatUnavailable},
		{400, "Bad Request: chat not found", domain.ErrChatUnavailable},
		{401, "Unauthorized", domain.ErrBotUnavailable},
		{429, "Too Many Requests: retry after 5", domain.ErrTelegramUnavailable},
		{502, "Bad Gateway", domain.ErrTelegramUnavailable},
	}
	for _, tc := range cases {
		c := serve(t, func(string, map[string]any) (int, string) {
			b, _ := json.Marshal(map[string]any{"ok": false, "error_code": tc.code, "description": tc.desc})
			return tc.code, string(b)
		})
		_, err := c.SendMessage(context.Background(), token, 1, "x")
		require.ErrorIs(t, err, tc.want, tc.desc)
		require.NotContains(t, err.Error(), token)
	}
}

func TestNetworkErrorHidesToken(t *testing.T) {
	c := NewClient("http://127.0.0.1:1") // никто не слушает
	_, err := c.SendMessage(context.Background(), token, 1, "x")
	require.ErrorIs(t, err, domain.ErrTelegramUnavailable)
	require.NotContains(t, err.Error(), token)
}

func TestGetUpdates(t *testing.T) {
	c := serve(t, func(method string, p map[string]any) (int, string) {
		require.Equal(t, "getUpdates", method)
		require.Equal(t, float64(5), p["offset"])
		require.Equal(t, float64(1), p["timeout"])
		return 200, `{"ok":true,"result":[
			{"update_id":5,"message":{"message_id":10,"date":1700000000,
			  "chat":{"id":42,"type":"private"},
			  "from":{"id":42,"is_bot":false,"first_name":"Иван","username":"ivan"},
			  "text":"привет"}},
			{"update_id":6,"message":{"message_id":11,"date":1700000001,
			  "chat":{"id":42,"type":"private"},"from":{"id":42,"is_bot":false},
			  "photo":[],"caption":"смотрите фото"}},
			{"update_id":7,"message":"сломанный"}
		]}`
	})
	ups, err := c.GetUpdates(context.Background(), token, 5, time.Second)
	require.NoError(t, err)
	require.Len(t, ups, 3)

	m := ups[0].Message
	require.Equal(t, int64(5), ups[0].ID)
	require.Equal(t, int64(10), m.ID)
	require.Equal(t, int64(42), m.ChatID)
	require.Equal(t, domain.ChatPrivate, m.ChatType)
	require.Equal(t, "привет", m.Text)
	require.Equal(t, "ivan", m.From.Username)
	require.Equal(t, time.Unix(1700000000, 0), m.Date)

	require.Equal(t, "смотрите фото", ups[1].Message.Text, "подпись к медиа считается текстом")

	require.Equal(t, int64(7), ups[2].ID, "неразобранный апдейт — только id")
	require.Nil(t, ups[2].Message)
}

package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"closerai/internal/telegram/app"
	"closerai/internal/telegram/domain"
)

type oneBot struct{ c domain.Credentials }

func (b oneBot) Credentials(_ context.Context, id uuid.UUID) (domain.Credentials, error) {
	if id != b.c.BotID {
		return domain.Credentials{}, domain.ErrBotNotFound
	}
	return b.c, nil
}
func (b oneBot) Active(context.Context) ([]domain.Credentials, error) { return nil, nil }

type inbox struct {
	got []domain.Inbound
	err error
}

func (i *inbox) Deliver(_ context.Context, m domain.Inbound) error {
	i.got = append(i.got, m)
	return i.err
}

type noAPI struct{}

func (noAPI) SendMessage(context.Context, string, int64, string) (int64, error) { return 0, nil }
func (noAPI) SendTyping(context.Context, string, int64) error                   { return nil }

const update = `{"update_id":1,"message":{"message_id":3,"date":1700000000,
	"chat":{"id":9,"type":"private"},"from":{"id":9,"is_bot":false,"first_name":"Аня"},"text":"здравствуйте"}}`

func TestWebhook(t *testing.T) {
	gin.SetMode(gin.TestMode)
	bot := domain.Credentials{BotID: uuid.New(), TenantID: uuid.New(), WebhookSecret: "s3cret", IsActive: true}

	cases := []struct {
		name   string
		path   string
		secret string
		body   string
		inErr  error
		status int
		got    int
	}{
		{"ok", bot.BotID.String(), "s3cret", update, nil, 200, 1},
		{"неверный секрет", bot.BotID.String(), "wrong", update, nil, 401, 0},
		{"неизвестный бот", uuid.NewString(), "s3cret", update, nil, 404, 0},
		{"кривой id", "abc", "s3cret", update, nil, 404, 0},
		{"кривой json", bot.BotID.String(), "s3cret", "{", nil, 400, 0},
		{"сбой обработки — Telegram повторит", bot.BotID.String(), "s3cret", update, errors.New("db"), 500, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := &inbox{err: tc.inErr}
			svc, err := app.NewService(app.Deps{Bots: oneBot{bot}, Inbox: in, API: noAPI{}})
			require.NoError(t, err)
			r := gin.New()
			NewHandler(svc).Routes(r)

			req := httptest.NewRequest(http.MethodPost, "/tg/webhook/"+tc.path, strings.NewReader(tc.body))
			req.Header.Set(SecretHeader, tc.secret)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, tc.status, w.Code)
			require.Len(t, in.got, tc.got)
			if tc.got > 0 {
				require.Equal(t, "здравствуйте", in.got[0].Message.Text)
				require.Equal(t, "Аня", in.got[0].Message.From.FirstName)
			}
		})
	}
}

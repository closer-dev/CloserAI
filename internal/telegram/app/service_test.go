package app

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"closerai/internal/telegram/domain"
)

type fakeBots map[uuid.UUID]domain.Credentials

func (f fakeBots) Credentials(_ context.Context, id uuid.UUID) (domain.Credentials, error) {
	c, ok := f[id]
	if !ok {
		return domain.Credentials{}, domain.ErrBotNotFound
	}
	return c, nil
}

func (f fakeBots) Active(context.Context) ([]domain.Credentials, error) {
	var out []domain.Credentials
	for _, c := range f {
		if c.IsActive {
			out = append(out, c)
		}
	}
	return out, nil
}

type fakeInbox struct{ got []domain.Inbound }

func (f *fakeInbox) Deliver(_ context.Context, m domain.Inbound) error {
	f.got = append(f.got, m)
	return nil
}

type fakeAPI struct {
	tokens []string
	typing int
}

func (f *fakeAPI) SendMessage(_ context.Context, token string, _ int64, _ string) (int64, error) {
	f.tokens = append(f.tokens, token)
	return 99, nil
}

func (f *fakeAPI) SendTyping(context.Context, string, int64) error {
	f.typing++
	return nil
}

var (
	active   = domain.Credentials{BotID: uuid.New(), TenantID: uuid.New(), Token: "tok-a", WebhookSecret: "sec-a", IsActive: true}
	inactive = domain.Credentials{BotID: uuid.New(), TenantID: uuid.New(), Token: "tok-b", WebhookSecret: "sec-b"}
)

func setup(t *testing.T) (*Service, *fakeInbox, *fakeAPI) {
	t.Helper()
	inbox, api := &fakeInbox{}, &fakeAPI{}
	svc, err := NewService(Deps{
		Bots:  fakeBots{active.BotID: active, inactive.BotID: inactive},
		Inbox: inbox, API: api,
	})
	require.NoError(t, err)
	return svc, inbox, api
}

func textUpdate(text string) domain.Update {
	return domain.Update{ID: 1, Message: &domain.Message{
		ID: 10, ChatID: 7, ChatType: domain.ChatPrivate, From: &domain.User{ID: 7}, Text: text,
	}}
}

func TestHandleWebhook(t *testing.T) {
	ctx := context.Background()

	t.Run("принимает сообщение клиента", func(t *testing.T) {
		svc, inbox, _ := setup(t)
		require.NoError(t, svc.HandleWebhook(ctx, active.BotID, "sec-a", textUpdate("привет")))
		require.Len(t, inbox.got, 1)
		require.Equal(t, active.TenantID, inbox.got[0].TenantID)
		require.Equal(t, active.BotID, inbox.got[0].BotID)
		require.Equal(t, "привет", inbox.got[0].Message.Text)
	})

	t.Run("чужой секрет", func(t *testing.T) {
		svc, inbox, _ := setup(t)
		err := svc.HandleWebhook(ctx, active.BotID, "sec-b", textUpdate("x"))
		require.ErrorIs(t, err, domain.ErrBadSecret)
		require.Empty(t, inbox.got)
	})

	t.Run("неизвестный бот", func(t *testing.T) {
		svc, _, _ := setup(t)
		require.ErrorIs(t, svc.HandleWebhook(ctx, uuid.New(), "sec-a", textUpdate("x")), domain.ErrBotNotFound)
	})

	t.Run("выключенный бот — молча пропускаем", func(t *testing.T) {
		svc, inbox, _ := setup(t)
		require.NoError(t, svc.HandleWebhook(ctx, inactive.BotID, "sec-b", textUpdate("x")))
		require.Empty(t, inbox.got)
	})

	t.Run("не сообщение клиента", func(t *testing.T) {
		svc, inbox, _ := setup(t)
		require.NoError(t, svc.HandleWebhook(ctx, active.BotID, "sec-a", domain.Update{ID: 2}))
		require.Empty(t, inbox.got)
	})
}

func TestSend(t *testing.T) {
	ctx := context.Background()
	svc, _, api := setup(t)

	id, err := svc.Send(ctx, active.BotID, 7, "ответ")
	require.NoError(t, err)
	require.Equal(t, int64(99), id)
	require.Equal(t, []string{"tok-a"}, api.tokens)

	_, err = svc.Send(ctx, inactive.BotID, 7, "ответ")
	require.ErrorIs(t, err, domain.ErrBotUnavailable)
	_, err = svc.Send(ctx, uuid.New(), 7, "ответ")
	require.ErrorIs(t, err, domain.ErrBotUnavailable)

	svc.Typing(ctx, active.BotID, 7)
	svc.Typing(ctx, inactive.BotID, 7) // ошибка только в лог
	require.Equal(t, 1, api.typing)
}

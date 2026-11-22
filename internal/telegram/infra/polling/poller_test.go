package polling

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"closerai/internal/telegram/app"
	"closerai/internal/telegram/domain"
	"closerai/internal/telegram/infra/tgapi"
)

// fakeAPI отдаёт апдейты из очереди, начиная с offset, и запоминает запросы.
type fakeAPI struct {
	mu      sync.Mutex
	updates []domain.Update
	offsets []int64
	deleted int
	getErr  error
}

func (f *fakeAPI) GetUpdates(ctx context.Context, token string, offset int64, _ time.Duration) ([]domain.Update, error) {
	f.mu.Lock()
	f.offsets = append(f.offsets, offset)
	err := f.getErr
	var out []domain.Update
	for _, u := range f.updates {
		if u.ID >= offset {
			out = append(out, u)
		}
	}
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		select { // long poll: ждём, пока не отменят
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
			return nil, nil
		}
	}
	return out, nil
}

func (f *fakeAPI) DeleteWebhook(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted++
	return nil
}

func (f *fakeAPI) lastOffset() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.offsets[len(f.offsets)-1]
}

type bots map[uuid.UUID]domain.Credentials

func (b bots) Credentials(_ context.Context, id uuid.UUID) (domain.Credentials, error) {
	c, ok := b[id]
	if !ok {
		return domain.Credentials{}, domain.ErrBotNotFound
	}
	return c, nil
}

func (b bots) Active(context.Context) ([]domain.Credentials, error) {
	var out []domain.Credentials
	for _, c := range b {
		if c.IsActive {
			out = append(out, c)
		}
	}
	return out, nil
}

// inbox падает failN раз, потом принимает.
type inbox struct {
	mu    sync.Mutex
	got   []int64
	failN int
}

func (i *inbox) Deliver(_ context.Context, m domain.Inbound) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.failN > 0 {
		i.failN--
		return errors.New("db down")
	}
	i.got = append(i.got, m.Message.ID)
	return nil
}

func (i *inbox) messages() []int64 {
	i.mu.Lock()
	defer i.mu.Unlock()
	return append([]int64(nil), i.got...)
}

func msg(updateID, msgID int64) domain.Update {
	return domain.Update{ID: updateID, Message: &domain.Message{
		ID: msgID, ChatID: 1, ChatType: domain.ChatPrivate, From: &domain.User{ID: 1}, Text: "x",
	}}
}

func newManager(t *testing.T, b bots, in *inbox, api *fakeAPI) *Manager {
	t.Helper()
	svc, err := app.NewService(app.Deps{Bots: b, Inbox: in, API: noSend{}})
	require.NoError(t, err)
	m := NewManager(svc, api, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(m.Stop)
	return m
}

type noSend struct{}

func (noSend) SendMessage(context.Context, string, int64, string) (int64, error) { return 0, nil }
func (noSend) SendTyping(context.Context, string, int64) error                   { return nil }

func TestPollDeliversAndAdvancesOffset(t *testing.T) {
	bot := domain.Credentials{BotID: uuid.New(), TenantID: uuid.New(), Token: "t", IsActive: true}
	api := &fakeAPI{updates: []domain.Update{msg(10, 1), msg(11, 2)}}
	in := &inbox{}
	m := newManager(t, bots{bot.BotID: bot}, in, api)

	require.NoError(t, m.Start(context.Background()))
	require.Eventually(t, func() bool { return len(in.messages()) == 2 }, 2*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return api.lastOffset() == 12 }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, []int64{1, 2}, in.messages())
	require.Equal(t, 1, api.deleted, "перед опросом снимается вебхук")
}

func TestPollRetriesFailedUpdate(t *testing.T) {
	bot := domain.Credentials{BotID: uuid.New(), TenantID: uuid.New(), Token: "t", IsActive: true}
	api := &fakeAPI{updates: []domain.Update{msg(10, 1)}}
	in := &inbox{failN: 1}
	m := newManager(t, bots{bot.BotID: bot}, in, api)

	require.NoError(t, m.Start(context.Background()))
	// после ошибки offset не сдвигается, апдейт приходит снова (через ~1 с бэкоффа)
	require.Eventually(t, func() bool { return len(in.messages()) == 1 }, 3*time.Second, 10*time.Millisecond)
}

func TestActivateDeactivate(t *testing.T) {
	bot := domain.Credentials{BotID: uuid.New(), TenantID: uuid.New(), Token: "t", IsActive: true}
	b := bots{}
	api := &fakeAPI{}
	m := newManager(t, b, &inbox{}, api)
	require.NoError(t, m.Start(context.Background()))

	b[bot.BotID] = bot
	require.NoError(t, m.Activate(context.Background(), bot.BotID))
	require.Eventually(t, func() bool {
		api.mu.Lock()
		defer api.mu.Unlock()
		return len(api.offsets) > 0
	}, time.Second, 5*time.Millisecond)

	m.Deactivate(bot.BotID)
	m.mu.Lock()
	require.Empty(t, m.runners)
	m.mu.Unlock()
}

func TestUnauthorizedStopsPolling(t *testing.T) {
	bot := domain.Credentials{BotID: uuid.New(), TenantID: uuid.New(), Token: "t", IsActive: true}
	api := &fakeAPI{getErr: &tgapi.APIError{Code: 401, Description: "Unauthorized"}}
	m := newManager(t, bots{bot.BotID: bot}, &inbox{}, api)
	require.NoError(t, m.Start(context.Background()))

	m.mu.Lock()
	r := m.runners[bot.BotID]
	m.mu.Unlock()
	select {
	case <-r.done:
	case <-time.After(time.Second):
		t.Fatal("опрос с отозванным токеном должен остановиться")
	}
}

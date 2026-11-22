package app

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"closerai/internal/bot/domain"
	"closerai/internal/kernel"
)

const (
	token    = "123456789:AAFakeTokenFakeTokenFakeToken_-12345"
	newToken = "123456789:AANewTokenNewTokenNewTokenNew_-1234"
	tgID     = int64(123456789)
)

// ---------- фейки портов ----------

type memRepo struct {
	bots   map[uuid.UUID]domain.Bot
	tokens map[uuid.UUID]string
}

func newRepo() *memRepo {
	return &memRepo{bots: map[uuid.UUID]domain.Bot{}, tokens: map[uuid.UUID]string{}}
}

func (r *memRepo) Create(_ context.Context, b domain.Bot, tok string) (domain.Bot, error) {
	for _, x := range r.bots {
		if x.TgBotID == b.TgBotID {
			return domain.Bot{}, domain.ErrAlreadyConnected
		}
	}
	b.ID = uuid.New()
	r.bots[b.ID], r.tokens[b.ID] = b, tok
	return b, nil
}
func (r *memRepo) List(_ context.Context, tenant uuid.UUID) ([]domain.Bot, error) {
	var out []domain.Bot
	for _, b := range r.bots {
		if b.TenantID == tenant {
			out = append(out, b)
		}
	}
	return out, nil
}
func (r *memRepo) Get(_ context.Context, tenant, id uuid.UUID) (domain.Bot, error) {
	b, ok := r.bots[id]
	if !ok || b.TenantID != tenant {
		return domain.Bot{}, domain.ErrNotFound
	}
	return b, nil
}
func (r *memRepo) GetForUpdate(ctx context.Context, tenant, id uuid.UUID) (domain.Bot, error) {
	return r.Get(ctx, tenant, id)
}
func (r *memRepo) Update(_ context.Context, b domain.Bot) (domain.Bot, error) {
	r.bots[b.ID] = b
	return b, nil
}
func (r *memRepo) UpdateToken(_ context.Context, _, id uuid.UUID, tok string) error {
	r.tokens[id] = tok
	return nil
}
func (r *memRepo) Delete(_ context.Context, _, id uuid.UUID) error {
	delete(r.bots, id)
	delete(r.tokens, id)
	return nil
}
func (r *memRepo) Credentials(_ context.Context, id uuid.UUID) (domain.Credentials, error) {
	b, ok := r.bots[id]
	if !ok {
		return domain.Credentials{}, domain.ErrNotFound
	}
	return domain.Credentials{BotID: id, TenantID: b.TenantID, Token: r.tokens[id], WebhookSecret: b.WebhookSecret, IsActive: b.IsActive}, nil
}
func (r *memRepo) ActiveCredentials(context.Context) ([]domain.Credentials, error) { return nil, nil }

// fakeTx имитирует откат: при ошибке fn возвращает репозиторий в прежнее состояние.
type fakeTx struct{ repo *memRepo }

func (t fakeTx) Do(ctx context.Context, fn func(context.Context) error) error {
	bots, tokens := map[uuid.UUID]domain.Bot{}, map[uuid.UUID]string{}
	for k, v := range t.repo.bots {
		bots[k] = v
	}
	for k, v := range t.repo.tokens {
		tokens[k] = v
	}
	if err := fn(ctx); err != nil {
		t.repo.bots, t.repo.tokens = bots, tokens
		return err
	}
	return nil
}

type fakeTG struct {
	ids map[string]int64 // токен → id бота; нет в карте — токен отклонён
	err error
}

func (f fakeTG) GetMe(_ context.Context, tok string) (domain.TelegramIdentity, error) {
	if f.err != nil {
		return domain.TelegramIdentity{}, f.err
	}
	id, ok := f.ids[tok]
	if !ok {
		return domain.TelegramIdentity{}, domain.ErrTokenRejected
	}
	return domain.TelegramIdentity{ID: id, Username: "shop_bot"}, nil
}

type fakeHooks struct {
	registered   []string
	unregistered []string
	err          error
}

func (f *fakeHooks) Register(_ context.Context, _ domain.Bot, tok string) error {
	if f.err != nil {
		return f.err
	}
	f.registered = append(f.registered, tok)
	return nil
}
func (f *fakeHooks) Unregister(_ context.Context, tok string) {
	f.unregistered = append(f.unregistered, tok)
}

type fakeEvents struct{ got []kernel.Event }

func (f *fakeEvents) Publish(_ context.Context, ev ...kernel.Event) { f.got = append(f.got, ev...) }

type env struct {
	svc    *Service
	repo   *memRepo
	hooks  *fakeHooks
	events *fakeEvents
	tenant uuid.UUID
}

func newEnv(t *testing.T) *env {
	t.Helper()
	repo, hooks, events := newRepo(), &fakeHooks{}, &fakeEvents{}
	svc, err := NewService(Deps{
		Repo: repo, Tx: fakeTx{repo}, Webhooks: hooks, Events: events,
		Telegram: fakeTG{ids: map[string]int64{token: tgID, newToken: tgID, "987654321:AAOtherBotOtherBotOtherBotOther_123": 987654321}},
	})
	require.NoError(t, err)
	return &env{svc: svc, repo: repo, hooks: hooks, events: events, tenant: uuid.New()}
}

func (e *env) connect(t *testing.T) domain.Bot {
	t.Helper()
	b, err := e.svc.Connect(context.Background(), e.tenant, ConnectInput{Token: " " + token + " "})
	require.NoError(t, err)
	e.events.got = nil
	e.hooks.registered = nil
	return b
}

// ---------- тесты ----------

func TestConnect(t *testing.T) {
	e := newEnv(t)
	b, err := e.svc.Connect(context.Background(), e.tenant, ConnectInput{Token: " " + token + " "})
	require.NoError(t, err)

	require.Equal(t, tgID, b.TgBotID)
	require.Equal(t, "shop_bot", b.Username)
	require.True(t, b.IsActive)
	require.NotEmpty(t, b.WebhookSecret)
	require.Equal(t, domain.DefaultSettings(), b.Settings)
	require.Equal(t, token, e.repo.tokens[b.ID], "токен сохраняется без пробелов")
	require.Equal(t, []string{token}, e.hooks.registered)
	require.Equal(t, []kernel.Event{kernel.BotActivated{TenantID: e.tenant, BotID: b.ID}}, e.events.got)
}

func TestConnectErrors(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)

	_, err := e.svc.Connect(ctx, e.tenant, ConnectInput{Token: "мусор"})
	require.ErrorIs(t, err, domain.ErrInvalidToken)

	_, err = e.svc.Connect(ctx, e.tenant, ConnectInput{Token: "111111111:AAUnknownUnknownUnknownUnknown_-123"})
	require.ErrorIs(t, err, domain.ErrTokenRejected)

	bad := float32(2)
	_, err = e.svc.Connect(ctx, e.tenant, ConnectInput{Token: token, Settings: domain.SettingsPatch{HandoffThreshold: &bad}})
	require.ErrorIs(t, err, domain.ErrInvalidThreshold)

	e.connect(t)
	_, err = e.svc.Connect(ctx, uuid.New(), ConnectInput{Token: token})
	require.ErrorIs(t, err, domain.ErrAlreadyConnected, "тот же бот в другой компании")
}

func TestConnectRollsBackWhenWebhookFails(t *testing.T) {
	e := newEnv(t)
	e.hooks.err = &domain.WebhookError{Reason: "HTTPS url must be provided for webhook"}

	_, err := e.svc.Connect(context.Background(), e.tenant, ConnectInput{Token: token})
	var we *domain.WebhookError
	require.ErrorAs(t, err, &we)
	require.Empty(t, e.repo.bots, "бот не должен остаться без вебхука")
	require.Empty(t, e.events.got)
}

func TestUpdateSettingsOnly(t *testing.T) {
	e := newEnv(t)
	b := e.connect(t)
	prompt := "Отвечай вежливо"
	got, err := e.svc.Update(context.Background(), e.tenant, b.ID, UpdateInput{Settings: domain.SettingsPatch{SystemPrompt: &prompt}})
	require.NoError(t, err)
	require.Equal(t, prompt, got.Settings.SystemPrompt)
	require.Empty(t, e.hooks.registered, "вебхук не трогаем")
	require.Empty(t, e.events.got)
}

func TestDeactivateAndActivate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	b := e.connect(t)

	got, err := e.svc.Update(ctx, e.tenant, b.ID, UpdateInput{IsActive: new(false)})
	require.NoError(t, err)
	require.False(t, got.IsActive)
	require.Equal(t, []string{token}, e.hooks.unregistered)
	require.Equal(t, []kernel.Event{kernel.BotDeactivated{TenantID: e.tenant, BotID: b.ID}}, e.events.got)

	// повторное выключение ничего не делает
	e.events.got, e.hooks.unregistered = nil, nil
	_, err = e.svc.Update(ctx, e.tenant, b.ID, UpdateInput{IsActive: new(false)})
	require.NoError(t, err)
	require.Empty(t, e.events.got)
	require.Empty(t, e.hooks.unregistered)

	_, err = e.svc.Update(ctx, e.tenant, b.ID, UpdateInput{IsActive: new(true)})
	require.NoError(t, err)
	require.Equal(t, []string{token}, e.hooks.registered)
	require.Equal(t, []kernel.Event{kernel.BotActivated{TenantID: e.tenant, BotID: b.ID}}, e.events.got)
}

func TestUpdateToken(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	b := e.connect(t)

	_, err := e.svc.Update(ctx, e.tenant, b.ID, UpdateInput{Token: new(newToken)})
	require.NoError(t, err)
	require.Equal(t, newToken, e.repo.tokens[b.ID])
	require.Equal(t, []string{newToken}, e.hooks.registered, "вебхук переставлен с новым токеном")
	require.Equal(t, []kernel.Event{kernel.BotActivated{TenantID: e.tenant, BotID: b.ID}}, e.events.got)

	_, err = e.svc.Update(ctx, e.tenant, b.ID, UpdateInput{Token: new("987654321:AAOtherBotOtherBotOtherBotOther_123")})
	require.ErrorIs(t, err, domain.ErrTokenMismatch)
	require.Equal(t, newToken, e.repo.tokens[b.ID], "токен другого бота не сохраняется")
}

func TestOtherTenantCannotTouchBot(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	b := e.connect(t)
	stranger := uuid.New()

	_, err := e.svc.Get(ctx, stranger, b.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = e.svc.Update(ctx, stranger, b.ID, UpdateInput{IsActive: new(false)})
	require.ErrorIs(t, err, domain.ErrNotFound)
	require.ErrorIs(t, e.svc.Delete(ctx, stranger, b.ID), domain.ErrNotFound)
	require.Contains(t, e.repo.bots, b.ID)
}

func TestDelete(t *testing.T) {
	e := newEnv(t)
	b := e.connect(t)
	require.NoError(t, e.svc.Delete(context.Background(), e.tenant, b.ID))
	require.Empty(t, e.repo.bots)
	require.Equal(t, []string{token}, e.hooks.unregistered)
	require.Equal(t, []kernel.Event{kernel.BotDeactivated{TenantID: e.tenant, BotID: b.ID}}, e.events.got)
}

func TestTelegramUnavailable(t *testing.T) {
	repo := newRepo()
	svc, err := NewService(Deps{Repo: repo, Tx: fakeTx{repo}, Webhooks: &fakeHooks{}, Events: &fakeEvents{},
		Telegram: fakeTG{err: errors.Join(domain.ErrTelegramUnavailable, errors.New("timeout"))}})
	require.NoError(t, err)
	_, err = svc.Connect(context.Background(), uuid.New(), ConnectInput{Token: token})
	require.ErrorIs(t, err, domain.ErrTelegramUnavailable)
}

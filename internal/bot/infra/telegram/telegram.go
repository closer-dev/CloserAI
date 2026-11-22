// Package telegram — адаптеры контекста bot к Telegram Bot API
// (go-telegram/bot): проверка токена (getMe) и управление вебхуком.
//
// Токен не должен попасть в логи. В URL запроса он есть (/bot<TOKEN>/getMe);
// go-telegram/bot сейчас маскирует его в ошибках сам, но на это не полагаемся:
// все ошибки здесь проходят через clean и не оборачиваются через %w.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"

	"closerai/internal/bot/app"
	"closerai/internal/bot/domain"
)

const requestTimeout = 15 * time.Second

// Client вызывает Telegram Bot API от имени любого бота по его токену.
type Client struct {
	serverURL string // пусто — https://api.telegram.org
	http      *http.Client
}

func NewClient(serverURL string) *Client {
	return &Client{serverURL: serverURL, http: &http.Client{Timeout: requestTimeout}}
}

var _ app.TelegramAPI = (*Client)(nil)

func (c *Client) api(token string) (*tgbot.Bot, error) {
	opts := []tgbot.Option{tgbot.WithSkipGetMe(), tgbot.WithHTTPClient(requestTimeout, c.http)}
	if c.serverURL != "" {
		opts = append(opts, tgbot.WithServerURL(c.serverURL))
	}
	b, err := tgbot.New(token, opts...)
	if err != nil {
		return nil, clean(err, token)
	}
	return b, nil
}

func (c *Client) GetMe(ctx context.Context, token string) (domain.TelegramIdentity, error) {
	b, err := c.api(token)
	if err != nil {
		return domain.TelegramIdentity{}, err
	}
	me, err := b.GetMe(ctx)
	switch {
	case errors.Is(err, tgbot.ErrorUnauthorized), errors.Is(err, tgbot.ErrorNotFound):
		return domain.TelegramIdentity{}, domain.ErrTokenRejected
	case err != nil:
		return domain.TelegramIdentity{}, fmt.Errorf("%w: getMe: %v", domain.ErrTelegramUnavailable, clean(err, token))
	case !me.IsBot:
		return domain.TelegramIdentity{}, domain.ErrTokenRejected
	}
	return domain.TelegramIdentity{ID: me.ID, Username: me.Username}, nil
}

// WebhookRegistrar — режим TG_MODE=webhook: Telegram сам присылает апдейты
// на {PUBLIC_URL}/tg/webhook/{bot_id} с секретом в заголовке.
type WebhookRegistrar struct {
	client    *Client
	publicURL string
	log       *slog.Logger
}

func NewWebhookRegistrar(client *Client, publicURL string, log *slog.Logger) *WebhookRegistrar {
	return &WebhookRegistrar{client: client, publicURL: strings.TrimRight(publicURL, "/"), log: log}
}

var _ app.Webhooks = (*WebhookRegistrar)(nil)

// WebhookURL — куда Telegram шлёт апдейты бота. Маршрут обслуживает контекст telegram.
func WebhookURL(publicURL, botID string) string {
	return strings.TrimRight(publicURL, "/") + "/tg/webhook/" + botID
}

func (w *WebhookRegistrar) Register(ctx context.Context, b domain.Bot, token string) error {
	api, err := w.client.api(token)
	if err != nil {
		return err
	}
	_, err = api.SetWebhook(ctx, &tgbot.SetWebhookParams{
		URL:            WebhookURL(w.publicURL, b.ID.String()),
		SecretToken:    b.WebhookSecret,
		AllowedUpdates: []string{"message"},
	})
	switch {
	case errors.Is(err, tgbot.ErrorBadRequest), errors.Is(err, tgbot.ErrorUnauthorized):
		return &domain.WebhookError{Reason: description(clean(err, token))}
	case err != nil:
		return fmt.Errorf("%w: setWebhook: %v", domain.ErrTelegramUnavailable, clean(err, token))
	}
	return nil
}

func (w *WebhookRegistrar) Unregister(ctx context.Context, token string) {
	api, err := w.client.api(token)
	if err == nil {
		_, err = api.DeleteWebhook(ctx, &tgbot.DeleteWebhookParams{})
	}
	if err != nil {
		w.log.WarnContext(ctx, "bot: не удалось снять вебхук", "err", clean(err, token))
	}
}

// NoWebhooks — режим TG_MODE=polling: вебхуки не ставятся, апдейты забирает
// контекст telegram через getUpdates (и сам снимает вебхук перед опросом).
type NoWebhooks struct{}

var _ app.Webhooks = NoWebhooks{}

func (NoWebhooks) Register(context.Context, domain.Bot, string) error { return nil }
func (NoWebhooks) Unregister(context.Context, string)                 {}

// clean убирает токен из текста ошибки и рвёт цепочку ошибок, чтобы токен
// не всплыл через errors.Unwrap в логах.
func clean(err error, token string) error {
	if err == nil {
		return nil
	}
	return errors.New(strings.ReplaceAll(err.Error(), token, "<token>"))
}

// description достаёт текст Telegram из ошибки вида "bad request, <описание>".
func description(err error) string {
	msg := err.Error()
	if _, after, ok := strings.Cut(msg, ", "); ok {
		return after
	}
	return msg
}

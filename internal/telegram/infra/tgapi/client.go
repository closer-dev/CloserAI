// Package tgapi — клиент Telegram Bot API для контекста telegram:
// getUpdates, deleteWebhook, sendMessage, sendChatAction и перевод апдейтов
// Bot API (go-telegram/bot/models) в доменные.
//
// Запросы делаются напрямую через net/http: для polling нужен контроль над
// offset (сдвигаем его только после успешной обработки апдейта), которого
// нет у go-telegram/bot.
//
// Токен есть в URL каждого запроса и не должен попасть в логи: ошибки
// транспорта проходят через clean, цепочка ошибок при этом рвётся.
package tgapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"closerai/internal/telegram/app"
	"closerai/internal/telegram/domain"
)

const (
	DefaultServerURL = "https://api.telegram.org"
	requestTimeout   = 15 * time.Second
	maxResponseSize  = 8 << 20
)

type Client struct {
	serverURL string
	http      *http.Client // таймаут задаётся контекстом запроса
}

func NewClient(serverURL string) *Client {
	if serverURL == "" {
		serverURL = DefaultServerURL
	}
	return &Client{serverURL: strings.TrimRight(serverURL, "/"), http: &http.Client{}}
}

var _ app.API = (*Client)(nil)

// APIError — Telegram ответил ok=false.
type APIError struct {
	Code        int
	Description string
	RetryAfter  int // секунд, для 429
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram api: %d %s", e.Code, e.Description)
}

type response struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// call выполняет метод Bot API. timeout — предел на весь запрос.
func (c *Client) call(ctx context.Context, token, method string, params, result any, timeout time.Duration) error {
	body, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("telegram: %s: encode: %w", method, err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.serverURL+"/bot"+token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram: %s: %v", method, clean(err, token))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		return fmt.Errorf("%w: %s: %v", domain.ErrTelegramUnavailable, method, clean(err, token))
	}
	defer resp.Body.Close()

	var r response
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseSize)).Decode(&r); err != nil {
		return fmt.Errorf("%w: %s: HTTP %d, ответ не разобран: %v",
			domain.ErrTelegramUnavailable, method, resp.StatusCode, clean(err, token))
	}
	if !r.OK {
		code := r.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		return &APIError{Code: code, Description: r.Description, RetryAfter: r.Parameters.RetryAfter}
	}
	if result != nil {
		if err := json.Unmarshal(r.Result, result); err != nil {
			return fmt.Errorf("telegram: %s: decode result: %w", method, err)
		}
	}
	return nil
}

// ---------- отправка ----------

func (c *Client) SendMessage(ctx context.Context, token string, chatID int64, text string) (int64, error) {
	var msg struct {
		ID int64 `json:"message_id"`
	}
	err := c.call(ctx, token, "sendMessage", map[string]any{
		"chat_id":              chatID,
		"text":                 text,
		"link_preview_options": map[string]any{"is_disabled": true},
	}, &msg, requestTimeout)
	if err != nil {
		return 0, classify(err)
	}
	return msg.ID, nil
}

func (c *Client) SendTyping(ctx context.Context, token string, chatID int64) error {
	err := c.call(ctx, token, "sendChatAction", map[string]any{"chat_id": chatID, "action": "typing"}, nil, requestTimeout)
	return classify(err)
}

// classify переводит ответ Telegram в доменные ошибки отправки.
func classify(err error) error {
	var ae *APIError
	if !errors.As(err, &ae) {
		return err // nil, сеть (уже ErrTelegramUnavailable) или отмена
	}
	desc := strings.ToLower(ae.Description)
	switch {
	case ae.Code == http.StatusForbidden, // бот заблокирован, клиент удалён
		ae.Code == http.StatusBadRequest && strings.Contains(desc, "chat not found"):
		return fmt.Errorf("%w: %v", domain.ErrChatUnavailable, ae)
	case ae.Code == http.StatusUnauthorized, ae.Code == http.StatusNotFound: // токен отозван
		return fmt.Errorf("%w: %v", domain.ErrBotUnavailable, ae)
	case ae.Code == http.StatusTooManyRequests, ae.Code >= 500:
		return fmt.Errorf("%w: %v", domain.ErrTelegramUnavailable, ae)
	}
	return ae
}

// ---------- приём (polling) ----------

// GetUpdates — long polling: ждёт апдейты до wait. Апдейты, которые не
// удалось разобрать, возвращаются только с ID (их пропустят, сдвинув offset).
func (c *Client) GetUpdates(ctx context.Context, token string, offset int64, wait time.Duration) ([]domain.Update, error) {
	var raw []json.RawMessage
	err := c.call(ctx, token, "getUpdates", map[string]any{
		"offset":          offset,
		"timeout":         int(wait.Seconds()),
		"allowed_updates": []string{"message"},
	}, &raw, wait+requestTimeout)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Update, 0, len(raw))
	for _, r := range raw {
		var u models.Update
		if err := json.Unmarshal(r, &u); err != nil {
			var head struct {
				ID int64 `json:"update_id"`
			}
			_ = json.Unmarshal(r, &head)
			u = models.Update{ID: head.ID}
		}
		out = append(out, ToDomain(&u))
	}
	return out, nil
}

// DeleteWebhook снимает вебхук: пока он стоит, getUpdates отвечает 409.
// Накопившиеся апдейты не сбрасываются.
func (c *Client) DeleteWebhook(ctx context.Context, token string) error {
	return c.call(ctx, token, "deleteWebhook", map[string]any{}, nil, requestTimeout)
}

// ToDomain переводит апдейт Bot API в доменный.
func ToDomain(u *models.Update) domain.Update {
	out := domain.Update{ID: u.ID}
	m := u.Message
	if m == nil {
		return out
	}
	text := m.Text
	if text == "" {
		text = m.Caption
	}
	dm := &domain.Message{
		ID:       int64(m.ID),
		ChatID:   m.Chat.ID,
		ChatType: domain.ChatType(m.Chat.Type),
		Text:     text,
		Date:     time.Unix(int64(m.Date), 0),
	}
	if m.From != nil {
		dm.From = &domain.User{
			ID: m.From.ID, IsBot: m.From.IsBot, Username: m.From.Username,
			FirstName: m.From.FirstName, LastName: m.From.LastName,
		}
	}
	out.Message = dm
	return out
}

// clean убирает токен из текста ошибки и рвёт цепочку ошибок.
func clean(err error, token string) error {
	if err == nil {
		return nil
	}
	return errors.New(strings.ReplaceAll(err.Error(), token, "<token>"))
}

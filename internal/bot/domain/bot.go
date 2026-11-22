// Package domain — доменная модель контекста bot: подключённый Telegram-бот
// компании и его настройки. Не зависит от HTTP, БД и Telegram API.
package domain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrNotFound            = errors.New("bot: not found")
	ErrInvalidToken        = errors.New("bot: invalid token format")
	ErrTokenRejected       = errors.New("bot: token rejected by telegram")
	ErrTokenMismatch       = errors.New("bot: token belongs to another bot")
	ErrAlreadyConnected    = errors.New("bot: already connected")
	ErrTelegramUnavailable = errors.New("bot: telegram unavailable")
	ErrPromptTooLong       = errors.New("bot: system prompt too long")
	ErrGreetingTooLong     = errors.New("bot: greeting too long")
	ErrInvalidThreshold    = errors.New("bot: handoff threshold out of range")
)

// WebhookError — Telegram отказался ставить вебхук (например, PUBLIC_URL не HTTPS).
type WebhookError struct{ Reason string }

func (e *WebhookError) Error() string { return "bot: telegram rejected webhook: " + e.Reason }

const (
	MaxSystemPromptLen      = 4000 // символов
	MaxGreetingLen          = 1000
	DefaultHandoffThreshold = 0.5
)

// Bot — корень агрегата. Токен в агрегат не входит: это секрет, он хранится
// зашифрованным и достаётся только через Credentials.
type Bot struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	TgBotID       int64
	Username      string
	WebhookSecret string
	Settings      Settings
	IsActive      bool
	CreatedAt     time.Time
}

// Settings — то, что владелец настраивает в панели.
type Settings struct {
	SystemPrompt string
	Greeting     string // первое сообщение клиенту на /start
	// HandoffThreshold — минимальная близость найденного контекста (0..1);
	// ниже неё диалог передаётся оператору.
	HandoffThreshold float32
}

func DefaultSettings() Settings { return Settings{HandoffThreshold: DefaultHandoffThreshold} }

// SettingsPatch — частичное изменение настроек: nil — поле не меняется.
type SettingsPatch struct {
	SystemPrompt     *string
	Greeting         *string
	HandoffThreshold *float32
}

// Apply возвращает настройки с применёнными изменениями и проверяет инварианты.
func (s Settings) Apply(p SettingsPatch) (Settings, error) {
	if p.SystemPrompt != nil {
		s.SystemPrompt = strings.TrimSpace(*p.SystemPrompt)
	}
	if p.Greeting != nil {
		s.Greeting = strings.TrimSpace(*p.Greeting)
	}
	if p.HandoffThreshold != nil {
		s.HandoffThreshold = *p.HandoffThreshold
	}
	return s, s.Validate()
}

func (s Settings) Validate() error {
	if utf8.RuneCountInString(s.SystemPrompt) > MaxSystemPromptLen {
		return ErrPromptTooLong
	}
	if utf8.RuneCountInString(s.Greeting) > MaxGreetingLen {
		return ErrGreetingTooLong
	}
	if !(s.HandoffThreshold >= 0 && s.HandoffThreshold <= 1) { // ловит и NaN
		return ErrInvalidThreshold
	}
	return nil
}

// Формат токена от @BotFather: "<id бота>:<секрет>".
var tokenRe = regexp.MustCompile(`^[0-9]{5,20}:[A-Za-z0-9_-]{30,}$`)

// NormalizeToken убирает пробелы по краям и проверяет формат. Действителен ли
// токен, знает только Telegram (getMe).
func NormalizeToken(token string) (string, error) {
	token = strings.TrimSpace(token)
	if !tokenRe.MatchString(token) {
		return "", ErrInvalidToken
	}
	return token, nil
}

// NewWebhookSecret — секрет для заголовка X-Telegram-Bot-Api-Secret-Token
// (Telegram допускает A-Z, a-z, 0-9, _ и -; до 256 символов).
func NewWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("bot: webhook secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// TelegramIdentity — что Telegram сообщает о боте по токену (getMe).
type TelegramIdentity struct {
	ID       int64
	Username string
}

// Credentials — всё, что нужно контексту telegram для работы с ботом.
// Содержит расшифрованный токен: не логировать и не отдавать в API.
type Credentials struct {
	BotID         uuid.UUID
	TenantID      uuid.UUID
	Token         string
	WebhookSecret string
	IsActive      bool
}

// Repository — порт хранения. Методы, принимающие tenantID, видят только
// ботов этой компании. Работают в транзакции из ctx, если она открыта.
type Repository interface {
	// Create сохраняет бота с зашифрованным токеном. ErrAlreadyConnected,
	// если бот с таким TgBotID уже подключён (в любой компании).
	Create(ctx context.Context, b Bot, token string) (Bot, error)
	List(ctx context.Context, tenantID uuid.UUID) ([]Bot, error)
	Get(ctx context.Context, tenantID, id uuid.UUID) (Bot, error)
	// GetForUpdate — как Get, но блокирует строку до конца транзакции.
	GetForUpdate(ctx context.Context, tenantID, id uuid.UUID) (Bot, error)
	// Update сохраняет username, настройки и IsActive.
	Update(ctx context.Context, b Bot) (Bot, error)
	UpdateToken(ctx context.Context, tenantID, id uuid.UUID, token string) error
	Delete(ctx context.Context, tenantID, id uuid.UUID) error

	// Системные методы (без tenantID) — для контекста telegram, который
	// узнаёт бота по id из URL вебхука, а не по пользователю.
	Credentials(ctx context.Context, id uuid.UUID) (Credentials, error)
	ActiveCredentials(ctx context.Context) ([]Credentials, error)
}

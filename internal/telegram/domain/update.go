// Package domain — доменная модель контекста telegram: апдейты Telegram
// в нашем виде и правила, какие из них считаются сообщениями клиента.
// Типы Bot API (go-telegram/bot/models) сюда не проникают: их переводит infra.
package domain

import (
	"crypto/subtle"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrBotNotFound = errors.New("telegram: bot not found")
	// ErrBadSecret — запрос на вебхук пришёл не от Telegram (секрет не совпал).
	ErrBadSecret = errors.New("telegram: bad webhook secret")
	// ErrBotUnavailable — бот выключен, удалён или его токен отозван.
	ErrBotUnavailable = errors.New("telegram: bot unavailable")
	// ErrChatUnavailable — писать в чат нельзя (клиент заблокировал бота и т.п.).
	ErrChatUnavailable = errors.New("telegram: chat unavailable")
	// ErrTelegramUnavailable — Telegram не ответил или попросил подождать; можно повторить.
	ErrTelegramUnavailable = errors.New("telegram: unavailable")
)

// Credentials — токен и секрет вебхука бота (из контекста bot).
// Содержит расшифрованный токен: не логировать.
type Credentials struct {
	BotID         uuid.UUID
	TenantID      uuid.UUID
	Token         string
	WebhookSecret string
	IsActive      bool
}

// SecretMatches сравнивает секрет вебхука за постоянное время.
func (c Credentials) SecretMatches(got string) bool {
	return c.WebhookSecret != "" &&
		subtle.ConstantTimeCompare([]byte(c.WebhookSecret), []byte(got)) == 1
}

// Update — апдейт Telegram. Нас интересуют только сообщения.
type Update struct {
	ID      int64
	Message *Message
}

type ChatType string

const ChatPrivate ChatType = "private"

type Message struct {
	ID       int64
	ChatID   int64
	ChatType ChatType
	From     *User
	Text     string // текст или подпись к медиа
	Date     time.Time
}

type User struct {
	ID        int64
	IsBot     bool
	Username  string
	FirstName string
	LastName  string
}

// CustomerMessage возвращает сообщение клиента, если апдейт — текстовое
// сообщение от человека в личном чате с ботом. Группы, каналы, сообщения
// без текста (стикеры, фото без подписи) пока не обрабатываются.
func (u Update) CustomerMessage() (Message, bool) {
	m := u.Message
	if m == nil || m.ChatType != ChatPrivate || m.From == nil || m.From.IsBot {
		return Message{}, false
	}
	if strings.TrimSpace(m.Text) == "" {
		return Message{}, false
	}
	return *m, true
}

// Inbound — сообщение клиента, адресованное боту компании.
type Inbound struct {
	TenantID uuid.UUID
	BotID    uuid.UUID
	Message  Message
}

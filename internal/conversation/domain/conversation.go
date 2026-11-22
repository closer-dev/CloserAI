// Package domain — доменная модель контекста conversation: клиенты бота,
// диалоги и сообщения. Не зависит от HTTP, БД и Telegram.
package domain

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"closerai/internal/kernel"
)

var (
	ErrNotFound = errors.New("conversation: not found")
	// ErrChatUnavailable — написать клиенту нельзя и повтор не поможет
	// (клиент заблокировал бота, чат удалён).
	ErrChatUnavailable = errors.New("conversation: chat unavailable")
)

// MaxTextLen — предел длины сообщения в Telegram (символов).
const MaxTextLen = 4096

// StartCommand — первое сообщение клиента боту; в ответ уходит приветствие.
const StartCommand = "/start"

// DefaultGreeting — приветствие, если владелец не задал своё.
const DefaultGreeting = "Здравствуйте! Напишите ваш вопрос, и я постараюсь помочь."

// Customer — клиент компании в Telegram (общий для всех её ботов).
type Customer struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	TgUserID  int64
	Username  string
	FirstName string
	LastName  string
}

// Conversation — корень агрегата: переписка клиента с ботом компании.
// У пары (бот, клиент) одновременно не больше одного незакрытого диалога.
type Conversation struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	BotID          uuid.UUID
	CustomerID     uuid.UUID
	ChatID         int64 // куда отправлять ответы (личный чат = tg id клиента)
	Status         kernel.ConversationStatus
	AssignedUserID uuid.NullUUID
	LastMessageAt  time.Time
	CreatedAt      time.Time
	ClosedAt       *time.Time
}

// BotReplies — отвечает ли в диалоге бот. Если диалог у оператора или ждёт
// его, бот молчит: сообщения клиента просто появляются в панели.
func (c Conversation) BotReplies() bool { return c.Status == kernel.StatusBot }

// Message — сообщение в диалоге.
type Message struct {
	ID             int64
	ConversationID uuid.UUID
	Sender         kernel.Sender
	UserID         uuid.NullUUID // оператор, если Sender == SenderOperator
	Text           string
	TgMessageID    int64 // 0 — сообщение не из Telegram / ещё не отправлено
	CreatedAt      time.Time
}

// IsStart — клиент открыл бота (/start, в том числе с параметром deep link).
func (m Message) IsStart() bool {
	cmd, _, _ := strings.Cut(strings.TrimSpace(m.Text), " ")
	return cmd == StartCommand
}

// Greeting возвращает приветствие бота или стандартное, если оно не задано.
func Greeting(configured string) string {
	if g := strings.TrimSpace(configured); g != "" {
		return g
	}
	return DefaultGreeting
}

// Truncate обрезает текст до лимита Telegram, не разрывая символы.
func Truncate(text string) string {
	if utf8.RuneCountInString(text) <= MaxTextLen {
		return text
	}
	r := []rune(text)
	return string(r[:MaxTextLen-1]) + "…"
}

// Repository — порт хранения. Методы с tenantID видят только данные этой
// компании. Работают в транзакции из ctx, если она открыта.
type Repository interface {
	// UpsertCustomer создаёт клиента или обновляет его профиль.
	UpsertCustomer(ctx context.Context, c Customer) (Customer, error)
	// OpenConversation возвращает незакрытый диалог клиента с ботом, создавая
	// его при необходимости, и блокирует его до конца транзакции.
	OpenConversation(ctx context.Context, tenantID, botID, customerID uuid.UUID) (Conversation, error)
	GetConversation(ctx context.Context, tenantID, id uuid.UUID) (Conversation, error)
	// AddMessage сохраняет сообщение и сдвигает last_message_at диалога.
	// added == false — сообщение с таким TgMessageID в диалоге уже есть
	// (Telegram прислал апдейт повторно).
	AddMessage(ctx context.Context, tenantID uuid.UUID, m Message) (saved Message, added bool, err error)
	GetMessage(ctx context.Context, tenantID, conversationID uuid.UUID, id int64) (Message, error)
}

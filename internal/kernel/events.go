package kernel

import (
	"time"

	"github.com/google/uuid"
)

// Event — доменное событие, которое один контекст публикует, а другие слушают
// через platform/eventbus. События — опубликованный язык между контекстами,
// поэтому живут в kernel: импортировать domain чужого контекста нельзя.
//
// Правила для новых событий:
//   - структура-значение (не указатель), поля только из stdlib, uuid и kernel;
//   - EventName — постоянная строка вида "<контекст>.<что_случилось>";
//   - обязательно TenantID: по нему realtime-хаб решает, кому слать событие.
type Event interface {
	EventName() string
	EventTenant() uuid.UUID
}

// Sender — кто написал сообщение (messages.sender).
type Sender string

const (
	SenderCustomer Sender = "customer"
	SenderBot      Sender = "bot"
	SenderOperator Sender = "operator"
	SenderSystem   Sender = "system"
)

// ConversationStatus — статус диалога (conversations.status).
type ConversationStatus string

const (
	StatusBot             ConversationStatus = "bot"
	StatusWaitingOperator ConversationStatus = "waiting_operator"
	StatusOperator        ConversationStatus = "operator"
	StatusClosed          ConversationStatus = "closed"
)

// MessageCreated — в диалог добавлено сообщение (от клиента, бота, оператора
// или системное). Публикует conversation.
type MessageCreated struct {
	TenantID       uuid.UUID
	ConversationID uuid.UUID
	MessageID      int64
	Sender         Sender
	UserID         uuid.NullUUID // оператор, если Sender == SenderOperator
	Text           string
	CreatedAt      time.Time
}

func (MessageCreated) EventName() string        { return "conversation.message_created" }
func (e MessageCreated) EventTenant() uuid.UUID { return e.TenantID }

// ConversationStatusChanged — диалог перешёл из одного статуса в другой
// (бот → ждёт оператора → оператор → бот …). Публикует conversation.
type ConversationStatusChanged struct {
	TenantID       uuid.UUID
	ConversationID uuid.UUID
	From, To       ConversationStatus
	AssignedUserID uuid.NullUUID // кто взял диалог (для статуса operator)
	ChangedAt      time.Time
}

func (ConversationStatusChanged) EventName() string        { return "conversation.status_changed" }
func (e ConversationStatusChanged) EventTenant() uuid.UUID { return e.TenantID }

// ConversationClosed — диалог закрыт вручную или по неактивности.
// Публикует conversation, слушает verdict (ставит задачу на вердикт).
type ConversationClosed struct {
	TenantID       uuid.UUID
	ConversationID uuid.UUID
	ClosedBy       uuid.NullUUID // пусто — закрыт автоматически по таймауту
	ClosedAt       time.Time
}

func (ConversationClosed) EventName() string        { return "conversation.closed" }
func (e ConversationClosed) EventTenant() uuid.UUID { return e.TenantID }

// BotActivated — бот подключён, включён или ему сменили токен. Публикует bot,
// слушает telegram (запускает или перезапускает приём апдейтов в режиме polling).
type BotActivated struct {
	TenantID uuid.UUID
	BotID    uuid.UUID
}

func (BotActivated) EventName() string        { return "bot.activated" }
func (e BotActivated) EventTenant() uuid.UUID { return e.TenantID }

// BotDeactivated — бот выключен или удалён. Публикует bot, слушает telegram
// (останавливает приём апдейтов).
type BotDeactivated struct {
	TenantID uuid.UUID
	BotID    uuid.UUID
}

func (BotDeactivated) EventName() string        { return "bot.deactivated" }
func (e BotDeactivated) EventTenant() uuid.UUID { return e.TenantID }

// Package app — прикладной слой контекста conversation: приём сообщений
// клиентов и ответы бота. Публичный API контекста.
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"closerai/internal/conversation/domain"
	"closerai/internal/kernel"
)

// ErrPermanent помечает ошибки, при которых повтор задачи не поможет или
// навредит (например, ответ уже ушёл клиенту). Воркер не ретраит такие задачи.
var ErrPermanent = errors.New("conversation: permanent failure")

// ---------- порты ----------

// TxManager — unit of work (реализация: platform/db.TxManager).
type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Events публикует доменные события (реализация: platform/eventbus.Bus).
type Events interface {
	Publish(ctx context.Context, events ...kernel.Event)
}

// ReplyTask — ответить клиенту на сообщение MessageID.
type ReplyTask struct {
	TenantID       uuid.UUID
	ConversationID uuid.UUID
	MessageID      int64
}

// ReplyScheduler ставит ответ бота в очередь задач. Внутри транзакции задача
// ставится в неё же: сообщение и задача на ответ появятся вместе.
type ReplyScheduler interface {
	ScheduleReply(ctx context.Context, t ReplyTask) error
}

// AnswerRequest — вопрос клиента, на который нужен ответ бота.
type AnswerRequest struct {
	TenantID       uuid.UUID
	BotID          uuid.UUID
	ConversationID uuid.UUID
	Question       string
}

// AnswerProvider формирует ответ бота (rag; до этапа 4 — эхо).
type AnswerProvider interface {
	Answer(ctx context.Context, r AnswerRequest) (string, error)
}

// BotProfile — настройки бота, которые нужны диалогу (контекст bot).
type BotProfile interface {
	// Greeting — приветствие на /start; пусто — не задано.
	Greeting(ctx context.Context, tenantID, botID uuid.UUID) (string, error)
}

// Sender отправляет сообщения клиенту в Telegram от имени бота.
type Sender interface {
	// Typing показывает «печатает…»; ошибки адаптер глотает сам.
	Typing(ctx context.Context, botID uuid.UUID, chatID int64)
	// Send возвращает id отправленного сообщения в Telegram.
	// domain.ErrChatUnavailable — клиенту писать нельзя (заблокировал бота).
	Send(ctx context.Context, botID uuid.UUID, chatID int64, text string) (int64, error)
}

type Deps struct {
	Repo    domain.Repository
	Tx      TxManager
	Events  Events
	Replies ReplyScheduler
	Answers AnswerProvider
	Bots    BotProfile
	Sender  Sender
	Now     func() time.Time // nil — time.Now
}

type Service struct {
	repo    domain.Repository
	tx      TxManager
	events  Events
	replies ReplyScheduler
	answers AnswerProvider
	bots    BotProfile
	sender  Sender
	now     func() time.Time
}

func NewService(d Deps) (*Service, error) {
	if d.Repo == nil || d.Tx == nil || d.Events == nil || d.Replies == nil ||
		d.Answers == nil || d.Bots == nil || d.Sender == nil {
		return nil, errors.New("conversation: не все зависимости переданы")
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{
		repo: d.Repo, tx: d.Tx, events: d.Events, replies: d.Replies,
		answers: d.Answers, bots: d.Bots, sender: d.Sender, now: d.Now,
	}, nil
}

// ---------- входящее сообщение ----------

// CustomerProfile — кто написал (данные профиля Telegram).
type CustomerProfile struct {
	TgUserID  int64
	Username  string
	FirstName string
	LastName  string
}

// Incoming — текстовое сообщение клиента боту (в личном чате).
type Incoming struct {
	TenantID    uuid.UUID
	BotID       uuid.UUID
	Customer    CustomerProfile
	Text        string
	TgMessageID int64
	SentAt      time.Time // zero — сейчас
}

// HandleIncoming сохраняет сообщение клиента в его открытый диалог (создавая
// клиента и диалог при необходимости) и, если диалог ведёт бот, ставит задачу
// на ответ. Повтор того же апдейта Telegram ничего не делает.
func (s *Service) HandleIncoming(ctx context.Context, in Incoming) error {
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return nil
	}
	sentAt := in.SentAt
	if sentAt.IsZero() {
		sentAt = s.now()
	}

	return s.tx.Do(ctx, func(ctx context.Context) error {
		cust, err := s.repo.UpsertCustomer(ctx, domain.Customer{
			TenantID:  in.TenantID,
			TgUserID:  in.Customer.TgUserID,
			Username:  in.Customer.Username,
			FirstName: in.Customer.FirstName,
			LastName:  in.Customer.LastName,
		})
		if err != nil {
			return err
		}
		conv, err := s.repo.OpenConversation(ctx, in.TenantID, in.BotID, cust.ID)
		if err != nil {
			return err
		}
		msg, added, err := s.repo.AddMessage(ctx, in.TenantID, domain.Message{
			ConversationID: conv.ID,
			Sender:         kernel.SenderCustomer,
			Text:           text,
			TgMessageID:    in.TgMessageID,
			CreatedAt:      sentAt,
		})
		if err != nil || !added {
			return err
		}
		s.events.Publish(ctx, messageCreated(in.TenantID, msg))
		if !conv.BotReplies() {
			return nil
		}
		return s.replies.ScheduleReply(ctx, ReplyTask{
			TenantID: in.TenantID, ConversationID: conv.ID, MessageID: msg.ID,
		})
	})
}

// ---------- ответ бота (из фоновой задачи) ----------

// Reply отвечает клиенту на сообщение: на /start — приветствием, иначе —
// ответом AnswerProvider. Если диалог за это время забрал оператор, бот молчит.
// Ошибки с ErrPermanent повторять нельзя, остальные — можно.
func (s *Service) Reply(ctx context.Context, t ReplyTask) error {
	conv, err := s.repo.GetConversation(ctx, t.TenantID, t.ConversationID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil // диалог удалён вместе с ботом
	}
	if err != nil {
		return err
	}
	if !conv.BotReplies() {
		return nil
	}
	question, err := s.repo.GetMessage(ctx, t.TenantID, t.ConversationID, t.MessageID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	var answer string
	if question.IsStart() {
		g, err := s.bots.Greeting(ctx, conv.TenantID, conv.BotID)
		if err != nil {
			return err
		}
		answer = domain.Greeting(g)
	} else {
		s.sender.Typing(ctx, conv.BotID, conv.ChatID)
		if answer, err = s.answers.Answer(ctx, AnswerRequest{
			TenantID: conv.TenantID, BotID: conv.BotID, ConversationID: conv.ID, Question: question.Text,
		}); err != nil {
			return err
		}
	}
	answer = domain.Truncate(strings.TrimSpace(answer))
	if answer == "" {
		return nil
	}

	tgID, err := s.sender.Send(ctx, conv.BotID, conv.ChatID, answer)
	if errors.Is(err, domain.ErrChatUnavailable) {
		return fmt.Errorf("%w: %w", ErrPermanent, err)
	}
	if err != nil {
		return err
	}

	err = s.tx.Do(ctx, func(ctx context.Context) error {
		msg, _, err := s.repo.AddMessage(ctx, conv.TenantID, domain.Message{
			ConversationID: conv.ID,
			Sender:         kernel.SenderBot,
			Text:           answer,
			TgMessageID:    tgID,
			CreatedAt:      s.now(),
		})
		if err != nil {
			return err
		}
		s.events.Publish(ctx, messageCreated(conv.TenantID, msg))
		return nil
	})
	if err != nil {
		// Клиент ответ уже получил: повтор задачи отправил бы его второй раз.
		return fmt.Errorf("%w: ответ отправлен, но не сохранён: %w", ErrPermanent, err)
	}
	return nil
}

func messageCreated(tenantID uuid.UUID, m domain.Message) kernel.MessageCreated {
	return kernel.MessageCreated{
		TenantID:       tenantID,
		ConversationID: m.ConversationID,
		MessageID:      m.ID,
		Sender:         m.Sender,
		UserID:         m.UserID,
		Text:           m.Text,
		CreatedAt:      m.CreatedAt,
	}
}

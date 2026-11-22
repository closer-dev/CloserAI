package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"closerai/internal/conversation/domain"
	"closerai/internal/kernel"
)

// ---------- фейки портов ----------

type memRepo struct {
	customers map[int64]domain.Customer
	convs     map[uuid.UUID]domain.Conversation
	messages  []domain.Message
	failAdd   error
}

func newRepo() *memRepo {
	return &memRepo{customers: map[int64]domain.Customer{}, convs: map[uuid.UUID]domain.Conversation{}}
}

func (r *memRepo) UpsertCustomer(_ context.Context, c domain.Customer) (domain.Customer, error) {
	if old, ok := r.customers[c.TgUserID]; ok {
		c.ID = old.ID
	} else {
		c.ID = uuid.New()
	}
	r.customers[c.TgUserID] = c
	return c, nil
}

func (r *memRepo) OpenConversation(_ context.Context, tenantID, botID, customerID uuid.UUID) (domain.Conversation, error) {
	for _, c := range r.convs {
		if c.BotID == botID && c.CustomerID == customerID && c.Status != kernel.StatusClosed {
			return c, nil
		}
	}
	var chat int64
	for _, cu := range r.customers {
		if cu.ID == customerID {
			chat = cu.TgUserID
		}
	}
	c := domain.Conversation{
		ID: uuid.New(), TenantID: tenantID, BotID: botID, CustomerID: customerID,
		ChatID: chat, Status: kernel.StatusBot,
	}
	r.convs[c.ID] = c
	return c, nil
}

func (r *memRepo) GetConversation(_ context.Context, tenantID, id uuid.UUID) (domain.Conversation, error) {
	c, ok := r.convs[id]
	if !ok || c.TenantID != tenantID {
		return domain.Conversation{}, domain.ErrNotFound
	}
	return c, nil
}

func (r *memRepo) AddMessage(_ context.Context, _ uuid.UUID, m domain.Message) (domain.Message, bool, error) {
	if r.failAdd != nil {
		return domain.Message{}, false, r.failAdd
	}
	for _, x := range r.messages {
		if m.TgMessageID != 0 && x.ConversationID == m.ConversationID && x.TgMessageID == m.TgMessageID {
			return domain.Message{}, false, nil
		}
	}
	m.ID = int64(len(r.messages) + 1)
	r.messages = append(r.messages, m)
	return m, true, nil
}

func (r *memRepo) GetMessage(_ context.Context, _, convID uuid.UUID, id int64) (domain.Message, error) {
	for _, m := range r.messages {
		if m.ID == id && m.ConversationID == convID {
			return m, nil
		}
	}
	return domain.Message{}, domain.ErrNotFound
}

type fakeTx struct{}

func (fakeTx) Do(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }

type fakeEvents struct{ got []kernel.Event }

func (e *fakeEvents) Publish(_ context.Context, ev ...kernel.Event) { e.got = append(e.got, ev...) }

type fakeReplies struct{ tasks []ReplyTask }

func (f *fakeReplies) ScheduleReply(_ context.Context, t ReplyTask) error {
	f.tasks = append(f.tasks, t)
	return nil
}

type fakeAnswers struct {
	asked []AnswerRequest
	err   error
}

func (f *fakeAnswers) Answer(_ context.Context, r AnswerRequest) (string, error) {
	f.asked = append(f.asked, r)
	return "ответ на: " + r.Question, f.err
}

type fakeBots struct{ greeting string }

func (f fakeBots) Greeting(context.Context, uuid.UUID, uuid.UUID) (string, error) {
	return f.greeting, nil
}

type sent struct {
	bot  uuid.UUID
	chat int64
	text string
}

type fakeSender struct {
	sent   []sent
	typing int
	err    error
}

func (f *fakeSender) Typing(context.Context, uuid.UUID, int64) { f.typing++ }
func (f *fakeSender) Send(_ context.Context, bot uuid.UUID, chat int64, text string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.sent = append(f.sent, sent{bot, chat, text})
	return int64(1000 + len(f.sent)), nil
}

type env struct {
	svc     *Service
	repo    *memRepo
	events  *fakeEvents
	replies *fakeReplies
	answers *fakeAnswers
	sender  *fakeSender
	tenant  uuid.UUID
	bot     uuid.UUID
}

func setup(t *testing.T, greeting string) *env {
	t.Helper()
	e := &env{
		repo: newRepo(), events: &fakeEvents{}, replies: &fakeReplies{},
		answers: &fakeAnswers{}, sender: &fakeSender{}, tenant: uuid.New(), bot: uuid.New(),
	}
	svc, err := NewService(Deps{
		Repo: e.repo, Tx: fakeTx{}, Events: e.events, Replies: e.replies,
		Answers: e.answers, Bots: fakeBots{greeting: greeting}, Sender: e.sender,
		Now: func() time.Time { return time.Unix(1_700_000_000, 0) },
	})
	require.NoError(t, err)
	e.svc = svc
	return e
}

func (e *env) incoming(text string, tgMsgID int64) Incoming {
	return Incoming{
		TenantID: e.tenant, BotID: e.bot, Text: text, TgMessageID: tgMsgID,
		Customer: CustomerProfile{TgUserID: 42, Username: "ivan", FirstName: "Иван"},
	}
}

// ---------- тесты ----------

func TestHandleIncomingSavesAndSchedulesReply(t *testing.T) {
	e := setup(t, "")
	ctx := context.Background()

	require.NoError(t, e.svc.HandleIncoming(ctx, e.incoming("  Где мой заказ?  ", 1)))

	require.Len(t, e.repo.messages, 1)
	m := e.repo.messages[0]
	require.Equal(t, kernel.SenderCustomer, m.Sender)
	require.Equal(t, "Где мой заказ?", m.Text)
	require.Equal(t, int64(1), m.TgMessageID)

	require.Len(t, e.events.got, 1)
	ev := e.events.got[0].(kernel.MessageCreated)
	require.Equal(t, e.tenant, ev.TenantID)
	require.Equal(t, m.ID, ev.MessageID)

	require.Equal(t, []ReplyTask{{TenantID: e.tenant, ConversationID: m.ConversationID, MessageID: m.ID}}, e.replies.tasks)

	// второе сообщение идёт в тот же диалог
	require.NoError(t, e.svc.HandleIncoming(ctx, e.incoming("алло", 2)))
	require.Len(t, e.repo.convs, 1)
	require.Len(t, e.repo.customers, 1)
}

func TestHandleIncomingDuplicateIgnored(t *testing.T) {
	e := setup(t, "")
	ctx := context.Background()
	require.NoError(t, e.svc.HandleIncoming(ctx, e.incoming("вопрос", 7)))
	require.NoError(t, e.svc.HandleIncoming(ctx, e.incoming("вопрос", 7)))

	require.Len(t, e.repo.messages, 1)
	require.Len(t, e.events.got, 1)
	require.Len(t, e.replies.tasks, 1)
}

func TestHandleIncomingEmptyIgnored(t *testing.T) {
	e := setup(t, "")
	require.NoError(t, e.svc.HandleIncoming(context.Background(), e.incoming("   ", 1)))
	require.Empty(t, e.repo.messages)
}

func TestHandleIncomingOperatorConversationNoReply(t *testing.T) {
	e := setup(t, "")
	ctx := context.Background()
	require.NoError(t, e.svc.HandleIncoming(ctx, e.incoming("первое", 1)))
	for id, c := range e.repo.convs {
		c.Status = kernel.StatusOperator
		e.repo.convs[id] = c
	}

	require.NoError(t, e.svc.HandleIncoming(ctx, e.incoming("второе", 2)))
	require.Len(t, e.repo.messages, 2)
	require.Len(t, e.events.got, 2, "оператор должен увидеть сообщение")
	require.Len(t, e.replies.tasks, 1, "бот не отвечает, пока диалог у оператора")
}

func (e *env) handleAndTask(t *testing.T, text string) ReplyTask {
	t.Helper()
	require.NoError(t, e.svc.HandleIncoming(context.Background(), e.incoming(text, int64(len(e.repo.messages)+1))))
	return e.replies.tasks[len(e.replies.tasks)-1]
}

func TestReplyAnswersAndSaves(t *testing.T) {
	e := setup(t, "")
	task := e.handleAndTask(t, "Сколько стоит доставка?")

	require.NoError(t, e.svc.Reply(context.Background(), task))

	require.Equal(t, []sent{{e.bot, 42, "ответ на: Сколько стоит доставка?"}}, e.sender.sent)
	require.Equal(t, 1, e.sender.typing)
	require.Len(t, e.answers.asked, 1)
	require.Equal(t, task.ConversationID, e.answers.asked[0].ConversationID)

	require.Len(t, e.repo.messages, 2)
	bot := e.repo.messages[1]
	require.Equal(t, kernel.SenderBot, bot.Sender)
	require.Equal(t, int64(1001), bot.TgMessageID)
	require.Equal(t, kernel.SenderBot, e.events.got[1].(kernel.MessageCreated).Sender)
}

func TestReplyStartSendsGreeting(t *testing.T) {
	e := setup(t, "Добро пожаловать в магазин!")
	require.NoError(t, e.svc.Reply(context.Background(), e.handleAndTask(t, "/start")))
	require.Equal(t, "Добро пожаловать в магазин!", e.sender.sent[0].text)
	require.Empty(t, e.answers.asked, "на /start модель не вызывается")

	e = setup(t, "")
	require.NoError(t, e.svc.Reply(context.Background(), e.handleAndTask(t, "/start")))
	require.Equal(t, domain.DefaultGreeting, e.sender.sent[0].text)
}

func TestReplySkippedWhenOperatorTookOver(t *testing.T) {
	e := setup(t, "")
	task := e.handleAndTask(t, "вопрос")
	c := e.repo.convs[task.ConversationID]
	c.Status = kernel.StatusOperator
	e.repo.convs[task.ConversationID] = c

	require.NoError(t, e.svc.Reply(context.Background(), task))
	require.Empty(t, e.sender.sent)
	require.Empty(t, e.answers.asked)
}

func TestReplyConversationGone(t *testing.T) {
	e := setup(t, "")
	task := e.handleAndTask(t, "вопрос")
	delete(e.repo.convs, task.ConversationID)
	require.NoError(t, e.svc.Reply(context.Background(), task))
	require.Empty(t, e.sender.sent)
}

func TestReplyErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("модель недоступна — повторить", func(t *testing.T) {
		e := setup(t, "")
		e.answers.err = errors.New("ollama down")
		err := e.svc.Reply(ctx, e.handleAndTask(t, "вопрос"))
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrPermanent)
		require.Empty(t, e.sender.sent)
	})

	t.Run("клиент заблокировал бота — не повторять", func(t *testing.T) {
		e := setup(t, "")
		e.sender.err = domain.ErrChatUnavailable
		err := e.svc.Reply(ctx, e.handleAndTask(t, "вопрос"))
		require.ErrorIs(t, err, ErrPermanent)
	})

	t.Run("сеть — повторить", func(t *testing.T) {
		e := setup(t, "")
		e.sender.err = errors.New("timeout")
		err := e.svc.Reply(ctx, e.handleAndTask(t, "вопрос"))
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrPermanent)
	})

	t.Run("отправлено, но не сохранено — не повторять", func(t *testing.T) {
		e := setup(t, "")
		task := e.handleAndTask(t, "вопрос")
		e.repo.failAdd = errors.New("db down")
		err := e.svc.Reply(ctx, task)
		require.ErrorIs(t, err, ErrPermanent)
		require.Len(t, e.sender.sent, 1)
	})
}

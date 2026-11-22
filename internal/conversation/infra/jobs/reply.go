// Package jobs — фоновые задачи контекста conversation (river): ответ бота
// клиенту. Ответ строится долго (LLM), поэтому не делается в обработчике
// апдейта Telegram, а ставится задачей в той же транзакции, что и сообщение.
package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"closerai/internal/conversation/app"
	"closerai/internal/platform/jobs"
)

// ReplyArgs — аргументы задачи «ответить клиенту».
type ReplyArgs struct {
	TenantID       uuid.UUID `json:"tenant_id"`
	ConversationID uuid.UUID `json:"conversation_id"`
	MessageID      int64     `json:"message_id"`
}

func (ReplyArgs) Kind() string { return "conversation.reply" }

// Ответ клиенту устаревает быстро: несколько коротких попыток (бэкофф river
// растёт как попытка⁴ секунд), а не сутки ретраев.
func (ReplyArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 4} }

// ReplyWorker вызывает сценарий app.Service.Reply.
type ReplyWorker struct {
	river.WorkerDefaults[ReplyArgs]
	Service *app.Service
}

// Timeout с запасом на генерацию ответа локальной моделью на CPU.
func (w *ReplyWorker) Timeout(*river.Job[ReplyArgs]) time.Duration { return 3 * time.Minute }

func (w *ReplyWorker) Work(ctx context.Context, job *river.Job[ReplyArgs]) error {
	err := w.Service.Reply(ctx, app.ReplyTask{
		TenantID:       job.Args.TenantID,
		ConversationID: job.Args.ConversationID,
		MessageID:      job.Args.MessageID,
	})
	if errors.Is(err, app.ErrPermanent) {
		return river.JobCancel(err)
	}
	return err
}

// ReplyScheduler — адаптер порта app.ReplyScheduler к очереди.
type ReplyScheduler struct{ Queue *jobs.Queue }

var _ app.ReplyScheduler = ReplyScheduler{}

func (s ReplyScheduler) ScheduleReply(ctx context.Context, t app.ReplyTask) error {
	return s.Queue.Enqueue(ctx, ReplyArgs{
		TenantID: t.TenantID, ConversationID: t.ConversationID, MessageID: t.MessageID,
	}, nil)
}

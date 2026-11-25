// Package eventbus — внутренняя шина доменных событий (kernel.Event):
// MessageCreated, ConversationStatusChanged, ConversationClosed и т.д.
// Через неё контексты реагируют друг на друга без прямых зависимостей.
//
// Семантика:
//   - Publish внутри TxManager.Do откладывает доставку до коммита; при откате
//     события не доставляются. Вне транзакции доставка сразу.
//   - Для каждого события обработчики вызываются синхронно в порядке подписки,
//     включая SubscribeAll. Разные публикации могут выполняться параллельно.
//     Долгую работу (LLM, Telegram) обработчик ставит в очередь platform/jobs.
//   - Ошибка или паника обработчика логируется и не мешает остальным:
//     данные уже закоммичены, откатывать публикацию поздно.
//
// Шина живёт в памяти процесса: это модульный монолит, один бинарник.
package eventbus

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"closerai/internal/kernel"
	"closerai/internal/platform/db"
)

// Handler обрабатывает любое событие (нужен, например, realtime-хабу,
// который пересылает всё подряд в браузер).
type Handler func(ctx context.Context, e kernel.Event) error

type subscription struct {
	name    string
	all     bool
	handler Handler
}

type Bus struct {
	log *slog.Logger

	mu   sync.RWMutex
	subs []subscription
}

func New(log *slog.Logger) *Bus {
	return &Bus{log: log}
}

// Subscribe подписывает типизированный обработчик на события типа E:
//
//	eventbus.Subscribe(bus, func(ctx context.Context, e kernel.ConversationClosed) error { … })
func Subscribe[E kernel.Event](b *Bus, h func(ctx context.Context, e E) error) {
	var zero E
	name := zero.EventName()
	b.subscribe(subscription{name: name, handler: func(ctx context.Context, e kernel.Event) error {
		typed, ok := e.(E)
		if !ok {
			return fmt.Errorf("eventbus: событие %q имеет тип %T, а подписчик ждёт %T", name, e, zero)
		}
		return h(ctx, typed)
	}})
}

// SubscribeAll подписывает обработчик на все события.
func (b *Bus) SubscribeAll(h Handler) {
	b.subscribe(subscription{all: true, handler: h})
}

func (b *Bus) subscribe(sub subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, sub)
}

// Publish доставляет события подписчикам: после коммита текущей транзакции
// или сразу, если транзакции в ctx нет. Метод совпадает с портом
// `Publish(ctx, ...kernel.Event)`, который объявляют слои app контекстов.
func (b *Bus) Publish(ctx context.Context, events ...kernel.Event) {
	if len(events) == 0 {
		return
	}
	// При отложенной доставке вызывающий код может переиспользовать исходный срез.
	events = slices.Clone(events)
	db.AfterCommit(ctx, func(ctx context.Context) {
		for _, e := range events {
			b.dispatch(ctx, e)
		}
	})
}

func (b *Bus) dispatch(ctx context.Context, e kernel.Event) {
	name := e.EventName()
	b.mu.RLock()
	var handlers []Handler
	for _, sub := range b.subs {
		if sub.all || sub.name == name {
			handlers = append(handlers, sub.handler)
		}
	}
	b.mu.RUnlock()

	for _, h := range handlers {
		b.call(ctx, h, e)
	}
}

func (b *Bus) call(ctx context.Context, h Handler, e kernel.Event) {
	defer func() {
		if r := recover(); r != nil {
			b.log.ErrorContext(ctx, "eventbus: паника в обработчике",
				"event", e.EventName(), "tenant_id", e.EventTenant(), "panic", r)
		}
	}()
	if err := h(ctx, e); err != nil {
		b.log.ErrorContext(ctx, "eventbus: ошибка обработчика",
			"event", e.EventName(), "tenant_id", e.EventTenant(), "err", err)
	}
}

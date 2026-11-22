// Package eventbus — внутренняя шина доменных событий (kernel.Event):
// MessageCreated, ConversationStatusChanged, ConversationClosed и т.д.
// Через неё контексты реагируют друг на друга без прямых зависимостей.
//
// Семантика:
//   - Publish внутри TxManager.Do откладывает доставку до коммита; при откате
//     события не доставляются. Вне транзакции доставка сразу.
//   - Обработчики вызываются синхронно, по очереди, в порядке подписки.
//     Поэтому они должны быть быстрыми: долгую работу (вызов LLM, отправку
//     в Telegram) обработчик ставит в очередь задач (platform/jobs).
//   - Ошибка или паника обработчика логируется и не мешает остальным:
//     данные уже закоммичены, откатывать публикацию поздно.
//
// Шина живёт в памяти процесса: это модульный монолит, один бинарник.
package eventbus

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"closerai/internal/kernel"
	"closerai/internal/platform/db"
)

// Handler обрабатывает любое событие (нужен, например, realtime-хабу,
// который пересылает всё подряд в браузер).
type Handler func(ctx context.Context, e kernel.Event) error

type Bus struct {
	log *slog.Logger

	mu     sync.RWMutex
	byName map[string][]Handler
	all    []Handler
}

func New(log *slog.Logger) *Bus {
	return &Bus{log: log, byName: map[string][]Handler{}}
}

// Subscribe подписывает типизированный обработчик на события типа E:
//
//	eventbus.Subscribe(bus, func(ctx context.Context, e kernel.ConversationClosed) error { … })
func Subscribe[E kernel.Event](b *Bus, h func(ctx context.Context, e E) error) {
	var zero E
	name := zero.EventName()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.byName[name] = append(b.byName[name], func(ctx context.Context, e kernel.Event) error {
		typed, ok := e.(E)
		if !ok {
			return fmt.Errorf("eventbus: событие %q имеет тип %T, а подписчик ждёт %T", name, e, zero)
		}
		return h(ctx, typed)
	})
}

// SubscribeAll подписывает обработчик на все события.
func (b *Bus) SubscribeAll(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.all = append(b.all, h)
}

// Publish доставляет события подписчикам: после коммита текущей транзакции
// или сразу, если транзакции в ctx нет. Метод совпадает с портом
// `Publish(ctx, ...kernel.Event)`, который объявляют слои app контекстов.
func (b *Bus) Publish(ctx context.Context, events ...kernel.Event) {
	if len(events) == 0 {
		return
	}
	db.AfterCommit(ctx, func(ctx context.Context) {
		for _, e := range events {
			b.dispatch(ctx, e)
		}
	})
}

func (b *Bus) dispatch(ctx context.Context, e kernel.Event) {
	b.mu.RLock()
	handlers := make([]Handler, 0, len(b.byName[e.EventName()])+len(b.all))
	handlers = append(handlers, b.byName[e.EventName()]...)
	handlers = append(handlers, b.all...)
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

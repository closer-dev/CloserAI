// Package polling — режим TG_MODE=polling: приложение само забирает апдейты
// у Telegram (getUpdates) для каждого включённого бота. Нужен для локальной
// разработки без HTTPS.
//
// Offset сдвигается только после успешной обработки апдейта, поэтому при
// сбое (например, недоступна БД) апдейт будет получен снова. Дубли отсекает
// conversation (уникальность id сообщения в диалоге).
//
// Telegram разрешает только одного получателя апдейтов на токен: если тот же
// бот опрашивается в другом процессе (второй экземпляр приложения, контейнер
// app рядом с `make dev-api`), getUpdates отвечает 409 — это видно в логе.
package polling

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"closerai/internal/telegram/app"
	"closerai/internal/telegram/domain"
	"closerai/internal/telegram/infra/tgapi"
)

const (
	longPollWait = 25 * time.Second
	maxBackoff   = 30 * time.Second
	// Апдейт, который не удаётся обработать столько раз подряд, пропускается,
	// чтобы один «ядовитый» апдейт не остановил бота навсегда.
	maxUpdateAttempts = 5
)

type API interface {
	GetUpdates(ctx context.Context, token string, offset int64, wait time.Duration) ([]domain.Update, error)
	DeleteWebhook(ctx context.Context, token string) error
}

// Manager держит по одному опрашивающему циклу на включённого бота.
type Manager struct {
	svc *app.Service
	api API
	log *slog.Logger

	mu      sync.Mutex
	base    context.Context // nil — Start ещё не вызван или уже Stop
	cancel  context.CancelFunc
	runners map[uuid.UUID]*runner
	wg      sync.WaitGroup
}

type runner struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func NewManager(svc *app.Service, api API, log *slog.Logger) *Manager {
	return &Manager{svc: svc, api: api, log: log, runners: map[uuid.UUID]*runner{}}
}

// Start запускает опрос всех включённых ботов. Циклы работают до Stop.
func (m *Manager) Start(ctx context.Context) error {
	bots, err := m.svc.ActiveBots(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.base, m.cancel = context.WithCancel(context.WithoutCancel(ctx))
	for _, b := range bots {
		m.startLocked(b)
	}
	m.mu.Unlock()
	m.log.Info("telegram: polling запущен", "bots", len(bots))
	return nil
}

// Stop останавливает все циклы и ждёт их завершения.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	m.base, m.runners = nil, map[uuid.UUID]*runner{}
	m.mu.Unlock()
	m.wg.Wait()
}

// Activate (пере)запускает опрос бота с актуальным токеном: бот подключён,
// включён или ему сменили токен.
func (m *Manager) Activate(ctx context.Context, botID uuid.UUID) error {
	creds, err := m.svc.Credentials(ctx, botID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.base == nil {
		return nil
	}
	m.stopLocked(botID)
	if creds.IsActive {
		m.startLocked(creds)
	}
	return nil
}

// Deactivate останавливает опрос бота.
func (m *Manager) Deactivate(botID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked(botID)
}

func (m *Manager) startLocked(b domain.Credentials) {
	ctx, cancel := context.WithCancel(m.base)
	r := &runner{cancel: cancel, done: make(chan struct{})}
	m.runners[b.BotID] = r
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer close(r.done)
		m.poll(ctx, b)
	}()
}

// stopLocked отменяет цикл и ждёт его: долгий запрос прерывается отменой
// контекста, так что ожидание короткое.
func (m *Manager) stopLocked(botID uuid.UUID) {
	if r, ok := m.runners[botID]; ok {
		r.cancel()
		<-r.done
		delete(m.runners, botID)
	}
}

func (m *Manager) poll(ctx context.Context, bot domain.Credentials) {
	log := m.log.With("bot_id", bot.BotID)

	var (
		offset   int64
		backoff  time.Duration
		failedID int64 // апдейт, который не удаётся обработать
		failures int
	)
	webhookDeleted := false

	for ctx.Err() == nil {
		if backoff > 0 && !sleep(ctx, backoff) {
			return
		}
		if !webhookDeleted {
			if err := m.api.DeleteWebhook(ctx, bot.Token); err != nil {
				if fatal(err) {
					log.Error("telegram: токен бота не принят, опрос остановлен", "err", err)
					return
				}
				log.Warn("telegram: не удалось снять вебхук", "err", err)
				backoff = next(backoff)
				continue
			}
			webhookDeleted = true
		}

		updates, err := m.api.GetUpdates(ctx, bot.Token, offset, longPollWait)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			var ae *tgapi.APIError
			switch {
			case fatal(err):
				log.Error("telegram: токен бота не принят, опрос остановлен", "err", err)
				return
			case errors.As(err, &ae) && ae.Code == http.StatusConflict:
				// Кто-то ещё забирает апдейты этого бота или стоит вебхук.
				log.Warn("telegram: getUpdates 409 — бот опрашивается в другом процессе или стоит вебхук", "err", err)
				webhookDeleted = false
				backoff = maxBackoff
			case errors.As(err, &ae) && ae.RetryAfter > 0:
				backoff = time.Duration(ae.RetryAfter) * time.Second
			default:
				log.Warn("telegram: getUpdates", "err", err)
				backoff = next(backoff)
			}
			continue
		}
		backoff = 0

		for _, u := range updates {
			if err := m.svc.HandleUpdate(ctx, bot, u); err != nil {
				if ctx.Err() != nil {
					return
				}
				if failedID != u.ID {
					failedID, failures = u.ID, 0
				}
				failures++
				if failures < maxUpdateAttempts {
					log.Warn("telegram: апдейт не обработан, повторю", "update_id", u.ID, "attempt", failures, "err", err)
					backoff = next(backoff)
					break
				}
				log.Error("telegram: апдейт пропущен после повторных ошибок", "update_id", u.ID, "err", err)
			}
			offset = u.ID + 1
		}
	}
}

// fatal — токен больше не действует (отозван в @BotFather): повторять бесполезно.
func fatal(err error) bool {
	var ae *tgapi.APIError
	return errors.As(err, &ae) && (ae.Code == http.StatusUnauthorized || ae.Code == http.StatusNotFound)
}

func next(d time.Duration) time.Duration {
	if d == 0 {
		return time.Second
	}
	return min(d*2, maxBackoff)
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

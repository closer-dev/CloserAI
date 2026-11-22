// Package jobs — фоновые задачи на riverqueue/river: очередь живёт прямо в
// Postgres, поэтому Redis и брокер не нужны.
//
// Как контекст пользуется очередью (на примере индексации документов):
//
//  1. В infra контекста объявляются аргументы задачи (river.JobArgs) и воркер
//     (river.Worker), который вызывает сценарий из app.
//  2. В module.go воркер регистрируется: jobs.Register(q, &IndexWorker{...}).
//  3. Слой app ставит задачу через свой порт (например, IndexScheduler), а
//     адаптер в infra вызывает q.Enqueue. Если это происходит внутри
//     TxManager.Do, задача вставляется в ту же транзакцию: документ и задача
//     на его индексацию появятся вместе или не появятся вовсе.
//
// Порядок в main: New → Register/AddPeriodic из модулей → Migrate → Start →
// (работа) → Stop.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"closerai/internal/platform/db"
)

// DefaultMaxWorkers — сколько задач выполняется одновременно. Большинство
// задач ходят в Ollama, которая параллелит мало (OLLAMA_NUM_PARALLEL), так
// что большое число здесь не ускорит работу.
const DefaultMaxWorkers = 4

var ErrNotStarted = errors.New("jobs: очередь ещё не запущена (Start не вызван)")

type Config struct {
	MaxWorkers int
}

type Queue struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	cfg  Config

	workers  *river.Workers
	nWorkers int
	periodic []*river.PeriodicJob

	client  atomic.Pointer[river.Client[pgx.Tx]]
	working bool
}

func New(pool *pgxpool.Pool, log *slog.Logger, cfg Config) *Queue {
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = DefaultMaxWorkers
	}
	return &Queue{pool: pool, log: log, cfg: cfg, workers: river.NewWorkers()}
}

// Register добавляет воркер. Вызывать до Start (обычно из module.go контекста).
func Register[A river.JobArgs](q *Queue, w river.Worker[A]) {
	if q.client.Load() != nil {
		panic("jobs: Register после Start")
	}
	river.AddWorker(q.workers, w)
	q.nWorkers++
}

// AddPeriodic добавляет периодическую задачу (например, автозакрытие
// неактивных диалогов). Для её аргументов тоже нужен зарегистрированный воркер.
func (q *Queue) AddPeriodic(j *river.PeriodicJob) {
	if q.client.Load() != nil {
		panic("jobs: AddPeriodic после Start")
	}
	q.periodic = append(q.periodic, j)
}

// Migrate создаёт и обновляет служебные таблицы river (river_job и др.).
// Миграции river версионируются самим river, поэтому не дублируются в goose.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("jobs: migrator: %w", err)
	}
	if _, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("jobs: migrate: %w", err)
	}
	return nil
}

// Start создаёт клиента river и, если есть воркеры, начинает выполнять задачи.
// Без воркеров очередь работает только на вставку.
func (q *Queue) Start(ctx context.Context) error {
	if q.client.Load() != nil {
		return errors.New("jobs: Start вызван повторно")
	}
	cfg := &river.Config{Logger: q.log}
	if q.nWorkers > 0 {
		cfg.Workers = q.workers
		cfg.Queues = map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: q.cfg.MaxWorkers}}
		cfg.PeriodicJobs = q.periodic
	} else if len(q.periodic) > 0 {
		return errors.New("jobs: есть периодические задачи, но не зарегистрировано ни одного воркера")
	}

	client, err := river.NewClient(riverpgxv5.New(q.pool), cfg)
	if err != nil {
		return fmt.Errorf("jobs: client: %w", err)
	}
	if q.nWorkers > 0 {
		// Отмена ctx у river — жёсткая остановка с прерыванием задач, поэтому
		// отвязываемся от неё: остановка идёт через Stop.
		if err := client.Start(context.WithoutCancel(ctx)); err != nil {
			return fmt.Errorf("jobs: start: %w", err)
		}
		q.working = true
		q.log.Info("очередь задач запущена", "workers", q.nWorkers, "periodic", len(q.periodic),
			"max_workers", q.cfg.MaxWorkers)
	} else {
		q.log.Info("очередь задач: воркеров нет, работает только постановка задач")
	}
	q.client.Store(client)
	return nil
}

// Enqueue ставит задачу. Внутри TxManager.Do задача вставляется в текущую
// транзакцию и станет видна воркерам только после коммита.
func (q *Queue) Enqueue(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) error {
	client := q.client.Load()
	if client == nil {
		return ErrNotStarted
	}
	var err error
	if tx, ok := db.TxFromContext(ctx); ok {
		_, err = client.InsertTx(ctx, tx, args, opts)
	} else {
		_, err = client.Insert(ctx, args, opts)
	}
	if err != nil {
		return fmt.Errorf("jobs: enqueue %s: %w", args.Kind(), err)
	}
	return nil
}

// Stop мягко останавливает очередь: новые задачи не берутся, текущие
// доделываются. Если ctx истёк раньше, текущие задачи прерываются.
func (q *Queue) Stop(ctx context.Context) error {
	client := q.client.Load()
	if client == nil || !q.working {
		return nil
	}
	if err := client.Stop(ctx); err != nil {
		q.log.Warn("очередь задач не остановилась вовремя, прерываю задачи", "err", err)
		hardCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return client.StopAndCancel(hardCtx)
	}
	return nil
}

# Архитектура: DDD в модульном монолите

Приложение — один Go-бинарник, но каждый модуль в `internal/` — **ограниченный контекст**
(bounded context) со строгими слоями. Контексты не знают друг о друге: связываются портами в
`cmd/app/main.go` и доменными событиями. Эталоны: `internal/tenant` (маленький) и
`internal/auth` (полный: порты, транзакции, доменные ошибки).

## Слои внутри контекста

```
internal/<контекст>/
├── module.go              # composition root: собирает слои, отдаёт Module{Service, Handler}
├── domain/                # сущности, value objects, инварианты, доменные ошибки,
│                          # интерфейсы репозиториев. Ни HTTP, ни SQL, ни JSON-тегов.
├── app/                   # сценарии (use cases) и порты на внешний мир
│                          # (TxManager, Events, Embedder, Scheduler…). Публичный API контекста.
├── infra/                 # реализации портов
│   ├── postgres/
│   │   ├── queries/*.sql  # sqlc-запросы ЭТОГО контекста
│   │   ├── sqlcgen/       # сгенерировано `make sqlc`, руками не править
│   │   └── repository.go  # маппинг sqlcgen ⇄ domain, pgx-ошибки → доменные
│   ├── jobs/              # аргументы и воркеры river-задач (вызывают сценарии app)
│   └── …                  # адаптеры к llm, telegram, почте и т.п.
└── api/                   # HTTP (Gin): DTO с json/binding-тегами, хендлеры,
                           # маппинг доменных ошибок в HTTP-статусы и тексты
```

Направление зависимостей: `api → app → domain ← infra`. `module.go` знает всех.

## Общие пакеты

| Пакет | Что там | Кто импортирует |
|---|---|---|
| `internal/kernel` | shared kernel: `Identity`, `Role`, доменные события (`kernel.Event`, `MessageCreated`, …) и общие перечисления (`Sender`, `ConversationStatus`) | все слои, включая `domain` |
| `internal/platform/db` | pgx-пул, goose-миграции, `TxManager` (+ `AfterCommit`, `TxFromContext`) | `infra`, `main` |
| `internal/platform/eventbus` | шина доменных событий в памяти | `infra`, `module.go`, `main` |
| `internal/platform/jobs` | очередь фоновых задач на river | `infra`, `module.go`, `main` |
| `internal/platform/llm` | порт `llm.Provider` (Chat, Embed) и адаптер `llm/ollama` | `infra`, `main` |
| `internal/platform/*` (прочее) | config, logger, httpx, mailer, crypto | `infra`, `api`, `main` |
| `db/migrations` | единая схема БД (goose). Таблицы при этом принадлежат контекстам | — |

`platform` — техника без бизнес-логики. Слои `domain` и `app` её не импортируют: им нужное
приходит через порты (интерфейсы, объявленные в `app`).

## Правила (проверяются тестом `internal/archtest`)

1. `domain` импортирует только stdlib, `uuid` и `kernel`.
2. `app` импортирует только свой `domain`, `kernel`, `uuid` и stdlib. Всё остальное — через порты.
3. `kernel` импортирует только stdlib и `uuid`.
4. Контекст не импортирует пакеты чужого контекста. Исключение: `auth/api` (`FromContext`, `RequireRole`) в чужих `api`.
5. Контексты связываются в `cmd/app/main.go` через адаптеры портов (пример: `auth` объявляет порт
   `TenantCreator`, main подставляет `tenant.Service.Create`).
6. Асинхронные реакции — через доменные события (`platform/eventbus`), а не прямыми вызовами.
7. Таблицу читает и пишет только контекст-владелец. Чужие данные — через сервис владельца.

Правила 5–7 тестом не проверяются, их держит ревью.

## Транзакции между агрегатами

`db.TxManager.Do(ctx, fn)` кладёт транзакцию в `ctx`; репозитории любых контекстов берут её через
`db.Executor(ctx, pool)`. Поэтому регистрация создаёт компанию (`tenant`) и владельца (`auth`)
атомарно, а сценарий `app` ничего не знает о pgx. Вложенные `Do` работают в той же транзакции.

`db.AfterCommit(ctx, fn)` откладывает действие до коммита (вне транзакции — выполняет сразу).
На нём построены события: что откатилось, то никто не увидит.

## Доменные события

События — опубликованный язык между контекстами, поэтому живут в `kernel/events.go`.
Каждое — структура-значение с `TenantID`, методы `EventName()` и `EventTenant()`.

| Событие | Публикует | Слушают |
|---|---|---|
| `MessageCreated` | `conversation` | `realtime` (в панель) |
| `ConversationStatusChanged` | `conversation` | `realtime` (в панель, подсветка «ждут оператора») |
| `ConversationClosed` | `conversation` | `verdict` (ставит задачу на вердикт), `realtime` |
| `BotActivated` | `bot` (подключение, включение, смена токена) | `telegram` (запуск/перезапуск polling) |
| `BotDeactivated` | `bot` (выключение, удаление) | `telegram` (остановка polling) |

Новое событие добавляется в `kernel/events.go` и в эту таблицу; контракт согласуется с QA/техлидом
(его же видит фронтенд через SSE).

**Публикация.** В `app` контекста объявляется порт, `*eventbus.Bus` ему соответствует:

```go
// conversation/app/ports.go
type Events interface {
    Publish(ctx context.Context, events ...kernel.Event)
}

// conversation/app: внутри tx.Do — доставка случится после коммита
s.events.Publish(ctx, kernel.ConversationClosed{TenantID: c.TenantID, ConversationID: c.ID, ClosedAt: now})
```

**Подписка** — в `module.go` слушающего контекста (или в `main.go`):

```go
eventbus.Subscribe(bus, func(ctx context.Context, e kernel.ConversationClosed) error {
    return svc.ScheduleVerdict(ctx, e.TenantID, e.ConversationID)
})
bus.SubscribeAll(hub.Broadcast) // realtime: всё подряд, фильтр по e.EventTenant()
```

Обработчики синхронные и должны быть быстрыми; ошибка или паника обработчика логируется и не
влияет на других. Долгая работа (LLM, Telegram) — только через задачу river.

## Фоновые задачи (river)

Очередь живёт в Postgres (`platform/jobs`), служебные таблицы river создаются при старте.

1. В `infra/jobs` контекста: аргументы задачи (`river.JobArgs`, `Kind()` вида `"knowledge.index"`)
   и воркер (`river.Worker`), который вызывает сценарий `app`.
2. В `module.go`: `jobs.Register(q, &IndexWorker{svc: svc})`; периодические — `q.AddPeriodic(...)`.
3. Слой `app` ставит задачу через свой порт (`IndexScheduler`), адаптер вызывает `q.Enqueue`.
   Внутри `tx.Do` задача вставляется в ту же транзакцию: документ и его задача появятся вместе.

Порядок в `main`: миграции → `jobs.New` → сборка модулей (регистрация воркеров) → `Start` → HTTP.
При остановке: сначала HTTP, потом `Stop` (текущие задачи доделываются).

## Языковая модель

`platform/llm.Provider` — `Chat` (с `Temperature`, `MaxTokens`, `JSONSchema`) и `Embed`.
Недоступность сервера или отсутствие модели — ошибка `llm.ErrUnavailable` (например, повод
передать диалог оператору). Контексты не держат `llm.Provider` в `app`: объявляют узкий порт
(`Embedder`, `AnswerGenerator`, `VerdictModel`) и реализуют его в своём `infra` поверх Provider.
В тестах `app` порт подменяется фейком.

## Как добавить новый контекст (пример: `knowledge`)

1. `domain/`: `Document`, `Chunk`, статусы, `DocumentRepository`, `ChunkRepository`.
2. `app/`: `UploadDocument`, `IndexDocument`, `Search`; порты `Embedder`, `IndexScheduler`, `TxManager`.
3. `infra/postgres/queries/*.sql` + новый блок в `sqlc.yaml` (скопировать блок `tenant`, поменять пути),
   `make sqlc`, `repository.go`.
4. `infra/`: адаптер `Embedder` поверх `llm.Provider`, задача `knowledge.index` и её воркер.
5. `api/`: DTO, хендлеры, маппинг ошибок.
6. `module.go` (`Deps{Pool, Tx, Events, Jobs, LLM}`) и подключение в `cmd/app/main.go`.

## Сквозной сценарий: ответ клиенту

```
telegram/api (вебхук) ─▶ conversation/app.HandleIncoming ──tx──▶ messages, conversations
                                   │            └─ Publish(MessageCreated) ─▶ realtime ─▶ SSE
                                   ├─▶ порт AnswerProvider ═(main)═▶ rag/app.Answer
                                   │                                   ├─▶ knowledge/app.Search
                                   │                                   └─▶ llm.Provider.Chat
                                   └─▶ порт Sender ═(main)═▶ telegram (sendMessage)
```

`═(main)═` — адаптер, который собирается в `main.go`: `conversation` объявляет порт, `rag`
и `telegram` не знают, кто их вызывает.

## Карта контекстов

| Контекст | Владелец | Таблицы | Использует (порты) | Статус |
|---|---|---|---|---|
| `auth` (пользователи, сессии, email, сотрудники) | Backend №2 / №4 | `users`, `email_codes` | `tenant` (создание компании), mailer | готов |
| `tenant` (компании, тарифы) | Backend №2 | `tenants` | — | готов |
| `bot` (подключение ботов) | Backend №3 | `bots` | Telegram API (`getMe`, вебхуки), crypto, события | готов |
| `telegram` (вебхук, polling, отправка) | Backend №3 | — | `bot` (токен, секрет), `conversation` | готов (этап 2) |
| `conversation` (клиенты, диалоги, сообщения) | Backend №3 | `customers`, `conversations`, `messages` | `rag`, `telegram`, события, jobs | приём сообщений и ответ бота (эхо до этапа 4); операторы — этап 5 |
| `knowledge` (документы, чанки, индексация) | Backend №1 | `documents`, `chunks` | llm (Embed), jobs | заготовка |
| `rag` (поиск, промпт, ответ) | Backend №1 | — (чанки через `knowledge`) | `knowledge`, llm | заготовка |
| `verdict` | Backend №4 | `verdicts` | `conversation` (история), llm, jobs, событие `ConversationClosed` | заготовка |
| `realtime` (SSE) | Backend №2 | — | все события, `auth/api` | заготовка |

| Платформа | Владелец | Статус |
|---|---|---|
| `platform/db` (+ `TxManager`, `AfterCommit`) | Backend №2 | готов |
| `platform/eventbus` + события в `kernel` | Backend №4 | готов |
| `platform/jobs` (river) | Backend №2 | готов |
| `platform/llm` + `llm/ollama` | Backend №1 | готов (Chat, Embed, JSON-схема) |
| `platform/mailer`, `platform/crypto` | Backend №4 | готовы |

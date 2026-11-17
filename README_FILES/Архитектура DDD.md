# Архитектура: DDD в модульном монолите

Приложение остаётся одним Go-бинарником, но каждый модуль в `internal/` теперь —
**ограниченный контекст** (bounded context) со строгими слоями. Эталон — `internal/tenant`
(маленький) и `internal/auth` (полный: порты, транзакции, доменные ошибки).

## Слои внутри контекста

```
internal/<контекст>/
├── module.go              # composition root: собирает слои, отдаёт Module{Service, Handler}
├── domain/                # сущности, value objects, инварианты, доменные ошибки,
│                          # интерфейсы репозиториев. Ни HTTP, ни SQL, ни JSON-тегов.
├── app/                   # сценарии (use cases) и порты на внешний мир
│                          # (TxManager, Tokens, Notifier, llm.Embedder…). Публичный API контекста.
├── infra/                 # реализации портов
│   ├── postgres/
│   │   ├── queries/*.sql  # sqlc-запросы ЭТОГО контекста
│   │   ├── sqlcgen/       # сгенерировано `make sqlc`, руками не править
│   │   └── repository.go  # маппинг sqlcgen ⇄ domain, pgx-ошибки → доменные
│   └── …                  # jwt, почта, ollama, telegram и т.п.
└── api/                   # HTTP (Gin): DTO с json/binding-тегами, хендлеры,
                           # маппинг доменных ошибок в HTTP-статусы и тексты
```

Направление зависимостей: `api → app → domain ← infra`. `module.go` знает всех.

## Общие пакеты

| Пакет | Что там |
|---|---|
| `internal/kernel` | shared kernel: `Identity`, `Role` — общий язык всех контекстов |
| `internal/platform/*` | техника без бизнес-логики: config, db (+ `TxManager`), logger, httpx, mailer, crypto, eventbus |
| `db/migrations` | единая схема БД (goose). Таблицы при этом принадлежат контекстам |

## Правила (проверяются тестом `internal/archtest`)

1. `domain` импортирует только stdlib, `uuid` и `kernel`.
2. `app` импортирует только свой `domain`, `kernel`, `uuid` и stdlib. Всё остальное — через порты.
3. Контекст не импортирует пакеты чужого контекста. Исключение: `auth/api` (`FromContext`, `RequireRole`) в чужих `api`.
4. Контексты связываются в `cmd/app/main.go` через адаптеры портов (пример: `auth` объявляет порт
   `TenantCreator`, main подставляет `tenant.Service.Create`).
5. Асинхронные реакции — через доменные события в `platform/eventbus` (`MessageCreated`,
   `ConversationClosed`, …), а не прямыми вызовами.
6. Таблицу читает и пишет только контекст-владелец. Чужие данные — через сервис владельца.

## Транзакции между агрегатами

`platform/db.TxManager.Do(ctx, fn)` кладёт транзакцию в `ctx`; репозитории любых контекстов
берут её через `db.Executor(ctx, pool)`. Поэтому регистрация создаёт компанию (`tenant`) и
владельца (`auth`) атомарно, а сценарий `app` ничего не знает о pgx.

## Как добавить новый контекст (пример: `knowledge`)

1. `domain/`: `Document`, `Chunk`, `Embedding`, статусы, `DocumentRepository`, `ChunkRepository`.
2. `app/`: `UploadDocument`, `IndexDocument`, `Search…`; порт `Embedder` (реализуется через `llm.Provider`).
3. `infra/postgres/queries/*.sql` + новый блок в `sqlc.yaml` (скопировать блок `tenant`, поменять пути),
   `make sqlc`, `repository.go`.
4. `api/`: DTO, хендлеры, маппинг ошибок.
5. `module.go` и подключение в `cmd/app/main.go`.

## Карта контекстов

| Контекст | Владелец | Статус |
|---|---|---|
| `auth` (пользователи, сессии, подтверждение email) | Backend №2 / №4 | переведён на DDD |
| `tenant` (компании, тарифы) | Backend №2 | переведён на DDD |
| `knowledge` (документы, чанки, эмбеддинги в pgvector) | Backend №1 | заготовка |
| `rag` (поиск, промпт, ответ) | Backend №1 | заготовка |
| `bot`, `telegram`, `conversation` | Backend №3 | заготовки |
| `verdict` | Backend №4 | заготовка |
| `realtime` (SSE) | Backend №2 | заготовка |
| `llm` (порт `Provider` + Ollama) | Backend №1 | интерфейс; станет `infra`-адаптером для `knowledge`/`rag`/`verdict` |

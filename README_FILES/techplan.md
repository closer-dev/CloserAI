# CloserAI — технический план

B2B SaaS: Telegram-бот техподдержки для малого бизнеса. Бот отвечает клиентам по базе знаний компании (RAG) с помощью локальной LLM. Сотрудник через веб-панель видит все диалоги и вердикты бота и может в любой момент подключиться к диалогу вместо бота.

**Стек:** Go + Gin, React, PostgreSQL (+ pgvector), Ollama, Docker.

---

## 1. Архитектура

Всё приложение — **один Go-бинарник** (модульный монолит). В нём работают:
- HTTP API на Gin для React-панели;
- приём сообщений из Telegram через вебхук;
- фоновые задачи: индексация документов и вынесение вердиктов;
- realtime-канал для операторов;
- раздача собранного React-приложения через `embed.FS`, поэтому nginx не нужен.

Внутри бинарника код разбит на **ограниченные контексты** (DDD): `auth`, `tenant`, `bot`, `telegram`, `conversation`, `knowledge`, `rag`, `verdict`, `realtime`. Каждый владеет своими таблицами и сценариями. Друг к другу контексты обращаются только через порты, которые связываются в `main.go`, и через доменные события. Общая техника без бизнес-логики (БД и транзакции, шина событий, очередь задач, LLM, почта) лежит в `internal/platform`. Подробно: [Архитектура DDD](Архитектура%20DDD.md).

```
┌──────────────┐   webhook    ┌─────────────────── app (Go, Gin) ───────────────────┐
│  Telegram    │ ───────────▶ │  telegram ─▶ conversation ─▶ rag ─▶ llm (Ollama)    │
│  (клиенты)   │ ◀─────────── │      ▲            │             │                    │
└──────────────┘  sendMessage │      │            ▼             ▼                    │
                              │   operator ◀── realtime (SSE)  knowledge (pgvector)  │
┌──────────────┐  REST + SSE  │      │                                               │
│ React-панель │ ◀──────────▶ │   auth / tenant / verdict / jobs                     │
└──────────────┘              └──────────────┬──────────────────────┬───────────────┘
                                             ▼                      ▼
                                  PostgreSQL + pgvector          Ollama
```

**Docker Compose: 3 контейнера.** `app`, `postgres` (образ `pgvector/pgvector:pg17`) и `ollama`. Отдельная векторная БД, Redis и брокер очередей не нужны: их роль выполняет Postgres.

## 2. Технологический стек

### Backend (Go)
| Задача | Библиотека | Почему |
|---|---|---|
| HTTP | `gin-gonic/gin` | задан стеком; в нём уже есть SSE (`c.SSEvent`) и валидация |
| Драйвер БД | `jackc/pgx/v5` | самый быстрый и полный драйвер для Postgres |
| SQL | `sqlc` (генерация кода) | обычный SQL превращается в типобезопасный Go-код без «магии» ORM |
| Векторы | `pgvector/pgvector-go` | тип `vector` для pgx |
| Миграции | `pressly/goose` | SQL-файлы, запуск при старте приложения |
| Telegram | `go-telegram/bot` | активно поддерживается, работает и с вебхуками, и с long polling |
| LLM | `ollama/ollama/api` | официальный клиент; обёрнут в `platform/llm` (порт `Provider`) |
| Фоновые задачи | `riverqueue/river` | очередь прямо в Postgres, без Redis; обёрнута в `platform/jobs` |
| События между контекстами | `platform/eventbus` (свой, в памяти) | доставка после коммита транзакции, без брокера |
| Конфиг | `caarlos0/env` | настройки из переменных окружения |
| Логи | `log/slog` (stdlib) | — |
| Auth | `golang-jwt/jwt/v5` + `golang.org/x/crypto/bcrypt` | JWT в httpOnly cookie |
| Шифрование токенов ботов | `crypto/aes` (AES-GCM, stdlib) | — |
| Почта | `net/smtp` (stdlib) | письма с кодами подтверждения; в dev письма пишутся в лог |
| Парсинг документов | `ledongthuc/pdf`; txt/md встроенными средствами | DOCX можно добавить позже |
| Тесты | `stretchr/testify` + `testcontainers-go` | интеграционные тесты с реальным Postgres |

### Локальная модель ИИ (через Ollama)
- **Чат:** `qwen2.5:3b-instruct` или более новая Qwen 3–4B. Это небольшие модели, которые прилично понимают русский и работают даже на CPU. Для самого слабого железа подойдёт `qwen2.5:1.5b`.
- **Эмбеддинги:** `bge-m3`, хорошо справляется с несколькими языками, включая русский (вектор размерности 1024). Лёгкая альтернатива — `nomic-embed-text` (тогда поменять размерность в миграции).
- Код работает с моделью только через интерфейс `platform/llm.Provider` (`Chat`, `Embed`), реализация — `platform/llm/ollama`. Контексты объявляют свои узкие порты (`Embedder`, `AnswerGenerator`…) и реализуют их в своём `infra` поверх `Provider`. Позже модель или провайдера можно заменить, не трогая контексты.

### Frontend
- **Vite + React + TypeScript**
- **React Router**: маршрутизация
- **TanStack Query**: запросы к API и кэш
- **Tailwind CSS + shadcn/ui**: готовые компоненты (таблицы, диалоги, чат)
- **react-hook-form + zod**: формы
- Обновления в реальном времени через встроенный в браузер `EventSource` (SSE), без дополнительных библиотек

### Инфраструктура
- Многоэтапный Dockerfile: `node` (сборка React) → `golang` (сборка бинарника со встроенной статикой) → `gcr.io/distroless/static`
- `deploy/docker-compose.yml` для разработки и прода; `Makefile` с командами `make dev`, `make sqlc`, `make test` и т.д.
- Для разработки: `air` (горячая перезагрузка Go-кода) и Vite dev server с проксированием `/api`

## 3. Структура репозитория

Код разбит по ограниченным контекстам (DDD), а внутри каждого — по слоям `domain` / `app` / `infra` / `api`. Новая фича — это новый контекст в `internal/`. Подробно: [Архитектура DDD](Архитектура%20DDD.md).

```
closerai/
├── cmd/app/main.go            # composition root: платформа, сборка контекстов, запуск
├── internal/
│   ├── kernel/                # shared kernel: Identity, Role, доменные события (kernel.Event)
│   ├── platform/              # техника без бизнес-логики:
│   │   ├── db/                #   pgx-пул, goose, TxManager (+ AfterCommit)
│   │   ├── eventbus/          #   шина доменных событий
│   │   ├── jobs/              #   очередь задач river
│   │   ├── llm/               #   порт Provider + ollama/
│   │   └── …                  #   config, logger, httpx, crypto, mailer
│   │   # ограниченные контексты:
│   ├── auth/                  # вход, JWT, middleware, роли, подтверждение email, сотрудники
│   ├── tenant/                # компании-клиенты, тарифы
│   ├── bot/                   # подключение токена бота, установка вебхука, настройки
│   ├── telegram/              # приём апдейтов, отправка сообщений
│   ├── conversation/          # клиенты, диалоги, сообщения, статусы, передача оператору
│   ├── knowledge/             # документы, чанкинг, индексация (river-задачи), поиск по чанкам
│   ├── rag/                   # сборка промпта и ответ по найденному контексту
│   ├── verdict/               # итоговая оценка диалога
│   ├── realtime/              # SSE-хаб для операторов
│   └── archtest/              # тест, который следит за правилами слоёв
│   # в каждом контексте: module.go, domain/, app/, infra/{postgres/{queries,sqlcgen},jobs,…}, api/
├── db/
│   └── migrations/            # goose *.sql (встраиваются в бинарник); запросы sqlc — в контекстах
├── web/                       # React-приложение (Vite)
├── deploy/                    # docker-compose.yml
├── Dockerfile
├── Makefile
└── sqlc.yaml
```

**Правила, которые упрощают добавление фич:**
1. Контекст регистрирует свои маршруты сам: `func (h *Handler) Routes(r *gin.RouterGroup)` в слое `api`.
2. Контексты не обращаются к таблицам и пакетам друг друга, только к сервисам через порты, которые связываются в `main.go`. Слой `domain` не зависит ни от чего, кроме `kernel`; слой `app` — только от своего `domain` и `kernel`.
3. Внутренняя шина событий (`platform/eventbus`) рассылает доменные события из `kernel`: `MessageCreated`, `ConversationStatusChanged`, `ConversationClosed`. Доставка — после коммита транзакции. На них подписываются realtime-хаб, модуль вердиктов, а в будущем аналитика, уведомления или CRM-интеграции, и ядро при этом не меняется.
4. Долгая работа (индексация, вердикт, автозакрытие) — задачи river через `platform/jobs`. Задача ставится в той же транзакции, что и данные, и воркер вызывает сценарий из `app`.

**Карта контекстов** (кто какими таблицами владеет и от кого зависит):

| Контекст | Таблицы | Использует |
|---|---|---|
| `auth` | `users`, `email_codes` | `tenant` (создание компании при регистрации), mailer |
| `tenant` | `tenants` | — |
| `bot` | `bots` | Telegram API (`getMe`, `setWebhook`), crypto |
| `telegram` | — | `bot` (токен, секрет вебхука), `conversation` (входящее сообщение) |
| `conversation` | `customers`, `conversations`, `messages` | `rag` (ответ), `telegram` (отправка), события, jobs (автозакрытие) |
| `knowledge` | `documents`, `chunks` | llm (`Embed`), jobs (индексация) |
| `rag` | — | `knowledge` (гибридный поиск), llm (`Chat`, `Embed`) |
| `verdict` | `verdicts` | событие `ConversationClosed`, `conversation` (история), llm (JSON-вывод), jobs |
| `realtime` | — | все события, `auth` (кто подключён) |

## 4. Схема БД (основные таблицы)

У каждой бизнес-таблицы есть колонка `tenant_id`, а каждый запрос фильтрует по ней (это изоляция данных разных компаний). Полная схема — в `db/migrations/00001_init.sql`. Схема общая, но у каждой таблицы один контекст-владелец (см. карту контекстов в разделе 3): только он пишет в неё sqlc-запросы.

```sql
tenants        (id, name, plan, created_at)
users          (id, tenant_id, email, password_hash, role[owner|operator], name,
                email_verified_at NULL)
email_codes    (id, user_id, purpose[verify_email], code_hash, attempts,
                expires_at, created_at)  -- одноразовые коды, хранится только HMAC
bots           (id, tenant_id, tg_bot_id, username, token_enc, webhook_secret,
                system_prompt, greeting, handoff_threshold, is_active)
customers      (id, tenant_id, tg_user_id, username, first_name, last_name)
conversations  (id, tenant_id, bot_id, customer_id,
                status[bot|waiting_operator|operator|closed],
                assigned_user_id, last_message_at, closed_at)
messages       (id, conversation_id, sender[customer|bot|operator|system],
                user_id NULL, text, tg_message_id, sources JSONB, created_at)
documents      (id, tenant_id, title, source_type[file|text|url],
                status[pending|indexing|ready|failed], error, created_at)
chunks         (id, tenant_id, document_id, content, embedding vector(1024),
                tsv tsvector)            -- HNSW-индекс на embedding + GIN на tsv
verdicts       (id, conversation_id, resolved bool,
                category, sentiment[positive|neutral|negative],
                summary text, needs_followup bool, raw JSONB, created_at)
-- + служебные таблицы river (river_job, river_queue, …): их создаёт
--   platform/jobs.Migrate при старте, в goose-миграциях их нет
```

## 5. Ключевые процессы

### 5.0 Подтверждение email
1. При регистрации (и при первом входе сотрудника, которого добавил владелец) создаётся код из 6 цифр и отправляется на почту. В базе хранится только HMAC-SHA256 кода.
2. Пользователь сразу получает сессию, но пока email не подтверждён, ему доступны только `GET /api/me`, `POST /api/auth/verify`, `POST /api/auth/resend` и `logout`. Остальной API отвечает 403, панель показывает экран ввода кода.
3. Код живёт 15 минут, даёт 5 попыток (счётчик увеличивается атомарно, до сравнения), повторная отправка — не чаще раза в 60 секунд. Новый код отменяет старые.
4. После подтверждения выставляется `users.email_verified_at` и выдаётся новый JWT с отметкой о подтверждении.

Отправка идёт через интерфейс `mailer.Sender`: SMTP (`SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM`) или, если `SMTP_HOST` пуст, вывод письма в лог — для разработки SMTP не нужен. Тот же `mailer` потом используется для приглашений операторов и сброса пароля.

### 5.1 Подключение бота владельцем бизнеса
Контекст `bot`. Токены ботов **не хранятся в `.env`**: у каждой компании свой бот, токен — её данные.

1. Владелец создаёт бота у @BotFather и вставляет токен в панель (`POST /api/bots`).
2. Backend проверяет формат токена и сам токен (`getMe`), шифрует его AES-GCM (`ENCRYPTION_KEY`) и сохраняет. Один и тот же бот не может быть подключён дважды (`tg_bot_id` уникален).
3. В режиме `TG_MODE=webhook` backend в той же транзакции вызывает `setWebhook` с адресом `{PUBLIC_URL}/tg/webhook/{bot_id}` и `secret_token`; если Telegram отказал, бот не сохраняется. Входящие вебхуки проверяются по заголовку `X-Telegram-Bot-Api-Secret-Token`.
4. Публикуется `BotActivated`. Выключение (`PATCH is_active=false`) и удаление снимают вебхук и публикуют `BotDeactivated`; по этим событиям контекст `telegram` запускает и останавливает приём апдейтов. Смена токена (после `/revoke` в @BotFather) — `PATCH token`, принимается только токен того же бота.

Один процесс обслуживает сколько угодно ботов. Для локальной разработки — режим long polling (`TG_MODE=polling`, по умолчанию): HTTPS не нужен, вебхуки не ставятся. `TG_MODE=webhook` требует `PUBLIC_URL` с `https://`, иначе приложение не стартует.

### 5.2 Ответ клиенту (RAG)
Участвуют `telegram` (приём и отправка), `conversation` (сценарий целиком), `rag` и `knowledge` (поиск и ответ). `conversation` объявляет порты `AnswerProvider` и `Sender`, `main.go` подставляет в них `rag` и `telegram`.

1. Приходит апдейт → находим или создаём `customer` и активный `conversation`, сохраняем сообщение и в той же транзакции публикуем `MessageCreated` (подписчики получат его после коммита).
2. Если статус `operator` или `waiting_operator`, бот молчит: сообщение просто появляется у оператора.
3. Иначе:
   - **Эмбеддинг** вопроса (с учётом 2–3 последних реплик).
   - **Гибридный поиск** в `chunks` (сервис `knowledge`, `rag` к таблице не обращается): `top-k=5` по косинусной близости плюс полнотекстовый поиск по `tsvector`. Оба результата объединяются через Reciprocal Rank Fusion.
   - **Если ничего релевантного не нашлось** (близость ниже `handoff_threshold`) или клиент просит «человека/оператора», статус меняется на `waiting_operator` (событие `ConversationStatusChanged`). Клиент получает «Передаю специалисту», операторы — уведомление. Так же поступаем, если модель недоступна (`llm.ErrUnavailable`).
   - **Промпт:** системная инструкция («отвечай ТОЛЬКО по контексту; если ответа нет — скажи об этом») + чанки + история + вопрос.
   - **Ответ модели** отправляется в Telegram (с `sendChatAction typing`). В `messages.sources` сохраняются id использованных чанков, чтобы оператор видел, на чём основан ответ.

### 5.3 Индексация базы знаний
Контекст `knowledge`. Загрузка файла в одной транзакции создаёт `document(pending)` и задачу river `knowledge.index`. Задача:
- извлекает текст;
- режет его на чанки по ~500 токенов с перекрытием 50 (сначала по абзацам, затем по размеру);
- получает эмбеддинги пачками и записывает их в `chunks`;
- переводит документ в статус `ready`.

### 5.4 Подключение оператора
- В панели список диалогов с фильтрами по статусу и боту. Сообщения приходят в реальном времени через SSE (`GET /api/stream`): контекст `realtime` подписан на все события шины и рассылает их только сотрудникам того же `tenant_id`.
- Кнопка **«Взять диалог»**: статус меняется на `operator`, в диалог записывается `assigned_user_id`, в историю — системное сообщение, клиенту можно отправить «К вам подключился специалист».
- Сообщение оператора уходит как `POST /api/conversations/:id/messages` → Telegram `sendMessage` → сохраняется с `sender=operator`.
- Кнопка **«Вернуть боту»** или **«Закрыть»**.

Блокировка от двойного захвата: `UPDATE conversations SET status='operator', assigned_user_id=$1 WHERE id=$2 AND status<>'operator'`.

### 5.5 Вердикт
Вердикт выносится при закрытии диалога: вручную или периодической задачей river (контекст `conversation`) после N минут неактивности (например, 30). Закрытие публикует `ConversationClosed`; контекст `verdict` подписан на него и ставит задачу river `verdict.generate` (с ретраями при сбое модели). Задача отправляет модели всю переписку и просит вернуть JSON по схеме (у Ollama есть параметр `format` с JSON-схемой):
```json
{"resolved": true, "category": "доставка", "sentiment": "neutral",
 "summary": "Клиент уточнял сроки доставки, бот ответил по базе знаний",
 "needs_followup": false}
```
Результат сохраняется в `verdicts` и показывается в карточке диалога и в списке (бейджи «решено / не решено», категория).

## 6. API (основное)
```
POST   /api/auth/register | login | logout        GET /api/me
POST   /api/auth/verify | resend                 # подтверждение email кодом
GET    /api/bots | /api/bots/:id                # все сотрудники; токен и секрет не отдаются
POST   /api/bots  {token, system_prompt?, greeting?, handoff_threshold?}          # owner
PATCH  /api/bots/:id  {token?, system_prompt?, greeting?, handoff_threshold?, is_active?}  # owner
DELETE /api/bots/:id                             # owner
GET    /api/documents      POST /api/documents (multipart)   DELETE /api/documents/:id
POST   /api/rag/test       # «песочница»: задать вопрос и увидеть ответ и найденные чанки
GET    /api/conversations?status=&bot_id=&q=&page=
GET    /api/conversations/:id            # сообщения + вердикт
POST   /api/conversations/:id/take | release | close
POST   /api/conversations/:id/messages
GET    /api/users  POST /api/users       # приглашение операторов (роль owner)
GET    /api/stream                       # SSE
POST   /tg/webhook/:botID                # Telegram
GET    /healthz
```

## 7. Экраны React-панели
1. **Вход / регистрация компании**
2. **Диалоги**: таблица с фильтрами, статусами, бейджем вердикта и подсветкой «ждут оператора»
3. **Чат диалога**: переписка (клиент, бот, оператор, системные сообщения), раскрываемые источники для ответов бота, кнопки «Взять / Вернуть / Закрыть», поле ввода, панель вердикта
4. **База знаний**: загрузка файлов, статусы индексации, песочница для проверки RAG
5. **Настройки бота**: токен, системный промпт, приветствие, порог передачи оператору
6. **Команда**: операторы и роли

## 8. Этапы разработки

| # | Этап | Результат | Срок* |
|---|---|---|---|
| 0 | Каркас | репозиторий, Docker Compose (app+pg+ollama), goose, sqlc, конфиг, healthz, Vite-проект, встраивание статики; DDD-слои и `archtest`; платформа: `TxManager`, `eventbus`, `jobs` (river), `llm/ollama` | 3–4 дня |
| 1 | Auth и tenants | регистрация, JWT, роли, middleware для `tenant_id`, страница входа | 3–4 дня |
| 1.5 | Подтверждение email | `mailer` (SMTP / лог), коды подтверждения, ограничение попыток, экран ввода кода | 1–2 дня |
| 2 | Telegram | подключение бота, вебхук или polling, сохранение клиентов и сообщений, эхо-ответ | 3–4 дня |
| 3 | База знаний | загрузка документов, river-задачи, чанкинг, эмбеддинги, pgvector, UI базы знаний | 5–6 дней |
| 4 | RAG-ответы | поиск, промпт, генерация, источники, песочница | 4–5 дней |
| 5 | Панель операторов | список и чат диалогов, SSE, взять / вернуть / закрыть, автоматическая передача оператору | 6–7 дней |
| 6 | Вердикты | закрытие по таймауту, JSON-вердикт, отображение в UI | 2–3 дня |
| 7 | Доведение до прода | тесты (сервисы + интеграционные), rate limit, логи, бэкапы БД, HTTPS (Caddy или внешний прокси), README | 4–5 дней |

\*Ориентир для одного разработчика. **MVP = этапы 0–6, около 6 недель.**

**Что можно добавить после MVP** (каждое — отдельный модуль): биллинг и лимиты тарифов, импорт базы знаний с сайта (краулер), аналитика по вердиктам, уведомления операторам в Telegram, шаблоны быстрых ответов, другие каналы (WhatsApp, виджет на сайт) через интерфейс `channel.Sender`.

## 9. Риски и как их снизить
- **Слабая модель может выдумывать ответы.** Строгий промпт, низкая `temperature` (0.1–0.3), порог релевантности с передачей оператору, хранение источников, песочница для проверки базы знаний.
- **Скорость на CPU.** Модель 3B на CPU отвечает примерно за 3–10 с. Нужно выставить `OLLAMA_NUM_PARALLEL`, держать модель в памяти (`keep_alive`) и показывать клиенту «печатает…». При росте нагрузки — сервер с GPU, код при этом не меняется.
- **Изоляция компаний.** `tenant_id` в каждом sqlc-запросе плюс интеграционные тесты на утечки данных. Позже можно добавить Postgres RLS.
- **Персональные данные** (если работаете в РФ — 152-ФЗ). Хранить данные на своём сервере, шифровать токены ботов, предусмотреть удаление данных клиента.

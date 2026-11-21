-include .env
export

COMPOSE := docker compose -f deploy/docker-compose.yml --env-file .env

.PHONY: help env deps-up deps-down models dev dev-api dev-api-watch dev-web web-build build up down logs sqlc migration migrate test vet lint

help: ## Список команд
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

env: .env ## Создать .env из примера (если его ещё нет)

# Без зависимостей: .env создаётся только если его нет и никогда не перезаписывается.
# Так как он подключён через -include, make создаст его автоматически перед любой командой.
.env:
	cp .env.example .env
	@echo "Создан .env из .env.example — для прода замените секреты"

deps-up: ## Поднять Postgres, Ollama и Mailpit для локальной разработки
	$(COMPOSE) up -d postgres ollama mailpit

deps-down: ## Остановить зависимости
	$(COMPOSE) down

models: ## Скачать модели в Ollama
	$(COMPOSE) exec ollama ollama pull $(LLM_CHAT_MODEL)
	$(COMPOSE) exec ollama ollama pull $(LLM_EMBED_MODEL)

dev: dev-api-watch ## Запустить Go-сервер с горячей перезагрузкой (air)

dev-api: ## Запустить Go-сервер локально (:8080)
	go run ./cmd/app

dev-api-watch: ## Go-сервер с горячей перезагрузкой (air)
	go run ./...

dev-web: ## Запустить Vite dev server (:5173, проксирует /api на :8080)
	cd web && npm run dev

web-build: ## Собрать фронтенд в web/dist
	cd web && npm ci && npm run build

build: web-build ## Собрать бинарник bin/app со встроенным фронтендом
	CGO_ENABLED=0 go build -o bin/app ./cmd/app

up: ## Поднять всё в Docker (app + postgres + ollama + mailpit)
	$(COMPOSE) --profile app up -d --build

down: ## Остановить всё
	$(COMPOSE) --profile app down

logs: ## Логи приложения
	$(COMPOSE) logs -f app

sqlc: ## Сгенерировать Go-код из db/queries
	docker run --rm -u "$$(id -u):$$(id -g)" -v "$(CURDIR):/src" -w /src sqlc/sqlc generate
	go mod tidy

migration: ## Создать миграцию: make migration name=add_something
	go run ://github.com -dir db/migrations create $(name) sql

migrate: ## Применить все новые миграции к локальной БД
	go run ://github.com -dir db/migrations postgres "$(DATABASE_URL)" up

test: ## Тесты
	go test ./...

vet: ## Статический анализ Go
	go vet ./...

lint: vet ## Линтеры Go и TypeScript
	go run ://github.com run ./...
	cd web && npm run lint
river-stats: ## Проверить размер и статус очереди river в БД
	$(COMPOSE) exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) -c "SELECT state, count(*) FROM river_job GROUP BY state;"

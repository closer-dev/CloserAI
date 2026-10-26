-include .env
export

COMPOSE := docker compose -f deploy/docker-compose.yml --env-file .env

.PHONY: help env deps-up deps-down models dev-api dev-web web-build build up down logs sqlc migration test vet

help: ## Список команд
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

env: ## Создать .env из примера
	@test -f .env || cp .env.example .env

deps-up: ## Поднять Postgres и Ollama для локальной разработки
	$(COMPOSE) up -d postgres ollama

deps-down: ## Остановить зависимости
	$(COMPOSE) down

models: ## Скачать модели в Ollama
	$(COMPOSE) exec ollama ollama pull $(LLM_CHAT_MODEL)
	$(COMPOSE) exec ollama ollama pull $(LLM_EMBED_MODEL)

dev-api: ## Запустить Go-сервер локально (:8080)
	go run ./cmd/app

dev-web: ## Запустить Vite dev server (:5173, проксирует /api на :8080)
	cd web && npm run dev

web-build: ## Собрать фронтенд в web/dist
	cd web && npm ci && npm run build

build: web-build ## Собрать бинарник bin/app со встроенным фронтендом
	CGO_ENABLED=0 go build -o bin/app ./cmd/app

up: ## Поднять всё в Docker (app + postgres + ollama)
	$(COMPOSE) --profile app up -d --build

down: ## Остановить всё
	$(COMPOSE) --profile app down

logs: ## Логи приложения
	$(COMPOSE) logs -f app

sqlc: ## Сгенерировать Go-код из db/queries
	docker run --rm -v "$(CURDIR):/src" -w /src sqlc/sqlc generate
	go mod tidy

migration: ## Создать миграцию: make migration name=add_something
	go run github.com/pressly/goose/v3/cmd/goose@latest -dir db/migrations create $(name) sql

test: ## Тесты
	go test ./...

vet: ## Статический анализ
	go vet ./...

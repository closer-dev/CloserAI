-include .env
export

TEST_DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:5432/closer_ai_test?sslmode=disable

COMPOSE := docker compose -f deploy/docker-compose.yml --env-file .env

.PHONY: help env deps-up deps-down models dev dev-api dev-api-watch dev-web web-build build up down logs sqlc migration migrate test test-int vet lint river-stats

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
	go run github.com/air-verse/air@latest

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

sqlc: ## Сгенерировать Go-код sqlc для всех контекстов (internal/*/infra/postgres)
	docker run --rm -u "$$(id -u):$$(id -g)" -v "$(CURDIR):/src" -w /src sqlc/sqlc generate
	go mod tidy

migration: ## Создать миграцию: make migration name=add_something
	go run github.com/pressly/goose/v3/cmd/goose@latest -dir db/migrations create $(name) sql

migrate: ## Применить миграции к локальной БД (нужен goose в PATH)
	goose -dir db/migrations postgres "$(DATABASE_URL)" up

test: ## Юнит-тесты (интеграционные пропускаются без TEST_DATABASE_URL)
	go test ./...

test-int: ## Все тесты, включая интеграционные (нужен make deps-up; база closer_ai_test)
	$(COMPOSE) exec -T postgres sh -c 'psql -U $$POSTGRES_USER -tc "SELECT 1 FROM pg_database WHERE datname = '"'"'closer_ai_test'"'"'" | grep -q 1 || createdb -U $$POSTGRES_USER closer_ai_test'
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -count=1 ./...

vet: ## Статический анализ
	go vet ./...

lint: vet ## Статический анализ Go и линтер фронтенда
	cd web && npm run lint

river-stats: ## Показать число задач River по статусам
	$(COMPOSE) exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) -c "SELECT state, count(*) FROM river_job GROUP BY state;"

# Runbook: развёртывание и эксплуатация CloserAI

## Подготовка сервера

1. Подготовьте сервер с ресурсами, подходящими для выбранной модели и нагрузки.
2. Настройте файрвол: оставьте доступными SSH и HTTP/HTTPS, закройте PostgreSQL и Ollama от внешнего доступа.
3. Установите Docker и Docker Compose.
4. Настройте параметры PostgreSQL и лимиты контейнеров в `.env` по ресурсам сервера.

## Первичное развёртывание

1. Скопируйте `.env.example` в `.env` и замените все `CHANGE_ME`/`SET_*` значения. Сгенерируйте отдельные случайные секреты для `JWT_SECRET`, `HMAC_SECRET` и `ENCRYPTION_KEY`.
2. Укажите production URL, режим Telegram и параметры SMTP.
3. Настройте домен в Caddy и DNS-запись на сервер.
4. Запустите `make up`, затем загрузите необходимые модели командой `make models`.

## Резервное копирование

Скрипт `deploy/backup.sh` создаёт сжатый дамп PostgreSQL и удаляет локальные дампы старше срока хранения. Для регулярного запуска настройте cron и отдельно настройте копирование каталога бэкапов во внешнее хранилище.

## Восстановление базы

```bash
gunzip -c /var/backups/closerai/closerai_db_YYYYMMDD_HHMMSS.sql.gz | \
  docker compose -f deploy/docker-compose.yml --env-file .env exec -T postgres \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB"
```

## Откат релиза

Верните предыдущую версию кода или образа и перезапустите приложение:

```bash
docker compose -f deploy/docker-compose.yml --env-file .env up -d --no-deps app
```

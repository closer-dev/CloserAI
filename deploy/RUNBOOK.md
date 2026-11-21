cat << 'EOF' > deploy/RUNBOOK.md
# Runbook: Развёртывание и эксплуатация CloserAI

## 1. Подготовка прод-сервера (РФ, 152-ФЗ)
1. Арендовать VPS/выделенный сервер в дата-центре РФ (рекомендуется GPU NVIDIA T4 16GB+ или от 8 vCPU / 16GB RAM).
2. Настроить файрвол (`ufw`), закрыв порты БД (`5432`) и Ollama (`11434`) извне:
   ```bash
   sudo ufw default deny incoming
   sudo ufw default allow outgoing
   sudo ufw allow 22/tcp
   sudo ufw allow 80/tcp
   sudo ufw allow 443/tcp
   sudo ufw enable
   ```
3. Установить Docker и Docker Compose.

## 2. Первичное развёртывание
1. Скопировать `.env.example` в `.env` и заполнить боевые секреты (`openssl rand -hex 32` для `JWT_SECRET` и `HMAC_SECRET`, `openssl rand -base64 32` для `ENCRYPTION_KEY`).
2. Указать `APP_ENV=prod`, `PUBLIC_URL=https://your-domain.ru`, `TG_MODE=webhook` и параметры российского SMTP-провайдера (SPF/DKIM/DMARC должны быть прописаны в DNS).
3. Поднять инфраструктуру и приложение:
   ```bash
   make up
   make models
   ```

## 3. Резервное копирование (Cron)
Добавить в `crontab -e` ежедневный запуск в 03:00 ночи (плюс настроить копирование папки `/var/backups/closerai` во внешнее S3-хранилище через `rclone`):
```cron
0 3 * * * cd /opt/closerai && ./deploy/backup.sh >> /var/log/closerai-backup.log 2>&1
```

## 4. Восстановление БД из бэкапа
```bash
gunzip -c /var/backups/closerai/closerai_db_YYYYMMDD_HHMMSS.sql.gz | \
  docker compose -f deploy/docker-compose.yml --env-file .env exec -T postgres \
  psql -U postgres -d closer_ai
```

## 5. Откат релиза (Rollback)
1. В `deploy/docker-compose.yml` указать предыдущий стабильный тег образа `app` (или сделать `git checkout <commit_hash>`).
2. Выполнить перезапуск контейнера:
   ```bash
   docker compose -f deploy/docker-compose.yml --env-file .env up -d --no-deps app
   ```
EOF


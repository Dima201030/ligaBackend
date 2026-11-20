#!/usr/bin/env bash
# Sync ligaBackend to the Pi, rebuild, and bring up the permanent stack
# (db + migrate + api + rankingsnapshot). mailworker stays off until SMTP is set.
set -euo pipefail

cd "$(dirname "$0")/.."

HOST="${PI_HOST:-dima@91.223.89.199}"
DIR="${PI_DIR:-/home/dima/ligaBackend}"

echo "== Синхронизация исходников"
rsync -az --delete \
  --exclude .git --exclude data --exclude .DS_Store \
  --exclude '.env' --exclude 'bin/' --exclude 'tmp/' \
  ./ "$HOST:$DIR/"

echo "== COMPOSE_PROFILES=jobs в .env на Pi (без SMTP → mailworker не стартует)"
ssh "$HOST" "cd '$DIR' && touch .env && grep -q '^COMPOSE_PROFILES=' .env \
  && sed -i 's/^COMPOSE_PROFILES=.*/COMPOSE_PROFILES=jobs/' .env \
  || printf '\nCOMPOSE_PROFILES=jobs\n' >> .env"

echo "== Сборка и запуск"
ssh "$HOST" "cd '$DIR' && docker compose --profile jobs build && docker compose --profile jobs up -d && docker compose --profile jobs ps"

echo "Готово: API на 127.0.0.1:3020 (nginx → …/api/)"

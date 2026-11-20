#!/usr/bin/env bash
# Sync ligaBackend to the Pi, rebuild, and bring up the permanent stack
# (db + migrate + api + rankingsnapshot). mailworker starts only when
# SMTP_HOST + SMTP_FROM are set in the Pi .env (compose profile smtp).
set -euo pipefail

cd "$(dirname "$0")/.."

HOST="${PI_HOST:-dima@91.223.89.199}"
DIR="${PI_DIR:-/home/dima/ligaBackend}"

echo "== Синхронизация исходников"
rsync -az --delete \
  --exclude .git --exclude data --exclude .DS_Store \
  --exclude '.env' --exclude 'bin/' --exclude 'tmp/' \
  ./ "$HOST:$DIR/"

echo "== COMPOSE_PROFILES на Pi (jobs всегда; smtp только при SMTP_HOST+SMTP_FROM)"
# shellcheck disable=SC2029
ssh "$HOST" "cd $(printf %q "$DIR") && touch .env && bash -s" <<'REMOTE'
set -euo pipefail
env_get() {
  local key="$1" line val
  line=$(grep -E "^${key}=" .env | tail -1 || true)
  if [[ -z "$line" ]]; then
    echo ""
    return
  fi
  val="${line#*=}"
  val="${val%\"}"
  val="${val#\"}"
  val="${val%\'}"
  val="${val#\'}"
  printf '%s' "$val"
}
host="$(env_get SMTP_HOST)"
frm="$(env_get SMTP_FROM)"
if [[ -n "$host" && -n "$frm" ]]; then
  profiles="jobs,smtp"
  smtp_ready=1
else
  profiles="jobs"
  smtp_ready=0
fi
if grep -q '^COMPOSE_PROFILES=' .env; then
  sed -i "s/^COMPOSE_PROFILES=.*/COMPOSE_PROFILES=${profiles}/" .env
else
  printf '\nCOMPOSE_PROFILES=%s\n' "$profiles" >> .env
fi
echo "COMPOSE_PROFILES=${profiles} (smtp_ready=${smtp_ready})"
REMOTE

echo "== Сборка и запуск"
if ssh "$HOST" "cd $(printf %q "$DIR") && grep -q '^COMPOSE_PROFILES=.*smtp' .env"; then
  ssh "$HOST" "cd $(printf %q "$DIR") && docker compose --profile jobs --profile smtp build && docker compose --profile jobs --profile smtp up -d && docker compose --profile jobs --profile smtp ps"
else
  ssh "$HOST" "cd $(printf %q "$DIR") && docker compose --profile jobs build && docker compose --profile jobs up -d && docker compose --profile jobs ps"
  echo "mailworker пропущен: задайте SMTP_HOST и SMTP_FROM в ${DIR}/.env"
fi

echo "Готово: API на 127.0.0.1:3020 (nginx → …/api/)"

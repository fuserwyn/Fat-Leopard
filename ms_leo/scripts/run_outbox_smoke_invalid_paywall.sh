#!/usr/bin/env bash
# Вставляет смоук-событие в outbox_events (см. outbox_smoke_invalid_paywall.sql).
# Использование из корня репозитория или из ms_leo:
#   export DATABASE_URL='postgres://...'
#   ./scripts/run_outbox_smoke_invalid_paywall.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SQL_FILE="${SCRIPT_DIR}/outbox_smoke_invalid_paywall.sql"

if [[ -z "${DATABASE_URL:-}" ]]; then
  echo "ERROR: задай DATABASE_URL (строка подключения к той же БД, что у ms_leo)." >&2
  exit 1
fi

if ! command -v psql >/dev/null 2>&1; then
  echo "ERROR: нужен psql (PostgreSQL client)." >&2
  exit 1
fi

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$SQL_FILE"

echo ""
echo "Проверка: SELECT id, event_type, status, attempts, last_error, updated_at"
echo "           FROM outbox_events WHERE aggregate_key LIKE 'smoke:%' ORDER BY id DESC LIMIT 5;"

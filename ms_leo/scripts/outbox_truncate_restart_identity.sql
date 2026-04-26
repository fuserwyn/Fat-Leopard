-- Полная очистка outbox_events и сброс счётчика id с 1.
-- Выполняй только там, где очередь пуста или смоук-мусор — на проде с активными pending/processing
-- сначала убедись, что воркер остановлен или очередь обработана.
--
-- psql:  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f scripts/outbox_truncate_restart_identity.sql

BEGIN;

TRUNCATE TABLE outbox_events RESTART IDENTITY;

COMMIT;

-- Проверка: строк 0, следующий INSERT получит id = 1
SELECT COUNT(*) AS outbox_rows FROM outbox_events;

-- Смоук-тест аутбокса (п. 4): битый paywall_access_restore_requested (user_id = 0, chat_id = 0).
--
-- Ожидание при ЗАПУЩЕННОМ боте ms_leo:
--   Сразу status = dead (invalid payload = non-retryable), last_error про invalid payload.
--   Раньше: DEFAULT next_attempt_at с (NOW() AT TIME ZONE 'Europe/Moscow')::timestamptz на БД с
--   session TimeZone=UTC ставил next_attempt_at в будущее — событие не попадало в claim до часов.
--   Явный next_attempt_at = NOW() + миграция 28 страхуют ручной INSERT.
--
-- Подключение: та же БД, что и у бота (DATABASE_URL).
--
-- Удалить тестовые строки после проверки:
--   DELETE FROM outbox_events WHERE aggregate_key LIKE 'smoke:%';

INSERT INTO outbox_events (event_type, aggregate_key, payload, status, next_attempt_at)
VALUES (
  'paywall_access_restore_requested',
  'smoke:invalid_paywall',
  '{"request_id": 0, "user_id": 0, "chat_id": 0}'::jsonb,
  'pending',
  NOW()
)
RETURNING id, event_type, aggregate_key, status, created_at;

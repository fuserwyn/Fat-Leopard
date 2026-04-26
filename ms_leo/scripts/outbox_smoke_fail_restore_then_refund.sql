-- Смоук: restore → мгновенный dead → автоматически вторая строка refund_requested (логика воркера).
--
-- Условия в коде (processOutboxEvent):
--   aggregate_key начинается с smoke:fail_restore
--   в payload user_id, chat_id, request_id все ненулевые
--
-- Безопасный вариант для refund-шага: request_id, которого НЕТ в paywall_access_requests —
-- воркер создаст refund_requested, затем переведёт её в dead: «refund request not found»
-- (без вызова ЮKassa / Stars).
--
-- Поменяй числа при необходимости. chat_id обычно = MONETIZED_CHAT_ID (для правдоподобия payload).

INSERT INTO outbox_events (event_type, aggregate_key, payload, status, next_attempt_at)
VALUES (
  'paywall_access_restore_requested',
  'smoke:fail_restore:demo',
  jsonb_build_object(
    'request_id', 999999999,
    'user_id', 166039118,
    'chat_id', -1003743863056
  ),
  'pending',
  NOW()
)
RETURNING id, event_type, aggregate_key, status;

-- После тика воркера проверь:
--   SELECT id, event_type, aggregate_key, status, last_error FROM outbox_events ORDER BY id DESC LIMIT 5;
-- Ожидание: первая строка dead (restore), вторая refund_requested (pending→dead или done).

-- Удаление смоука:
-- DELETE FROM outbox_events
-- WHERE aggregate_key LIKE 'smoke:fail_restore%' OR aggregate_key = 'refund_request:999999999';

-- Эмуляция строки refund_requested в outbox (как после enqueueRefundRequestedForRestoreFailure).
-- Полезно посмотреть claim → process → dead без правки кода.
--
-- Подставь request_id / user_id:
--   request_id — существующий id из paywall_access_requests (иначе воркер даст dead: refund request not found);
--   user_id — должен совпадать с paywall_access_requests.user_id для этой заявки.
--
-- aggregate_key с префиксом smoke: — уведомление OwnerID в личку при dead не шлётся (outboxEventIsTestNoise).
--
-- Пример: заявка id=10, user 166039118
--   (замени на свои значения из SELECT id, user_id, status FROM paywall_access_requests ORDER BY id DESC LIMIT 5;)

INSERT INTO outbox_events (event_type, aggregate_key, payload, status, next_attempt_at)
VALUES (
  'refund_requested',
  'smoke:refund_manual',
  jsonb_build_object(
    'request_id', 10,
    'user_id', 166039118,
    'reason', 'manual smoke: emulate enqueueRefundRequestedForRestoreFailure'
  ),
  'pending',
  NOW()
)
RETURNING id, event_type, aggregate_key, status;

-- Удаление после просмотра:
-- DELETE FROM outbox_events WHERE aggregate_key = 'smoke:refund_manual';

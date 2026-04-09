"""
ms_payments — микросервис вебхука ЮKassa для монорепозитория LeoPoacherBot.

Структура (слои):
- ``api/`` — HTTP: ``v1/views`` (контроллеры), ``dependencies`` (Depends);
- ``services/`` — бизнес-логика и внешние шлюзы (Telegram, проверка ЮKassa);
- ``repositories/`` — доступ к БД (основная + леджер платежей);
- ``domain/`` — DTO и чистые функции без I/O;
- ``core/`` — конфиг, пулы PostgreSQL и lifecycle.
"""

"""
Пулы подключений к PostgreSQL и синглтоны репозиториев на время процесса.
"""

from __future__ import annotations

import asyncio
import logging
from urllib.parse import urlparse

import asyncpg

from app.core.config import settings
from app.repositories.payment_ledger_repository import PaymentLedgerRepository
from app.repositories.paywall_repository import PaywallRepository

logger = logging.getLogger(__name__)

_main_pool: asyncpg.Pool | None = None
_ledger_repo: PaymentLedgerRepository | None = None


def _dsn_hint(dsn: str) -> str:
    """Часть DSN для логов без пароля."""
    try:
        p = urlparse(dsn)
        host = p.hostname or "?"
        port = p.port or 5432
        db = (p.path or "/").lstrip("/") or "?"
        return f"{p.scheme}://{host}:{port}/{db}"
    except Exception:
        return "(invalid DATABASE_URL)"


async def _create_pool_with_retry(
    dsn: str,
    *,
    min_size: int,
    max_size: int,
    label: str,
) -> asyncpg.Pool:
    hint = _dsn_hint(dsn)
    attempts = settings.db_connect_max_attempts
    delay = max(0.5, settings.db_connect_retry_delay_sec)
    last_error: BaseException | None = None
    for n in range(1, attempts + 1):
        try:
            pool = await asyncpg.create_pool(dsn, min_size=min_size, max_size=max_size)
            if n > 1:
                logger.info("%s: подключение с попытки %d — %s", label, n, hint)
            return pool
        except (
            OSError,
            ConnectionError,
            TimeoutError,
            asyncpg.PostgresError,
        ) as exc:
            last_error = exc
            logger.warning(
                "%s: попытка %d/%d — не удалось подключиться к %s: %s",
                label,
                n,
                attempts,
                hint,
                exc,
            )
            if n < attempts:
                await asyncio.sleep(delay * min(n, 5))
    logger.error(
        "%s: сдаюсь после %d попыток. Проверь DATABASE_URL: хост должен быть "
        "доступен из этого контейнера (не localhost, если БД в другом сервисе). DSN: %s",
        label,
        attempts,
        hint,
    )
    assert last_error is not None
    raise last_error


async def init_database() -> None:
    global _main_pool, _ledger_repo
    _main_pool = await _create_pool_with_retry(
        settings.database_url,
        min_size=1,
        max_size=5,
        label="main",
    )
    logger.info("Main database pool ready — %s", _dsn_hint(settings.database_url))

    if settings.payment_database_url:
        ledger_pool = await _create_pool_with_retry(
            settings.payment_database_url,
            min_size=1,
            max_size=3,
            label="payment_ledger",
        )
        _ledger_repo = PaymentLedgerRepository(ledger_pool)
        await _ledger_repo.ensure_schema()
        logger.info("Payment ledger pool ready — %s", _dsn_hint(settings.payment_database_url))
    else:
        _ledger_repo = None
        logger.info("PAYMENT_DATABASE_URL not set — ledger disabled")


async def shutdown_database() -> None:
    global _main_pool, _ledger_repo
    if _ledger_repo is not None:
        await _ledger_repo.close()
        _ledger_repo = None
    if _main_pool is not None:
        await _main_pool.close()
        _main_pool = None
    logger.info("Database pools closed")


def get_paywall_repository() -> PaywallRepository:
    if _main_pool is None:
        raise RuntimeError("Database not initialized")
    return PaywallRepository(_main_pool)


def get_ledger_repository() -> PaymentLedgerRepository | None:
    return _ledger_repo

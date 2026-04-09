"""
Пулы подключений к PostgreSQL и синглтоны репозиториев на время процесса.
"""

from __future__ import annotations

import logging

import asyncpg

from app.core.config import settings
from app.repositories.payment_ledger_repository import PaymentLedgerRepository
from app.repositories.paywall_repository import PaywallRepository

logger = logging.getLogger(__name__)

_main_pool: asyncpg.Pool | None = None
_ledger_repo: PaymentLedgerRepository | None = None


async def init_database() -> None:
    global _main_pool, _ledger_repo
    _main_pool = await asyncpg.create_pool(settings.database_url, min_size=1, max_size=5)
    logger.info("Main database pool ready")

    if settings.payment_database_url:
        ledger_pool = await asyncpg.create_pool(
            settings.payment_database_url,
            min_size=1,
            max_size=3,
        )
        _ledger_repo = PaymentLedgerRepository(ledger_pool)
        await _ledger_repo.ensure_schema()
        logger.info("Payment ledger pool ready")
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

"""
Репозиторий: отдельная БД учёта событий ЮKassa (таблица yookassa_payment_events).
"""

from __future__ import annotations

import logging

import asyncpg

logger = logging.getLogger(__name__)

_SCHEMA = """
CREATE TABLE IF NOT EXISTS yookassa_payment_events (
    id BIGSERIAL PRIMARY KEY,
    yookassa_payment_id TEXT NOT NULL UNIQUE,
    paywall_request_id BIGINT NOT NULL,
    user_telegram_id BIGINT NOT NULL,
    monetized_chat_id BIGINT NOT NULL,
    amount_minor INTEGER NOT NULL,
    currency TEXT NOT NULL,
    webhook_event TEXT NOT NULL DEFAULT 'payment.succeeded',
    main_db_synced_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_yk_pay_paywall_req
    ON yookassa_payment_events (paywall_request_id);
CREATE INDEX IF NOT EXISTS idx_yk_pay_user
    ON yookassa_payment_events (user_telegram_id);
CREATE INDEX IF NOT EXISTS idx_yk_pay_synced
    ON yookassa_payment_events (main_db_synced_at);
"""


class PaymentLedgerRepository:
    def __init__(self, pool: asyncpg.Pool) -> None:
        self._pool = pool

    async def close(self) -> None:
        await self._pool.close()

    async def ensure_schema(self) -> None:
        async with self._pool.acquire() as conn:
            await conn.execute(_SCHEMA)
        logger.info("Payment ledger schema ensured")

    async def upsert_webhook(
        self,
        yookassa_payment_id: str,
        paywall_request_id: int,
        user_telegram_id: int,
        monetized_chat_id: int,
        amount_minor: int,
        currency: str,
        webhook_event: str = "payment.succeeded",
    ) -> None:
        await self._pool.execute(
            """
            INSERT INTO yookassa_payment_events (
                yookassa_payment_id, paywall_request_id, user_telegram_id,
                monetized_chat_id, amount_minor, currency, webhook_event
            )
            VALUES ($1, $2, $3, $4, $5, $6, $7)
            ON CONFLICT (yookassa_payment_id) DO UPDATE SET
                paywall_request_id = EXCLUDED.paywall_request_id,
                user_telegram_id = EXCLUDED.user_telegram_id,
                monetized_chat_id = EXCLUDED.monetized_chat_id,
                amount_minor = EXCLUDED.amount_minor,
                currency = EXCLUDED.currency,
                webhook_event = EXCLUDED.webhook_event
            """,
            yookassa_payment_id,
            paywall_request_id,
            user_telegram_id,
            monetized_chat_id,
            amount_minor,
            currency,
            webhook_event,
        )

    async def mark_main_db_synced(self, yookassa_payment_id: str) -> None:
        await self._pool.execute(
            """
            UPDATE yookassa_payment_events
            SET main_db_synced_at = COALESCE(main_db_synced_at, NOW() AT TIME ZONE 'UTC')
            WHERE yookassa_payment_id = $1
            """,
            yookassa_payment_id,
        )

"""
Репозиторий: таблица paywall_access_requests (основная БД бота).
"""

from __future__ import annotations

from typing import Any

import asyncpg


class PaywallRepository:
    def __init__(self, pool: asyncpg.Pool) -> None:
        self._pool = pool

    async def get_by_id(self, req_id: int) -> dict[str, Any] | None:
        row = await self._pool.fetchrow(
            """
            SELECT id, user_id, monetized_chat_id, status, created_at, completed_at,
                   access_expires_at, telegram_payment_charge_id, total_amount_minor, currency
            FROM paywall_access_requests
            WHERE id = $1
            """,
            req_id,
        )
        return dict(row) if row else None

    async def complete_if_pending(
        self,
        req_id: int,
        user_id: int,
        monetized_chat_id: int,
        charge_id: str,
        amount_minor: int,
        currency: str,
    ) -> bool:
        """Атомарно закрывает заявку только если ``status = pending`` (как в Go)."""
        status = await self._pool.fetchval(
            """
            UPDATE paywall_access_requests
            SET status = 'completed',
                completed_at = NOW(),
                access_expires_at = NOW() + INTERVAL '30 days',
                telegram_payment_charge_id = $4,
                total_amount_minor = $5,
                currency = $6
            WHERE id = $1 AND user_id = $2 AND monetized_chat_id = $3 AND status = 'pending'
            RETURNING status
            """,
            req_id,
            user_id,
            monetized_chat_id,
            charge_id,
            amount_minor,
            currency,
        )
        return status == "completed"

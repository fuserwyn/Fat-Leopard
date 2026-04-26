"""
Репозиторий: таблица paywall_access_requests (основная БД бота).
"""

from __future__ import annotations

import logging
from datetime import datetime
from typing import Any
from zoneinfo import ZoneInfo

import asyncpg

logger = logging.getLogger(__name__)


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

    async def reactivate_returned_user(self, user_id: int, chat_id: int, username: str = "") -> None:
        """Дублирует ms_leo ReactivateReturnedUser — вебхук ЮKassa не проходит outbox paywall_access_restore_requested."""
        now = datetime.now(ZoneInfo("Europe/Moscow")).isoformat()
        un = (username or "").strip()
        row = await self._pool.fetchrow(
            """
            UPDATE training_state
            SET is_deleted = FALSE,
                lifecycle_status = 'active',
                xp = 42,
                achievement_count = 0,
                has_training_done = FALSE,
                has_sick_leave = FALSE,
                has_healthy = FALSE,
                timer_start_time = NULL,
                returned_at = (NOW() AT TIME ZONE 'Europe/Moscow'),
                return_count = COALESCE(return_count, 0) + 1,
                username = CASE WHEN NULLIF($3, '') IS NULL THEN username ELSE $3 END,
                updated_at = $4
            WHERE user_id = $1 AND chat_id = $2
            RETURNING user_id
            """,
            user_id,
            chat_id,
            un,
            now,
        )
        if row is not None:
            return
        try:
            await self._pool.execute(
                """
                INSERT INTO training_state (
                    user_id, username, chat_id, xp, streak_days, calorie_streak_days, cups_earned,
                    last_message, has_training_done, has_sick_leave, has_healthy, is_deleted,
                    timer_start_time, timezone_offset_from_moscow, achievement_count, return_count,
                    returned_at, lifecycle_status, created_at, updated_at
                ) VALUES (
                    $1, NULLIF($2, ''), $3, 42, 0, 0, 0,
                    $4, FALSE, FALSE, FALSE, FALSE,
                    NULL, 0, 0, 1,
                    (NOW() AT TIME ZONE 'Europe/Moscow'), 'active', $4, $4
                )
                """,
                user_id,
                un,
                chat_id,
                now,
            )
        except Exception:
            logger.exception(
                "reactivate_returned_user: insert training_state user=%s chat=%s",
                user_id,
                chat_id,
            )

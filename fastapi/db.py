from __future__ import annotations

import re
from typing import Any

import asyncpg

from config import settings

_paywall_payload_re = re.compile(r"^pw_(\d+)$")


def parse_paywall_payload(payload: str) -> int | None:
    payload = (payload or "").strip()
    m = _paywall_payload_re.match(payload)
    if not m:
        return None
    n = int(m.group(1))
    return n if n > 0 else None


def minor_units_from_yookassa_amount(amount_block: Any) -> tuple[int, str]:
    """YooKassa object.amount: { value: '100.00', currency: 'RUB' } -> kopeks, currency."""
    if not isinstance(amount_block, dict):
        return 0, ""
    cur = str(amount_block.get("currency") or "").strip().upper()
    raw = amount_block.get("value")
    try:
        val = float(str(raw).replace(",", "."))
    except (TypeError, ValueError):
        return 0, cur
    if cur == "RUB":
        return int(round(val * 100)), cur
    return int(round(val * 100)), cur


class DB:
    def __init__(self, pool: asyncpg.Pool):
        self.pool = pool

    @classmethod
    async def connect(cls) -> DB:
        pool = await asyncpg.create_pool(settings.database_url, min_size=1, max_size=5)
        return cls(pool)

    async def close(self) -> None:
        await self.pool.close()

    async def get_paywall_request(self, req_id: int) -> dict[str, Any] | None:
        row = await self.pool.fetchrow(
            """
            SELECT id, user_id, monetized_chat_id, status, created_at, completed_at,
                   access_expires_at, telegram_payment_charge_id, total_amount_minor, currency
            FROM paywall_access_requests
            WHERE id = $1
            """,
            req_id,
        )
        return dict(row) if row else None

    async def complete_paywall_request(
        self,
        req_id: int,
        user_id: int,
        monetized_chat_id: int,
        charge_id: str,
        amount_minor: int,
        currency: str,
    ) -> bool:
        """Same rules as Go CompletePaywallAccessRequest — only pending rows update."""
        status = await self.pool.fetchval(
            """
            UPDATE paywall_access_requests
            SET status = 'completed',
                completed_at = NOW() AT TIME ZONE 'Europe/Moscow',
                access_expires_at = (NOW() AT TIME ZONE 'Europe/Moscow') + INTERVAL '30 days',
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


_db: DB | None = None


async def get_db() -> DB:
    global _db
    if _db is None:
        _db = await DB.connect()
    return _db


async def shutdown_db() -> None:
    global _db
    if _db is not None:
        await _db.close()
        _db = None

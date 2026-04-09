"""
Сервис обработки успешной оплаты ЮKassa: валидация, БД бота, леджер, Telegram.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass

from fastapi import HTTPException

from app.core.config import Settings
from app.domain.paywall import minor_units_from_yookassa_amount, parse_paywall_payload
from app.domain.schemas.webhook import PaymentNotification
from app.repositories.payment_ledger_repository import PaymentLedgerRepository
from app.repositories.paywall_repository import PaywallRepository
from app.services.telegram_gateway import TelegramGateway

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class WebhookOutcome:
    """Результат обработки вебхука для HTTP-ответа."""

    status_code: int
    body: dict


def _metadata_string(meta: dict, *keys: str) -> str:
    for k in keys:
        v = meta.get(k)
        if v is None:
            continue
        return str(v).strip()
    return ""


class PaymentWebhookService:
    def __init__(
        self,
        paywall_repo: PaywallRepository,
        ledger_repo: PaymentLedgerRepository | None,
        telegram: TelegramGateway,
        app_settings: Settings,
    ) -> None:
        self._paywall = paywall_repo
        self._ledger = ledger_repo
        self._telegram = telegram
        self._settings = app_settings

    async def handle_payment_succeeded(self, notification: PaymentNotification) -> WebhookOutcome:
        obj = notification.object or {}
        payment_id = str(obj.get("id") or "").strip()
        if not payment_id:
            raise HTTPException(status_code=400, detail="payment id missing")

        meta = obj.get("metadata") or {}
        if not isinstance(meta, dict):
            meta = {}

        user_raw = _metadata_string(meta, "user_telegram_id", "user_telegramId")
        payload_str = _metadata_string(meta, "invoice_payload", "invoicePayload")

        if not user_raw:
            logger.warning("yookassa webhook: no user_telegram_id in metadata, payment=%s", payment_id)
            return WebhookOutcome(400, {"status": "user_telegram_id missing"})

        try:
            user_tid = int(user_raw)
        except ValueError:
            return WebhookOutcome(400, {"status": "invalid user_telegram_id"})

        req_id = parse_paywall_payload(payload_str)
        if req_id is None:
            logger.warning(
                "yookassa webhook: invoice_payload must be pw_<id>, got=%r",
                payload_str,
            )
            return WebhookOutcome(
                400,
                {"status": "invalid invoice_payload, expected pw_<request_id>"},
            )

        if self._settings.monetized_chat_id == 0:
            logger.error("MONETIZED_CHAT_ID is not set")
            raise HTTPException(status_code=500, detail="server misconfigured")

        rec = await self._paywall.get_by_id(req_id)
        if not rec:
            return WebhookOutcome(404, {"status": "paywall request not found"})

        if int(rec["user_id"]) != user_tid:
            logger.warning(
                "yookassa webhook: user mismatch payment=%s db_user=%s meta_user=%s",
                payment_id,
                rec["user_id"],
                user_tid,
            )
            return WebhookOutcome(403, {"status": "user mismatch"})

        if int(rec["monetized_chat_id"]) != self._settings.monetized_chat_id:
            logger.warning(
                "yookassa webhook: chat mismatch req=%s db_chat=%s env_chat=%s",
                req_id,
                rec["monetized_chat_id"],
                self._settings.monetized_chat_id,
            )
            return WebhookOutcome(403, {"status": "chat mismatch"})

        amount_minor, currency = minor_units_from_yookassa_amount(obj.get("amount"))
        if amount_minor <= 0 or not currency:
            logger.warning("yookassa webhook: missing amount, payment=%s", payment_id)
            amount_minor = int(rec["total_amount_minor"] or 0)
            currency = str(rec["currency"] or "RUB")
            if amount_minor <= 0:
                amount_minor = 1

        chat_id = int(rec["monetized_chat_id"])

        if self._ledger:
            await self._ledger.upsert_webhook(
                payment_id,
                req_id,
                user_tid,
                chat_id,
                amount_minor,
                currency,
                notification.event,
            )

        if rec["status"] == "completed":
            logger.info("yookassa webhook: already completed payment=%s req=%s", payment_id, req_id)
            if self._ledger:
                await self._ledger.mark_main_db_synced(payment_id)
            return WebhookOutcome(200, {"status": "already processed"})

        if rec["status"] != "pending":
            return WebhookOutcome(409, {"status": f"unexpected status {rec['status']}"})

        updated = await self._paywall.complete_if_pending(
            req_id,
            user_tid,
            chat_id,
            payment_id,
            amount_minor,
            currency,
        )
        if not updated:
            logger.info("yookassa webhook: complete raced payment=%s req=%s", payment_id, req_id)
            if self._ledger:
                await self._ledger.mark_main_db_synced(payment_id)
            return WebhookOutcome(200, {"status": "already processed"})

        if self._ledger:
            await self._ledger.mark_main_db_synced(payment_id)

        approved = await self._telegram.approve_chat_join_request(chat_id, user_tid)
        if approved:
            await self._telegram.send_message(
                user_tid,
                "✅ Оплата через ЮKassa принята, доступ к группе открыт на 30 дней. Если заявка ещё висит — она должна одобриться автоматически.",
            )
        else:
            await self._telegram.send_message(
                user_tid,
                "✅ Оплата принята, доступ записан. Подай заявку в группу ещё раз или открой пригласительную ссылку — бот одобрит вступление.",
            )

        logger.info("yookassa webhook: completed payment=%s req=%s user=%s", payment_id, req_id, user_tid)
        return WebhookOutcome(200, {"status": "success"})

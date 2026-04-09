from __future__ import annotations

import logging

from fastapi import APIRouter, HTTPException, Request
from fastapi.responses import JSONResponse

from auth_yookassa import verify_yookassa_request
from config import settings
from db import get_db, minor_units_from_yookassa_amount, parse_paywall_payload
from schemas.schemas import PaymentNotification
from telegram_notify import approve_join_request, send_dm

logger = logging.getLogger(__name__)

router = APIRouter(tags=["payment"])


def _meta_str(meta: dict, *keys: str) -> str:
    for k in keys:
        v = meta.get(k)
        if v is None:
            continue
        return str(v).strip()
    return ""


@router.post("/webhook/payment")
async def payment_webhook(request: Request, notification: PaymentNotification):
    await verify_yookassa_request(request)

    if notification.event != "payment.succeeded":
        return JSONResponse({"status": "event not handled"}, status_code=400)

    obj = notification.object or {}
    payment_id = str(obj.get("id") or "").strip()
    if not payment_id:
        raise HTTPException(status_code=400, detail="payment id missing")

    meta = obj.get("metadata") or {}
    if not isinstance(meta, dict):
        meta = {}

    user_raw = _meta_str(meta, "user_telegram_id", "user_telegramId")
    payload_str = _meta_str(meta, "invoice_payload", "invoicePayload")

    if not user_raw:
        logger.warning("yookassa webhook: no user_telegram_id in metadata, payment=%s", payment_id)
        return JSONResponse({"status": "user_telegram_id missing"}, status_code=400)

    try:
        user_tid = int(user_raw)
    except ValueError:
        return JSONResponse({"status": "invalid user_telegram_id"}, status_code=400)

    req_id = parse_paywall_payload(payload_str)
    if req_id is None:
        logger.warning(
            "yookassa webhook: invoice_payload must be pw_<id> like Telegram invoice, got=%r",
            payload_str,
        )
        return JSONResponse({"status": "invalid invoice_payload, expected pw_<request_id>"}, status_code=400)

    if settings.monetized_chat_id == 0:
        logger.error("MONETIZED_CHAT_ID is not set")
        raise HTTPException(status_code=500, detail="server misconfigured")

    db = await get_db()
    rec = await db.get_paywall_request(req_id)
    if not rec:
        return JSONResponse({"status": "paywall request not found"}, status_code=404)

    if int(rec["user_id"]) != user_tid:
        logger.warning(
            "yookassa webhook: user mismatch payment=%s db_user=%s meta_user=%s",
            payment_id,
            rec["user_id"],
            user_tid,
        )
        return JSONResponse({"status": "user mismatch"}, status_code=403)

    if int(rec["monetized_chat_id"]) != settings.monetized_chat_id:
        logger.warning(
            "yookassa webhook: chat mismatch req=%s db_chat=%s env_chat=%s",
            req_id,
            rec["monetized_chat_id"],
            settings.monetized_chat_id,
        )
        return JSONResponse({"status": "chat mismatch"}, status_code=403)

    if rec["status"] == "completed":
        logger.info("yookassa webhook: already completed payment=%s req=%s", payment_id, req_id)
        return {"status": "already processed"}

    if rec["status"] != "pending":
        return JSONResponse({"status": f"unexpected status {rec['status']}"}, status_code=409)

    amount_minor, currency = minor_units_from_yookassa_amount(obj.get("amount"))
    if amount_minor <= 0 or not currency:
        logger.warning("yookassa webhook: missing amount, payment=%s", payment_id)
        amount_minor = int(rec["total_amount_minor"] or 0)
        currency = str(rec["currency"] or "RUB")
        if amount_minor <= 0:
            amount_minor = 1

    chat_id = int(rec["monetized_chat_id"])
    updated = await db.complete_paywall_request(
        req_id,
        user_tid,
        chat_id,
        payment_id,
        amount_minor,
        currency,
    )
    if not updated:
        logger.info("yookassa webhook: complete raced or not pending payment=%s req=%s", payment_id, req_id)
        return {"status": "already processed"}

    approved = await approve_join_request(chat_id, user_tid)
    if approved:
        await send_dm(
            user_tid,
            "✅ Оплата через ЮKassa принята, доступ к группе открыт на 30 дней. Если заявка ещё висит — она должна одобриться автоматически.",
        )
    else:
        await send_dm(
            user_tid,
            "✅ Оплата принята, доступ записан. Подай заявку в группу ещё раз или открой пригласительную ссылку — бот одобрит вступление.",
        )

    logger.info("yookassa webhook: completed payment=%s req=%s user=%s", payment_id, req_id, user_tid)
    return {"status": "success"}

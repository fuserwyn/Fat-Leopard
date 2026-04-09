"""
Контроллер вебхука платежей (view).
"""

import logging
from typing import Annotated

from fastapi import APIRouter, Body, Depends, Request
from fastapi.responses import JSONResponse

from app.api.dependencies import get_payment_webhook_service, get_yookassa_auth_service
from app.domain.schemas.webhook import PaymentNotification
from app.services.payment_webhook_service import PaymentWebhookService
from app.services.yookassa_auth import YooKassaAuthService

logger = logging.getLogger(__name__)

router = APIRouter(tags=["payment"])

# Честный пример для Swagger; реальный object у ЮKassa больше полей — они просто игнорируются.
_YOOKASSA_WEBHOOK_EXAMPLE = {
    "type": "notification",
    "event": "payment.succeeded",
    "object": {
        "id": "2d7f3e7a-000f-5000-9000-1baf0c000000",
        "status": "succeeded",
        "paid": True,
        "amount": {"value": "100.00", "currency": "RUB"},
        "description": "Доступ в группу",
        "metadata": {
            "user_telegram_id": "7738691355",
            "invoice_payload": "pw_1",
        },
    },
}


@router.post("/webhook/payment")
async def payment_webhook(
    request: Request,
    notification: Annotated[
        PaymentNotification,
        Body(
            openapi_examples={
                "yookassa_payment_succeeded": {
                    "summary": "ЮKassa: payment.succeeded",
                    "description": "Реальный вебхук содержит больше полей в object — так и должно быть.",
                    "value": _YOOKASSA_WEBHOOK_EXAMPLE,
                }
            }
        ),
    ],
    auth: Annotated[YooKassaAuthService, Depends(get_yookassa_auth_service)],
    service: Annotated[PaymentWebhookService, Depends(get_payment_webhook_service)],
):
    logger.info("yookassa webhook: запрос получен, event=%s", notification.event)
    await auth.verify_webhook_request(request)

    if notification.event != "payment.succeeded":
        logger.info(
            "yookassa webhook: игнор события %s (нужен payment.succeeded)",
            notification.event,
        )
        return JSONResponse({"status": "event not handled"}, status_code=400)

    outcome = await service.handle_payment_succeeded(notification)
    logger.info(
        "yookassa webhook: обработка завершена, http=%s detail=%s",
        outcome.status_code,
        outcome.body,
    )
    if outcome.status_code != 200:
        return JSONResponse(outcome.body, status_code=outcome.status_code)
    return outcome.body

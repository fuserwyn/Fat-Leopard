"""
Контроллер вебхука платежей (view).
"""

import logging
from typing import Annotated

from fastapi import APIRouter, Depends, Request
from fastapi.responses import JSONResponse

from app.api.dependencies import get_payment_webhook_service, get_yookassa_auth_service
from app.domain.schemas.webhook import PaymentNotification
from app.services.payment_webhook_service import PaymentWebhookService
from app.services.yookassa_auth import YooKassaAuthService

logger = logging.getLogger(__name__)

router = APIRouter(tags=["payment"])


@router.post("/webhook/payment")
async def payment_webhook(
    request: Request,
    notification: PaymentNotification,
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

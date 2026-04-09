"""
FastAPI Depends: сборка сервисов и репозиториев.
"""

from app.core.config import settings
from app.core.database import get_ledger_repository, get_paywall_repository
from app.services.payment_webhook_service import PaymentWebhookService
from app.services.telegram_gateway import TelegramGateway
from app.services.yookassa_auth import YooKassaAuthService


def get_yookassa_auth_service() -> YooKassaAuthService:
    return YooKassaAuthService(settings)


def get_payment_webhook_service() -> PaymentWebhookService:
    return PaymentWebhookService(
        get_paywall_repository(),
        get_ledger_repository(),
        TelegramGateway(settings),
        settings,
    )

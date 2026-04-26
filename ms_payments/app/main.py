"""
Точка входа FastAPI: приложение, lifespan, корневые маршруты.
"""

import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI
from starlette.requests import Request

from app.api.v1.router import api_router
from app.api.v1.views import payment as payment_views
from app.core.database import init_database, shutdown_database
from app.core.log_sanitize import install_log_redaction

logging.basicConfig(level=logging.INFO)
install_log_redaction()
logger = logging.getLogger(__name__)
webhook_logger = logging.getLogger("app.webhook")


@asynccontextmanager
async def lifespan(_: FastAPI):
    # После старта uvicorn на корне могут появиться свои хендлеры — повторно подключаем фильтр.
    install_log_redaction()
    await init_database()
    logger.info(
        "ЮKassa: POST …/api/v1/webhook/payment на ЭТОТ сервис (ms_payments). "
        "Открой в браузере GET / — должен быть JSON service=ms_payments; если не он — домен Railway ведёт на другой сервис."
    )
    yield
    await shutdown_database()


def create_app() -> FastAPI:
    application = FastAPI(title="ms_payments — YooKassa webhook", lifespan=lifespan)
    application.include_router(api_router, prefix="/api/v1")
    # Совместимость со старыми инструкциями / docker: тот же хендлер без префикса.
    application.include_router(payment_views.router, include_in_schema=False)

    @application.middleware("http")
    async def log_webhook_requests(request: Request, call_next):
        path = request.url.path
        if "/webhook" in path:
            webhook_logger.info(
                "HTTP %s %s (client=%s, content-type=%s)",
                request.method,
                path,
                request.client.host if request.client else "?",
                request.headers.get("content-type", ""),
            )
        response = await call_next(request)
        if "/webhook" in path:
            webhook_logger.info(
                "HTTP %s %s -> %s",
                request.method,
                path,
                response.status_code,
            )
        return response

    @application.get("/health")
    async def health():
        return {"ok": True}

    @application.get("/")
    async def whoami():
        """Проверка, что публичный URL Railway смотрит на ms_payments, а не на бота."""
        return {
            "service": "ms_payments",
            "yookassa_webhook_post": "/api/v1/webhook/payment",
            "legacy_webhook_post": "/webhook/payment",
            "health": "/health",
        }

    return application


app = create_app()

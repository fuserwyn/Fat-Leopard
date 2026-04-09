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

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)
webhook_logger = logging.getLogger("app.webhook")


@asynccontextmanager
async def lifespan(_: FastAPI):
    await init_database()
    logger.info(
        "ЮKassa: URL уведомлений POST …/api/v1/webhook/payment (или старый …/webhook/payment). "
        "HTTPS, публичный Railway. Пока сюда не приходит POST — paywall останется pending."
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

    return application


app = create_app()

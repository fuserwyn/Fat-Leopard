"""
Точка входа FastAPI: приложение, lifespan, корневые маршруты.
"""

import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app.api.v1.router import api_router
from app.core.database import init_database, shutdown_database

logging.basicConfig(level=logging.INFO)


@asynccontextmanager
async def lifespan(_: FastAPI):
    await init_database()
    yield
    await shutdown_database()


def create_app() -> FastAPI:
    application = FastAPI(title="ms_payments — YooKassa webhook", lifespan=lifespan)
    application.include_router(api_router)

    @application.get("/health")
    async def health():
        return {"ok": True}

    return application


app = create_app()

import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI

from db import shutdown_db
from routers.payment import router as payment_router

logging.basicConfig(level=logging.INFO)


@asynccontextmanager
async def lifespan(_: FastAPI):
    yield
    await shutdown_db()


app = FastAPI(title="LeoPoacherBot payment webhook", lifespan=lifespan)
app.include_router(payment_router)


@app.get("/health")
async def health():
    return {"ok": True}

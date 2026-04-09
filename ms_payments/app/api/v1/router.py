"""Агрегация маршрутов v1."""

from fastapi import APIRouter

from app.api.v1.views import payment

api_router = APIRouter()
api_router.include_router(payment.router)

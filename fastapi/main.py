"""Обратная совместимость: ``uvicorn main:app`` из каталога ``fastapi``."""

from app.main import app

__all__ = ["app"]

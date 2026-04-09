"""Реэкспорт: ``uvicorn main:app`` из каталога ``ms_payments``."""

from app.main import app

__all__ = ["app"]

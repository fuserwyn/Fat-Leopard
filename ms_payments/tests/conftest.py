"""Фикстуры: приложение без реального подключения к PostgreSQL."""

from __future__ import annotations

from collections.abc import Generator
from unittest.mock import AsyncMock, patch

import pytest
from fastapi.testclient import TestClient


@pytest.fixture
def client() -> Generator[TestClient, None, None]:
    with (
        patch("app.main.init_database", new_callable=AsyncMock),
        patch("app.main.shutdown_database", new_callable=AsyncMock),
    ):
        from app.main import create_app

        app = create_app()
        with TestClient(app) as c:
            yield c

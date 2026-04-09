"""
Проверка подлинности HTTP-запроса вебхука ЮKassa (опциональный Basic Auth).
"""

from __future__ import annotations

import base64
import logging
import secrets

from fastapi import HTTPException, Request

from app.core.config import Settings

logger = logging.getLogger(__name__)


class YooKassaAuthService:
    def __init__(self, settings: Settings) -> None:
        self._settings = settings

    async def verify_webhook_request(self, request: Request) -> None:
        if not self._settings.yookassa_webhook_verify_basic_auth:
            logger.debug("yookassa webhook: Basic auth выключен (YOOKASSA_WEBHOOK_VERIFY_BASIC_AUTH)")
            return
        if not self._settings.yookassa_shop_id or not self._settings.yookassa_secret_key:
            logger.info(
                "yookassa webhook: YOOKASSA_SHOP_ID/SECRET пусты — проверка Basic Auth пропущена"
            )
            return
        auth = request.headers.get("authorization") or ""
        if not auth.startswith("Basic "):
            logger.warning(
                "yookassa webhook: 401 — нет заголовка Basic. Включи HTTP Basic в ЛК ЮKassa для этого URL "
                "(логин=shopId, пароль=secret_key) или отключи проверку: YOOKASSA_WEBHOOK_VERIFY_BASIC_AUTH=false"
            )
            raise HTTPException(status_code=401, detail="missing basic auth")
        try:
            raw = base64.b64decode(auth[6:].strip(), validate=False).decode("utf-8")
        except Exception as exc:
            raise HTTPException(status_code=401, detail="invalid auth encoding") from exc
        if ":" not in raw:
            raise HTTPException(status_code=401, detail="invalid auth format")
        got_id, _, got_secret = raw.partition(":")
        if not secrets.compare_digest(got_id.encode(), self._settings.yookassa_shop_id.encode()):
            raise HTTPException(status_code=401, detail="invalid credentials")
        if not secrets.compare_digest(got_secret.encode(), self._settings.yookassa_secret_key.encode()):
            raise HTTPException(status_code=401, detail="invalid credentials")

"""
Исходящие вызовы Telegram Bot API.
"""

from __future__ import annotations

import logging

import httpx

from app.core.config import Settings

logger = logging.getLogger(__name__)


class TelegramGateway:
    def __init__(self, settings: Settings) -> None:
        self._token = settings.bot_token

    async def approve_chat_join_request(self, chat_id: int, user_id: int) -> bool:
        if not self._token:
            logger.error("bot token empty, cannot approve join request")
            return False
        url = f"https://api.telegram.org/bot{self._token}/approveChatJoinRequest"
        async with httpx.AsyncClient(timeout=30.0) as client:
            r = await client.post(url, json={"chat_id": chat_id, "user_id": user_id})
            data = r.json()
        if not data.get("ok"):
            logger.warning("approveChatJoinRequest failed: %s", data)
            return False
        return True

    async def send_message(self, chat_id: int, text: str) -> None:
        if not self._token:
            return
        url = f"https://api.telegram.org/bot{self._token}/sendMessage"
        async with httpx.AsyncClient(timeout=30.0) as client:
            r = await client.post(url, json={"chat_id": chat_id, "text": text})
            data = r.json()
        if not data.get("ok"):
            logger.warning("sendMessage failed: %s", data)

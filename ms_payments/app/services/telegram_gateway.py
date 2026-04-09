"""
Исходящие вызовы Telegram Bot API.
"""

from __future__ import annotations

import logging
from typing import Any

import httpx

from app.core.config import Settings

logger = logging.getLogger(__name__)


class TelegramGateway:
    def __init__(self, settings: Settings) -> None:
        self._token = settings.bot_token

    async def _post(self, method: str, payload: dict[str, Any]) -> dict[str, Any]:
        url = f"https://api.telegram.org/bot{self._token}/{method}"
        async with httpx.AsyncClient(timeout=30.0) as client:
            r = await client.post(url, json=payload)
            return r.json()

    async def create_chat_invite_link(
        self,
        chat_id: int,
        *,
        creates_join_request: bool,
    ) -> str | None:
        """Дополнительная ссылка в группу. Без заявок — member_limit=1 (по сути одно приглашение)."""
        if not self._token:
            logger.error("bot token empty, cannot create invite link")
            return None
        body: dict[str, Any] = {
            "chat_id": chat_id,
            "creates_join_request": creates_join_request,
        }
        if not creates_join_request:
            body["member_limit"] = 1
        data = await self._post("createChatInviteLink", body)
        if not data.get("ok"):
            logger.warning("createChatInviteLink failed: %s", data)
            return None
        result = data.get("result") or {}
        link = str(result.get("invite_link") or "").strip()
        return link or None

    async def approve_chat_join_request(self, chat_id: int, user_id: int) -> bool:
        if not self._token:
            logger.error("bot token empty, cannot approve join request")
            return False
        data = await self._post(
            "approveChatJoinRequest",
            {"chat_id": chat_id, "user_id": user_id},
        )
        if not data.get("ok"):
            logger.warning("approveChatJoinRequest failed: %s", data)
            return False
        return True

    async def send_message(
        self,
        chat_id: int,
        text: str,
        *,
        button_text: str | None = None,
        button_url: str | None = None,
    ) -> None:
        if not self._token:
            return
        body: dict[str, Any] = {"chat_id": chat_id, "text": text}
        if button_text and button_url:
            body["reply_markup"] = {
                "inline_keyboard": [
                    [{"text": button_text, "url": button_url}],
                ]
            }
        data = await self._post("sendMessage", body)
        if not data.get("ok"):
            logger.warning("sendMessage failed: %s", data)

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

    async def _create_chat_invite_link_once(
        self,
        chat_id: int,
        *,
        creates_join_request: bool,
    ) -> str | None:
        """Один вызов createChatInviteLink. Без заявок — member_limit=1 (одно вступление по ссылке)."""
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

    async def create_chat_invite_link_best_effort(
        self,
        chat_id: int,
        *,
        primary_creates_join_request: bool,
    ) -> tuple[str | None, bool]:
        """Подбор рабочей ссылки: как в ms_leo — сначала MONETIZED_INVITE_CREATES_JOIN_REQUEST, затем наоборот."""
        for creates_jr in (primary_creates_join_request, not primary_creates_join_request):
            link = await self._create_chat_invite_link_once(
                chat_id, creates_join_request=creates_jr
            )
            if link:
                return link, creates_jr
        return None, primary_creates_join_request

    async def unban_chat_member(self, chat_id: int, user_id: int) -> None:
        """Снимает бан на вход (после кика за неактивность ms_leo ставит постоянный бан). Как paywallUnbanUserFromMonetizedGroup."""
        if not self._token:
            logger.error("bot token empty, cannot unbanChatMember")
            return
        data = await self._post(
            "unbanChatMember",
            {"chat_id": chat_id, "user_id": user_id, "only_if_banned": False},
        )
        if not data.get("ok"):
            logger.warning("unbanChatMember failed: %s", data)

    async def approve_chat_join_request(self, chat_id: int, user_id: int) -> bool:
        if not self._token:
            logger.error("bot token empty, cannot approve join request")
            return False
        data = await self._post(
            "approveChatJoinRequest",
            {"chat_id": chat_id, "user_id": user_id},
        )
        if not data.get("ok"):
            desc = str((data.get("description") or "")).strip()
            if "HIDE_REQUESTER_MISSING" in desc:
                logger.info(
                    "approveChatJoinRequest: нет активной заявки на вступление (ожидаемо до перехода по ссылке): %s",
                    data,
                )
            else:
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
            logger.error(
                "bot token empty, cannot sendMessage (set FAT_LEOPARD_API_TOKEN or API_TOKEN on ms_payments)"
            )
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

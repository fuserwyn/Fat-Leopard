from __future__ import annotations

import logging

import httpx

from config import settings

logger = logging.getLogger(__name__)


async def approve_join_request(chat_id: int, user_id: int) -> bool:
    token = settings.bot_token
    if not token:
        logger.error("bot token empty, cannot approve join request")
        return False
    url = f"https://api.telegram.org/bot{token}/approveChatJoinRequest"
    async with httpx.AsyncClient(timeout=30.0) as client:
        r = await client.post(url, json={"chat_id": chat_id, "user_id": user_id})
        data = r.json()
    if not data.get("ok"):
        logger.warning("approveChatJoinRequest failed: %s", data)
        return False
    return True


async def send_dm(chat_id: int, text: str) -> None:
    token = settings.bot_token
    if not token:
        return
    url = f"https://api.telegram.org/bot{token}/sendMessage"
    async with httpx.AsyncClient(timeout=30.0) as client:
        r = await client.post(
            url,
            json={"chat_id": chat_id, "text": text},
        )
        data = r.json()
    if not data.get("ok"):
        logger.warning("sendMessage failed: %s", data)

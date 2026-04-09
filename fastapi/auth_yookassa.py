from __future__ import annotations

import base64
import secrets

from fastapi import HTTPException, Request

from config import settings


def _const_eq(a: str, b: str) -> bool:
    return secrets.compare_digest(a.encode(), b.encode())


async def verify_yookassa_request(request: Request) -> None:
    """Optional: YooKassa sends Basic auth shopId:secretKey on webhooks."""
    sid = settings.yookassa_shop_id
    key = settings.yookassa_secret_key
    if not sid or not key:
        return
    auth = request.headers.get("authorization") or ""
    if not auth.startswith("Basic "):
        raise HTTPException(status_code=401, detail="missing basic auth")
    try:
        raw = base64.b64decode(auth[6:].strip(), validate=False).decode("utf-8")
    except Exception as exc:
        raise HTTPException(status_code=401, detail="invalid auth encoding") from exc
    if ":" not in raw:
        raise HTTPException(status_code=401, detail="invalid auth format")
    got_id, _, got_secret = raw.partition(":")
    if not _const_eq(got_id, sid) or not _const_eq(got_secret, key):
        raise HTTPException(status_code=401, detail="invalid credentials")

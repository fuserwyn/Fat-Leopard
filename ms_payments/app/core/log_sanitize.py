"""
Санитизация логов: токен бота в URL Telegram API не должен попадать в stdout/файлы.
"""

from __future__ import annotations

import logging
import re

# https://api.telegram.org/bot<token>/method — токен между "bot" и следующим "/"
# Токен — всё между литералами "bot" и "/" (без пробелов и слэшей).
_BOT_API_URL = re.compile(r"(https://api\.telegram\.org/)bot[^/\s]+(?=/)", re.IGNORECASE)


def redact_telegram_bot_urls(text: str) -> str:
    if not text:
        return text
    return _BOT_API_URL.sub(r"\1bot<redacted>", text)


class SecretRedactingFilter(logging.Filter):
    """Подменяет сообщение записи, если в нём виден URL с токеном Bot API."""

    def filter(self, record: logging.LogRecord) -> bool:  # noqa: A003 — logging API
        try:
            if record.args:
                try:
                    rendered = record.msg % record.args  # type: ignore[str-format]
                except (TypeError, ValueError):
                    return True
            else:
                rendered = str(record.msg)
        except Exception:
            return True
        redacted = redact_telegram_bot_urls(rendered)
        if redacted != rendered:
            record.msg = redacted
            record.args = ()
        return True


def install_log_redaction() -> None:
    """После logging.basicConfig: тише httpx и фильтр на корневые хендлеры (идемпотентно)."""
    logging.getLogger("httpx").setLevel(logging.WARNING)
    logging.getLogger("httpcore").setLevel(logging.WARNING)
    root = logging.getLogger()
    for handler in root.handlers:
        if any(isinstance(f, SecretRedactingFilter) for f in handler.filters):
            continue
        handler.addFilter(SecretRedactingFilter())

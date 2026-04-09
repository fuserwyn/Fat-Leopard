"""
Конфигурация приложения (переменные окружения).
"""

import os

from dotenv import load_dotenv

load_dotenv()


def _token() -> str:
    return os.getenv("FAT_LEOPARD_API_TOKEN", "") or os.getenv("API_TOKEN", "")


def _float_env(name: str, default: float) -> float:
    raw = os.getenv(name, "").strip()
    if not raw:
        return default
    try:
        return float(raw.replace(",", "."))
    except ValueError:
        return default


def _int(name: str, default: int = 0) -> int:
    raw = os.getenv(name, "")
    if raw == "":
        return default
    try:
        return int(raw)
    except ValueError:
        return default


def _bool_env(name: str, default: bool = True) -> bool:
    raw = os.getenv(name, "").strip().lower()
    if raw == "":
        return default
    return raw not in ("false", "0", "no")


def _normalize_postgres_url(url: str) -> str:
    u = url.strip()
    if u.startswith("postgres://"):
        u = "postgresql://" + u[len("postgres://") :]
    return u


class Settings:
    """Настройки, общие для всех слоёв."""

    database_url: str = _normalize_postgres_url(
        os.getenv(
            "DATABASE_URL",
            "postgresql://postgres:password@localhost:5432/leo_bot_db?sslmode=disable",
        )
    )
    payment_database_url: str | None = (
        _normalize_postgres_url(v)
        if (v := os.getenv("PAYMENT_DATABASE_URL", "").strip())
        else None
    )
    # Повторы при старте (Docker/Railway: Postgres ещё не слушает или сеть не готова).
    db_connect_max_attempts: int = max(1, _int("DB_CONNECT_MAX_ATTEMPTS", 30))
    db_connect_retry_delay_sec: float = _float_env("DB_CONNECT_RETRY_DELAY_SEC", 2.0)
    bot_token: str = _token()
    monetized_chat_id: int = _int("MONETIZED_CHAT_ID", 0)
    yookassa_shop_id: str = os.getenv("YOOKASSA_SHOP_ID", "").strip()
    yookassa_secret_key: str = os.getenv("YOOKASSA_SECRET_KEY", "").strip()
    # True: требовать Authorization: Basic (логин shopId, пароль secret_key) — как в ЛК ЮKassa при включённой HTTP Basic для уведомлений.
    # False: не проверять (удобно, если в ЛК Basic не включён; на проде лучше включить Basic и оставить true).
    # По умолчанию false: ЮKassa часто шлёт уведомления без Basic, хотя shop/secret заданы для API.
    yookassa_webhook_verify_basic_auth: bool = _bool_env(
        "YOOKASSA_WEBHOOK_VERIFY_BASIC_AUTH", False
    )
    # Как в ms_leo: True = ссылка на заявку (member_limit в Telegram задать нельзя).
    paywall_invite_creates_join_request: bool = _bool_env(
        "MONETIZED_INVITE_CREATES_JOIN_REQUEST", True
    )


settings = Settings()

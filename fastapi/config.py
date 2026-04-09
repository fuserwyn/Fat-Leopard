import os

from dotenv import load_dotenv

load_dotenv()


def _token() -> str:
    return os.getenv("FAT_LEOPARD_API_TOKEN", "") or os.getenv("API_TOKEN", "")


def _int(name: str, default: int = 0) -> int:
    raw = os.getenv(name, "")
    if raw == "":
        return default
    try:
        return int(raw)
    except ValueError:
        return default


class Settings:
    database_url: str = os.getenv(
        "DATABASE_URL",
        "postgresql://postgres:password@localhost:5432/leo_bot_db?sslmode=disable",
    )
    bot_token: str = _token()
    monetized_chat_id: int = _int("MONETIZED_CHAT_ID", 0)
    yookassa_shop_id: str = os.getenv("YOOKASSA_SHOP_ID", "").strip()
    yookassa_secret_key: str = os.getenv("YOOKASSA_SECRET_KEY", "").strip()


settings = Settings()

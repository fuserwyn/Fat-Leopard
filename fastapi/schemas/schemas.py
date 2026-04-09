from pydantic import BaseModel, ConfigDict


class PaymentNotification(BaseModel):
    """Тело HTTP-уведомления YooKassa (фрагмент)."""

    model_config = ConfigDict(extra="ignore")

    type: str = ""
    event: str
    object: dict

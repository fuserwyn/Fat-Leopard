"""DTO входящего уведомления ЮKassa."""

from pydantic import BaseModel, ConfigDict


class PaymentNotification(BaseModel):
    model_config = ConfigDict(extra="ignore")

    type: str = ""
    event: str
    object: dict

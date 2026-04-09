"""DTO входящего уведомления ЮKassa."""

from pydantic import BaseModel, ConfigDict

# В уведомлении поле object — полный объект платежа ЮKassa (десятки полей, разные версии API).
# Тип dict + extra=ignore на корне: парсится любой реальный JSON вебхука; в Swagger это выглядит
# как «произвольный объект» — см. пример POST /api/v1/webhook/payment в Swagger.


class PaymentNotification(BaseModel):
    model_config = ConfigDict(extra="ignore")

    type: str = ""
    event: str
    object: dict

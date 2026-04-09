# Контекст сборки — эта папка (Railway Root Directory = ms_payments, или docker compose context: ./ms_payments).
FROM python:3.12-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

EXPOSE 8000

# access-log: каждая строка HTTP в логах Railway (включая POST от ЮKassa)
CMD ["sh", "-c", "uvicorn app.main:app --host 0.0.0.0 --port ${PORT:-8000} --access-log --log-level info"]

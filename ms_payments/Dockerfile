# Сборка из корня монорепозитория:
#   docker build -f ms_payments/Dockerfile .
FROM python:3.12-slim

WORKDIR /app

COPY ms_payments/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY ms_payments/ .

EXPOSE 8000

CMD ["sh", "-c", "uvicorn app.main:app --host 0.0.0.0 --port ${PORT:-8000}"]

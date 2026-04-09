# По умолчанию — только бот (Go). HTTP-вебхук ЮKassa в этом образе НЕ обслуживается.
# Нужен отдельный сервис Railway: RAILWAY_DOCKERFILE_PATH=Dockerfile.ms_payments (корень репо).
# В личном кабинете ЮKassa URL должен совпадать с публичным URL именно сервиса ms_payments.
#   docker build .
#   docker build -f Dockerfile.ms_payments .
FROM golang:1.21

WORKDIR /app

COPY ms_leo/go.mod ./
COPY ms_leo/go.sum* ./

RUN go mod download

COPY ms_leo/ .

RUN go mod tidy

RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/bot

EXPOSE 8080

CMD ["./main"]

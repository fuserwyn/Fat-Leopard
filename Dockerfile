# Сборка из КОРНЯ монorepo. Railway **автоматически** использует только файл с именем `Dockerfile` —
# `Dockerfile.bot` не ищет, иначе включится Nixpacks+Node (miniapp/package.json).
# Логика копия Dockerfile.bot; менять оба вместе или вынеси в общий образ.
# Рекомендация: для бота в Railway задать Source Root = `ms_leo` и `ms_leo/Dockerfile` (без префикса ms_leo/ в COPY).
#
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

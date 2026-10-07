# Контекст сборки — эта папка (Railway Root Directory = ms_leo, или docker compose context: ./ms_leo).
FROM golang:1.25

WORKDIR /app

COPY go.mod ./
COPY go.sum* ./

RUN go mod download

COPY . .

RUN go mod tidy

# Тесты в момент выкатки, с временным Postgres для SQL-тестов. Упали — образ
# не собирается и на прод ничего не уезжает.
RUN apt-get update \
    && apt-get install -y --no-install-recommends postgresql \
    && rm -rf /var/lib/apt/lists/*
RUN sh ./scripts/test-in-build.sh

RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/bot
RUN CGO_ENABLED=0 GOOS=linux go build -o migrate ./cmd/migrate

EXPOSE 8080

CMD ["./main"]

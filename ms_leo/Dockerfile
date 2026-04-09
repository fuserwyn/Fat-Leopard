# Контекст сборки — эта папка (Railway Root Directory = ms_leo, или docker compose context: ./ms_leo).
FROM golang:1.21

WORKDIR /app

COPY go.mod ./
COPY go.sum* ./

RUN go mod download

COPY . .

RUN go mod tidy

RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/bot

EXPOSE 8080

CMD ["./main"]

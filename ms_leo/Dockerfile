# Сборка из корня монорепозитория (контекст — родительская папка):
#   docker build -f ms_leo/Dockerfile .
# docker compose использует context: .. в корне репо.
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

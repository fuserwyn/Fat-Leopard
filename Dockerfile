# Dockerfile для Railway
FROM golang:1.21

# Устанавливаем рабочую директорию
WORKDIR /app

# Копируем go.mod и go.sum (если есть) для кеширования зависимостей
COPY go.mod go.sum* ./

# Скачиваем зависимости (это создаст go.sum, если его нет)
RUN go mod download

# Проверяем зависимости
RUN go mod verify

# Копируем остальные файлы
COPY . .

# Собираем приложение
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/bot

# Открываем порт
EXPOSE 8080

# Запускаем приложение
CMD ["./main"]

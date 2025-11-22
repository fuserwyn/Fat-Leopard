# Dockerfile для Railway
FROM golang:1.21

# Устанавливаем рабочую директорию
WORKDIR /app

# Копируем go.mod и go.sum для кеширования зависимостей
COPY go.mod ./
COPY go.sum* ./

# Скачиваем зависимости
RUN go mod download

# Копируем остальные файлы
COPY . .

# Синхронизируем зависимости после копирования всех файлов
RUN go mod tidy

# Собираем приложение
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/bot

# Открываем порт
EXPOSE 8080

# Запускаем приложение
CMD ["./main"]

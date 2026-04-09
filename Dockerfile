# Корень монорепозитория — Railway подхватывает именно `Dockerfile`, иначе идёт Nixpacks.
#   docker build .
# Для ms_payments из корня репо: docker build -f Dockerfile.ms_payments .
# Или в Railway для сервиса вебхука: RAILWAY_DOCKERFILE_PATH=Dockerfile.ms_payments
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

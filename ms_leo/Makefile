.PHONY: build run test clean docker-build docker-run docker-build-run railway-deploy docker-stop docker-run-detached docker-logs db-connect db-start db-stop db-reset migrate build-migrate

# Корень монорепозитория (docker-compose.yml в родительской папке)
REPO_ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/..)
COMPOSE := docker compose -f $(REPO_ROOT)/docker-compose.yml

# Сборка приложения
build:
	go build -o bin/leo-bot ./cmd/bot

# Запуск приложения
run: build
	./bin/leo-bot

# Тесты
test:
	go test ./...

# Очистка
clean:
	rm -rf bin/
	go clean

# Сборка Docker образа бота (запускай из каталога ms_leo; контекст — .)
docker-build:
	docker build -t leo-bot .

# Запуск всего стека из корня репозитория
docker-run:
	$(COMPOSE) up --build

# Сборка и запуск Docker (все в одном)
docker-build-run:
	docker build -t leo-bot .
	$(COMPOSE) up

# Railway deployment (использует Nixpacks)
railway-deploy:
	git add .
	git commit -m "Update for Railway deployment"
	git push

# Остановка Docker Compose
docker-stop:
	$(COMPOSE) down

# Запуск в фоновом режиме
docker-run-detached:
	$(COMPOSE) up -d --build

# Просмотр логов
docker-logs:
	$(COMPOSE) logs -f

# Подключение к базе данных
db-connect:
	$(COMPOSE) exec postgres psql -U postgres -d leo_bot_db

# Запуск только базы данных
db-start:
	$(COMPOSE) up -d postgres

# Остановка базы данных
db-stop:
	$(COMPOSE) stop postgres

# Сброс базы данных
db-reset:
	$(COMPOSE) down -v
	$(COMPOSE) up -d postgres

# Запуск миграций
migrate:
	go run ./cmd/migrate

# Сборка команды миграций
build-migrate:
	go build -o bin/migrate ./cmd/migrate

# LeoPoacherBot — монорепозиторий

| Сервис | Папка | Описание |
|--------|--------|----------|
| **ms_leo** | `ms_leo/` | Telegram-бот на Go (основной «леопард») |
| **ms_payments** | `ms_payments/` | HTTP-вебхук ЮKassa на FastAPI |

Общие для локального запуска: в корне **`docker-compose.yml`**, **`docker/postgres/init/`**, **`env_template.txt`**, **`.env`**.

- Сборка и тесты бота: `cd ms_leo && make build` / `make test`
- Полный стек: `docker compose up --build` из **корня** репозитория
- Railway: задай **Root Directory** — `ms_leo` для бота, `ms_payments` для платёжного сервиса

Подробности по боту: [ms_leo/README.md](ms_leo/README.md).

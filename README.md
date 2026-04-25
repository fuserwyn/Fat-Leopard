# LeoPoacherBot — монорепозиторий

| Сервис | Папка | Описание |
|--------|--------|----------|
| **ms_leo** | `ms_leo/` | Telegram-бот на Go (основной «леопард») |
| **ms_payments** | `ms_payments/` | HTTP-вебхук ЮKassa на FastAPI |

Общие для локального запуска: в корне **`docker-compose.yml`**, **`docker/postgres/init/`**, **`env_template.txt`**, **`.env`**.

- Сборка и тесты бота: `cd ms_leo && make build` / `make test`
- Полный стек: `docker compose up --build` из **корня** репозитория
- Railway: **Root Directory** = `ms_leo` (бот) или `ms_payments` / `miniapp`. Для **бота** при root = `ms_leo` не указывай путь к `Dockerfile.bot` в корне (будет `COPY ms_leo: not found`). **Мини-апп** — Root = `miniapp`, Dockerfile `Dockerfile.miniapp` (см. `miniapp/railway.toml`); в логах Golang+`ms_leo` = выбран не тот сервис/корень. Для ручного билда бота с корня репо: `docker build -f Dockerfile.bot -t leo .`

Подробности по боту: [ms_leo/README.md](ms_leo/README.md).

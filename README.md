# LeoPoacherBot — монорепозиторий

| Сервис | Папка | Описание |
|--------|--------|----------|
| **ms_leo** | `ms_leo/` | Telegram-бот на Go (основной «леопард») |
| **ms_payments** | `ms_payments/` | HTTP-вебхук ЮKassa на FastAPI |

Общие для локального запуска: в корне **`docker-compose.yml`**, **`docker/postgres/init/`**, **`env_template.txt`**, **`.env`**.

- Сборка и тесты бота: `cd ms_leo && make build` / `make test`
- Полный стек: `docker compose up --build` из **корня** репозитория
- Railway: **Root Directory** = `ms_leo` (лучше для бота) или `ms_payments` / `miniapp`. Мини-апп: только Root = `miniapp` (см. `miniapp/railway.toml`). Бот, если **оставлен Root = весь репо**, подхватит [railway.toml](railway.toml) в корне → `Dockerfile.bot`, иначе Nixpacks. При Root = `ms_leo` путь к `Dockerfile.bot` из UI не вешаем. Ручной билд бота с корня: `docker build -f Dockerfile.bot -t leo .`

Подробности по боту: [ms_leo/README.md](ms_leo/README.md).

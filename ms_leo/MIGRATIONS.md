# Миграции базы данных

## Обзор

Система миграций автоматически обновляет схему базы данных при запуске приложения. Миграции выполняются только один раз и отслеживаются в таблице `migrations`.

## Как это работает

1. **При первом запуске**: Создаются базовые таблицы с простыми типами `TIMESTAMP`
2. **Автоматически**: Запускаются миграции, которые обновляют схему до нужного состояния
3. **Безопасно**: Каждая миграция выполняется в транзакции и может быть откачена

## Существующие миграции

### Миграция 1: Обновление временных полей на московский часовой пояс

**Описание**: Обновляет поля `created_at` и `updated_at` для использования московского времени

**Изменения**:
- `training_state` (легаси: `message_log`) — `created_at` / `updated_at` → `TIMESTAMP WITH TIME ZONE` с московским временем
- `training_log.*` — те же приведения к МСК

### Миграция 14: `training_sessions.session_date`

Колонка приводится к типу **`DATE`** (раньше `TEXT`). Невалидные значения заменяются на `2000-01-01`.

### Миграция 15: `training_log` и несколько чатов

Добавляется **`chat_id`**, первичный ключ **`(user_id, chat_id)`**. Уже существующие строки получают `chat_id = 0` (наследие «один глобальный отчёт на пользователя»).

### Миграция 31: история личного чата с Лео (кросс-девайс)

Создаётся таблица `miniapp_personal_chat` для серверного хранения переписки юзера с Лео. Раньше история жила в `localStorage` браузера/устройства — между Telegram Desktop и iPhone синхронизация ломалась. Теперь источник правды — БД.

Поля:
- `id BIGSERIAL PRIMARY KEY` — серверный id, используется как `since_id` курсор в API.
- `user_id BIGINT NOT NULL`, `pack_chat_id BIGINT NOT NULL` — изоляция между паками.
- `role TEXT NOT NULL CHECK (role IN ('user','leo'))` — кто пишет.
- `message_text TEXT NOT NULL`, `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`.
- Индексы по `(user_id, pack_chat_id, id DESC)` и `(user_id, pack_chat_id, created_at DESC)` — для быстрого фида.

API: `POST /api/miniapp/personal-chat/feed` с `{ init_data, since_id }` возвращает упорядоченный список сообщений в хронологическом порядке. Запись юзер-сообщений идёт в `ProcessMiniAppPrivateText`, ответов Лео — в `miniappPersonalPush`.

### Миграция 87: контакты пользователя и уведомление «контакт вступил в стаю»

Bot API не отдаёт адресную книгу, поэтому контакты — это карточки, которые пользователь сам присылает боту в личку.

- `user_contacts (owner_user_id, contact_user_id, contact_name)` — кому из Telegram-пользователей сообщать о вступлении.
- `miniapp_contact_join_notifications (user_id, pack_chat_id, enabled)` — переключатель в профиле мини-аппа; строки нет — включено.
- `contact_join_notify_log (owner_user_id, contact_user_id, pack_chat_id)` — не больше одного уведомления на пару.

### Миграция 88: челленджи

N дней подряд с тренировкой (логика — `internal/bot/challenges.go`).

- `challenges (id, code, title, length_days, author_user_id, created_at)` — `code` идёт в ссылку `t.me/<бот>?start=ch-<code>`. Шесть стандартных (7, 14, 30, 60, 90, 100 дней, коды `days7`…`days100`, `author_user_id IS NULL`) заводит миграция; свои (3–365 дней, название до 40 символов) создают те, кто прошёл 100-дневный.
- `challenge_participants (challenge_id, user_id, pack_chat_id, start_date, status, days_done, last_counted_date, finished_at)` — статус `active` / `completed` / `failed`; частичный уникальный индекс держит один активный челлендж на участника. Даты — локальные даты участника.
- `challenge_invites (user_id, challenge_id)` — челлендж из ссылки, который мини-апп предложит принять.

API: `POST /api/miniapp/challenges/state`, `/challenges/accept {code}`, `/challenges/create {title, length_days}`, `/challenges/leave`, `/challenges/invite/dismiss`.

## Ручной запуск миграций

Если нужно запустить миграции вручную:

```bash
# Запуск миграций
make migrate

# Или напрямую
go run ./cmd/migrate
```

## Добавление новых миграций

1. Создайте новую миграцию в `internal/database/migrations.go`:

```go
{
    Version:     2, // Следующий номер версии
    Description: "Описание изменений",
    UpSQL:       `SQL для применения изменений`,
    DownSQL:     `SQL для отката изменений`,
}
```

2. Добавьте миграцию в массив `Migrations`

3. Перезапустите приложение - миграция выполнится автоматически

## Откат миграций

В текущей версии откат миграций не реализован автоматически. Для отката нужно:

1. Выполнить SQL из поля `DownSQL` вручную
2. Удалить запись из таблицы `migrations`

## Мониторинг

Проверить статус миграций можно через SQL:

```sql
SELECT * FROM migrations ORDER BY version;
```

## Безопасность

- Все миграции выполняются в транзакциях
- При ошибке миграция автоматически откатывается
- Каждая миграция выполняется только один раз
- Миграции выполняются в порядке версий

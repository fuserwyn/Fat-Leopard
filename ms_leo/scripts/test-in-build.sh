#!/bin/sh
# Тесты в момент выкатки: запускается внутри сборки образа (Dockerfile).
# Поднимает временный Postgres, чтобы SQL-тесты шли на настоящей схеме со
# всеми миграциями, и гоняет `go test ./...`. Упали тесты — образ не
# собирается, Railway оставляет работать прошлую версию.
set -e
PGBIN=$(ls -d /usr/lib/postgresql/*/bin 2>/dev/null | tail -1)
if [ -n "$PGBIN" ]; then
	DIR=/tmp/leo-build-pg
	rm -rf "$DIR"
	mkdir -p "$DIR"
	chown postgres "$DIR"
	su postgres -c "$PGBIN/initdb -D $DIR -A trust -U postgres" >/dev/null
	su postgres -c "$PGBIN/pg_ctl -D $DIR -o '-p 54330 -c listen_addresses=127.0.0.1' -l $DIR/server.log -w start" >/dev/null
	export LEO_TEST_PG_DSN="postgres://postgres@127.0.0.1:54330/postgres?sslmode=disable&timezone=Europe/Moscow"
	echo "тесты: временный Postgres поднят"
else
	echo "тесты: Postgres не найден, SQL-тесты будут пропущены"
fi
status=0
# -coverpkg: покрытие считается по всему коду, а не только по пакету теста —
# сквозные тесты API проходят через bot и database.
go test ./... -coverpkg=./internal/... -coverprofile=/tmp/leo-cover.out || status=$?
if [ "$status" -eq 0 ] && [ -n "$PGBIN" ]; then
	total=$(go tool cover -func=/tmp/leo-cover.out | awk '/^total:/ {sub(/%/, "", $3); print $3}')
	floor=$(cat coverage-floor.txt 2>/dev/null || echo 0)
	echo "тесты: покрытие ${total}% (не ниже ${floor}%)"
	# Порог только растёт: новый код без тестов опускает покрытие и не выкатывается.
	if awk -v t="$total" -v f="$floor" 'BEGIN { exit !(t + 0 < f + 0) }'; then
		echo "тесты: покрытие ${total}% ниже порога ${floor}% — добавьте тесты на новый код"
		status=1
	fi
fi
if [ -n "$PGBIN" ]; then
	su postgres -c "$PGBIN/pg_ctl -D /tmp/leo-build-pg -m fast stop" >/dev/null 2>&1 || true
fi
exit $status

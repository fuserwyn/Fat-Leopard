FROM golang:1.25 AS build

WORKDIR /src
COPY go.mod ./
COPY go.sum* ./
RUN go mod download
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/main ./cmd/tracker

FROM python:3.12-slim

# Go, Node и Postgres нужны не трекеру, а агенту: перед сдачей задачи он сам
# собирает сервисы, гоняет тесты и проверяет SQL на временной базе
# (leo-test-pg). Без них агент сдавал код «на глаз».
RUN apt-get update \
    && apt-get install -y --no-install-recommends git ca-certificates tini curl postgresql \
    && curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && pip install --no-cache-dir 'cursor-sdk>=0.1.0' 'claude-agent-sdk>=0.2.0' \
    && apt-get purge -y curl \
    && apt-get autoremove -y \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH
ENV GOFLAGS=-buildvcs=false

WORKDIR /app
COPY --from=build /out/main /app/main
COPY cursor_run.py /app/cursor_run.py
COPY claude_run.py /app/claude_run.py
COPY leo-test-pg /usr/local/bin/leo-test-pg
ENV CURSOR_RUN=/app/cursor_run.py
ENV CLAUDE_RUN=/app/claude_run.py

EXPOSE 8080
# tini — PID 1, собирает осиротевших внуков (node-bridge Cursor SDK, CLI Claude, git).
# Без него ./main сам был PID 1, зомби копились до лимита pids и
# clone падал «cannot fork(): Resource temporarily unavailable».
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["./main"]

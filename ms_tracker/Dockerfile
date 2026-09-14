FROM golang:1.25 AS build

WORKDIR /src
COPY go.mod ./
COPY go.sum* ./
RUN go mod download
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/main ./cmd/tracker

FROM python:3.12-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends git ca-certificates tini \
    && pip install --no-cache-dir 'cursor-sdk>=0.1.0' \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=build /out/main /app/main
COPY cursor_run.py /app/cursor_run.py
ENV CURSOR_RUN=/app/cursor_run.py

EXPOSE 8080
# tini — PID 1, собирает осиротевших внуков (node-bridge Cursor SDK, git).
# Без него ./main сам был PID 1, зомби копились до лимита pids и
# clone падал «cannot fork(): Resource temporarily unavailable».
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["./main"]

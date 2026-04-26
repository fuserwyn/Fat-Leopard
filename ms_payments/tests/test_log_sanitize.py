from app.core.log_sanitize import redact_telegram_bot_urls


def test_redact_telegram_bot_urls():
    raw = (
        'HTTP Request: POST https://api.telegram.org/bot123456:ABC-DEF_x/sendMessage '
        '"HTTP/1.1 200 OK"'
    )
    out = redact_telegram_bot_urls(raw)
    assert "ABC-DEF" not in out
    assert "bot<redacted>/sendMessage" in out
    assert "https://api.telegram.org/" in out

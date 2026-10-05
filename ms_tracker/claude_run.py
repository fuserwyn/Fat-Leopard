#!/usr/bin/env python3
"""Один прогон Claude Agent SDK локально в клоне репозитория.

Протокол тот же, что у cursor_run.py: на stdin JSON {cwd, prompt, model},
на stdout одна строка JSON {ok, status, result | error}.
"""

from __future__ import annotations

import asyncio
import json
import os
import sys

from claude_agent_sdk import (
    AssistantMessage,
    ClaudeAgentOptions,
    ClaudeSDKError,
    ResultMessage,
    TextBlock,
    query,
)

DEFAULT_MODEL = "claude-opus-5-5"
MAX_TURNS = int(os.environ.get("CLAUDE_MAX_TURNS") or "150")

# Инструменты, которые агент может звать без подтверждения: правка файлов
# и шелл (тесты, сборка, локальный git commit). Пуш делает сам трекер.
ALLOWED_TOOLS = ["Read", "Write", "Edit", "Glob", "Grep", "Bash", "TodoWrite"]


def _emit(obj: dict) -> None:
    print(json.dumps(obj, ensure_ascii=False), flush=True)


async def _run(model: str, cwd: str, prompt: str) -> tuple[ResultMessage | None, str]:
    options = ClaudeAgentOptions(
        cwd=cwd,
        model=model,
        allowed_tools=ALLOWED_TOOLS,
        permission_mode="acceptEdits",
        max_turns=MAX_TURNS,
        # Без пользовательских/проектных настроек — только то, что задали тут.
        setting_sources=[],
    )
    result: ResultMessage | None = None
    last_text = ""
    async for msg in query(prompt=prompt, options=options):
        if isinstance(msg, AssistantMessage):
            parts = [b.text for b in msg.content if isinstance(b, TextBlock) and b.text.strip()]
            if parts:
                last_text = "\n".join(parts)
        elif isinstance(msg, ResultMessage):
            result = msg
    return result, last_text


def main() -> int:
    try:
        payload = json.load(sys.stdin)
    except json.JSONDecodeError as exc:
        _emit({"ok": False, "error": f"stdin json: {exc}"})
        return 1

    cwd = str(payload.get("cwd") or "").strip()
    prompt = str(payload.get("prompt") or "").strip()
    model = str(payload.get("model") or DEFAULT_MODEL).strip() or DEFAULT_MODEL
    # Claude Agent SDK берёт доступ из окружения: токен подписки Claude Code
    # (CLAUDE_CODE_OAUTH_TOKEN) или, если его нет, ANTHROPIC_API_KEY.
    if not any((os.environ.get(k) or "").strip() for k in ("CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY")):
        _emit({"ok": False, "error": "нет CLAUDE_CODE_OAUTH_TOKEN"})
        return 1
    if not cwd or not os.path.isdir(cwd):
        _emit({"ok": False, "error": "нет каталога репозитория"})
        return 1
    if not prompt:
        _emit({"ok": False, "error": "пустой промпт"})
        return 1

    try:
        result, last_text = asyncio.run(_run(model, cwd, prompt))
    except ClaudeSDKError as err:
        _emit({"ok": False, "error": str(err) or type(err).__name__})
        return 1
    except Exception as exc:  # noqa: BLE001
        _emit({"ok": False, "error": str(exc) or type(exc).__name__})
        return 1

    if result is None:
        _emit({"ok": False, "error": f"claude не вернул результат (model {model})"})
        return 2
    text = str(result.result or last_text or "").strip()
    # Упёрся в лимит ходов — правки могли успеть лечь в репо; трекер сам
    # проверит git status и не примет задачу без правок приложения.
    if result.is_error and result.subtype == "error_max_turns":
        _emit({"ok": True, "status": result.subtype, "result": text or f"Claude упёрся в лимит {MAX_TURNS} ходов."})
        return 0
    if result.is_error:
        reason = text or "; ".join(str(e) for e in (result.errors or [])) or result.subtype
        _emit({"ok": False, "status": result.subtype, "error": f"claude: {reason} (model {model})"})
        return 2
    _emit(
        {
            "ok": True,
            "status": result.subtype or "success",
            "result": text,
            "cost_usd": result.total_cost_usd,
            "turns": result.num_turns,
        }
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())

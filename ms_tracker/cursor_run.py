#!/usr/bin/env python3
"""Один прогон Cursor SDK локально в клоне репозитория (как myvibelab)."""

from __future__ import annotations

import json
import os
import sys

from cursor_sdk import Agent, AgentOptions, CursorAgentError, LocalAgentOptions

try:
    from cursor_sdk import SendOptions
except ImportError:  # старые версии SDK
    SendOptions = None  # type: ignore


def _text(result: object) -> str:
    raw = getattr(result, "result", None)
    if raw is None:
        raw = getattr(result, "text", None)
    if callable(raw):
        try:
            raw = raw()
        except TypeError:
            raw = None
    return str(raw or "").strip()


def _error_text(result: object) -> str:
    err = getattr(result, "error", None)
    if err is None:
        return ""
    if isinstance(err, dict):
        return str(err.get("message") or err.get("code") or "").strip()
    msg = getattr(err, "message", None)
    if msg:
        return str(msg).strip()
    code = getattr(err, "code", None)
    if code:
        return str(code).strip()
    return str(err).strip()


FALLBACK_MODEL = "default"


def _run(api_key: str, model: str, cwd: str, prompt: str) -> object:
    if SendOptions is None:
        opts = AgentOptions(
            api_key=api_key,
            model=model,
            local=LocalAgentOptions(cwd=cwd, setting_sources=[]),
        )
        return Agent.prompt(prompt, opts)
    with Agent.create(
        model=model,
        api_key=api_key,
        local=LocalAgentOptions(cwd=cwd, setting_sources=[]),
    ) as agent:
        run = agent.send(prompt, SendOptions(mode="agent"))
        result = run.wait()
        if not _text(result) and hasattr(run, "text"):
            try:
                result = type("R", (), {"status": getattr(result, "status", ""), "result": run.text()})()
            except Exception:  # noqa: BLE001
                pass
        return result


def _failed_silently(result: object) -> bool:
    status = str(getattr(result, "status", "") or "").lower()
    return status == "error" and not _text(result) and not _error_text(result)


def _emit(payload: dict) -> None:
    print(json.dumps(payload, ensure_ascii=False), flush=True)


def main() -> int:
    try:
        payload = json.load(sys.stdin)
    except json.JSONDecodeError as exc:
        _emit({"ok": False, "error": f"stdin json: {exc}"})
        return 1

    cwd = str(payload.get("cwd") or "").strip()
    prompt = str(payload.get("prompt") or "").strip()
    model = str(payload.get("model") or "composer-2.5").strip() or "composer-2.5"
    api_key = (os.environ.get("CURSOR_API_KEY") or "").strip()
    if not api_key:
        _emit({"ok": False, "error": "нет CURSOR_API_KEY"})
        return 1
    if not cwd or not os.path.isdir(cwd):
        _emit({"ok": False, "error": "нет каталога репозитория"})
        return 1
    if not prompt:
        _emit({"ok": False, "error": "пустой промпт"})
        return 1

    used_model = model
    result: object | None = None
    try:
        result = _run(api_key, model, cwd, prompt)
        # Именованная модель вне лимита тарифа Cursor сдаётся молча: status=error
        # без текста (resource_exhausted / 429). Auto («default») при этом часто
        # ещё работает — пробуем её, а не валим задачу на первой же попытке.
        if _failed_silently(result) and model != FALLBACK_MODEL:
            used_model = FALLBACK_MODEL
            result = _run(api_key, FALLBACK_MODEL, cwd, prompt)
    except CursorAgentError as err:
        if model != FALLBACK_MODEL:
            try:
                used_model = FALLBACK_MODEL
                result = _run(api_key, FALLBACK_MODEL, cwd, prompt)
            except Exception:  # noqa: BLE001
                _emit(
                    {
                        "ok": False,
                        "error": err.message,
                        "retryable": bool(getattr(err, "is_retryable", False)),
                    }
                )
                return 1
        else:
            _emit(
                {
                    "ok": False,
                    "error": err.message,
                    "retryable": bool(getattr(err, "is_retryable", False)),
                }
            )
            return 1
    except Exception as exc:  # noqa: BLE001
        if model != FALLBACK_MODEL:
            try:
                used_model = FALLBACK_MODEL
                result = _run(api_key, FALLBACK_MODEL, cwd, prompt)
            except Exception as fallback_exc:  # noqa: BLE001
                _emit({"ok": False, "error": str(fallback_exc)})
                return 1
        else:
            _emit({"ok": False, "error": str(exc)})
            return 1

    status = str(getattr(result, "status", "") or "").lower()
    text = _text(result)
    detail = _error_text(result)
    if status == "error":
        if not text and not detail:
            detail = (
                f"cursor run error (model {used_model}): "
                "likely Cursor usage limit — named models blocked; Auto also failed"
            )
        elif not text:
            detail = f"{detail} (model {used_model})"
        _emit({"ok": False, "error": text or detail, "status": status})
        return 2
    _emit({"ok": True, "status": status or "finished", "result": text})
    return 0


if __name__ == "__main__":
    sys.exit(main())

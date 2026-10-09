/**
 * Гостевой просмотр: человек открыл мини-апп, ещё не вступив в стаю (пришёл по
 * чужой ссылке). Сервер отдаёт несколько свежих тренировок и ссылку «Вступить»
 * (POST /api/miniapp/feed/guest, ms_leo/internal/bot/miniapp_guest_feed.go).
 */
import type { PackFeedItemDTO } from "./packFeed";

const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

export type GuestFeed = {
  /** Смотрящий уже в стае — показывать обычное приложение. */
  inPack: boolean;
  items: PackFeedItemDTO[];
  /** Ссылка на бота для «Вступить»; пусто — просто закрыть мини-апп (бот и так рядом). */
  joinUrl: string;
};

const str = (v: unknown): string => (typeof v === "string" ? v : "");
const num = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) ? v : 0);

/** Разбор ответа сервера; битые записи пропускаются. */
export function parseGuestFeed(raw: unknown): GuestFeed | null {
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Record<string, unknown>;
  const list = Array.isArray(r.items) ? r.items : [];
  const items: PackFeedItemDTO[] = [];
  for (const x of list) {
    if (!x || typeof x !== "object") continue;
    const it = x as Record<string, unknown>;
    const text = str(it.text);
    if (!text.trim()) continue;
    items.push({
      id: num(it.id),
      user_id: 0,
      username: str(it.username).trim() || "Участник стаи",
      type: str(it.type) || "training_done",
      source: "feed",
      text,
      created_at: str(it.created_at),
      streak_days: num(it.streak_days),
      is_you: false,
      training_photo_url: str(it.training_photo_url) || undefined,
    });
  }
  const joinUrl = str(r.join_url).trim();
  return {
    inPack: r.in_pack === true,
    items,
    joinUrl: /^https:\/\/t\.me\//.test(joinUrl) ? joinUrl : "",
  };
}

/** Гостевая лента; null — сервер недоступен или отказал. */
export async function fetchGuestFeed(initData: string): Promise<GuestFeed | null> {
  if (!apiBase || !initData.trim()) return null;
  try {
    const res = await fetch(`${apiBase}/api/miniapp/feed/guest`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ init_data: initData }),
    });
    const j = (await res.json().catch(() => ({}))) as { ok?: boolean };
    if (!res.ok || !j.ok) return null;
    return parseGuestFeed(j);
  } catch {
    return null;
  }
}

/**
 * «Вступить»: открыть чат с ботом (там оплата или бесплатный вход). Без ссылки
 * просто закрываем мини-апп — гость пришёл из чата с ботом и вернётся туда же.
 */
export function openGuestJoin(joinUrl: string): void {
  const wa = window.Telegram?.WebApp;
  if (!joinUrl) {
    wa?.close();
    return;
  }
  if (wa?.openTelegramLink) wa.openTelegramLink(joinUrl);
  else if (wa?.openLink) wa.openLink(joinUrl);
  else window.open(joinUrl, "_blank");
}

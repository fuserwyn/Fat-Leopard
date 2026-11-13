const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

const cache = new Map<string, string>();

/** Текст Лео с кириллицей — имеет смысл переводить на EN. */
export function leoTextNeedsEnglishTranslate(text: string): boolean {
  return /[А-Яа-яЁё]/.test(text);
}

export async function translateLeoTextToEnglish(initData: string, text: string): Promise<string> {
  const trimmed = text.trim();
  if (!trimmed) return "";
  if (!leoTextNeedsEnglishTranslate(trimmed)) return trimmed;
  const hit = cache.get(trimmed);
  if (hit) return hit;
  if (!apiBase || !initData.trim()) {
    throw new Error("unavailable");
  }
  const res = await fetch(`${apiBase}/api/miniapp/leo/translate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ init_data: initData, text: trimmed }),
  });
  const j = (await res.json().catch(() => ({}))) as { ok?: boolean; text_en?: string; error?: string };
  if (!res.ok || !j.ok || typeof j.text_en !== "string" || !j.text_en.trim()) {
    throw new Error(j.error ?? `translate ${res.status}`);
  }
  const out = j.text_en.trim();
  cache.set(trimmed, out);
  return out;
}

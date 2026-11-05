// Подсказка в описании тренировки про совместную тренировку: @-отметка уведомляет
// участника стаи, а тому, кого в стае нет, бот шлёт инвайт от отметившего.
export const WORKOUT_NOTE_PLACEHOLDER =
  "Как прошла тренировка? Тренировались вместе — отметь напарника: @ник";

const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

/** Участник стаи в подсказке «@»: mention вставляется после «@» (ник или имя без пробелов). */
export type MentionCandidate = { userId: number; name: string; mention: string };

/** Символы, которые обрывают @-отметку — как rePackMemberMentions в ms_leo. */
const MENTION_STOP = /[\s@,.!?;:()[\]{}«»"']/;

/**
 * Набираемая у курсора отметка: «@» в начале текста или после пробела/знака и
 * дальше до курсора — без пробелов. null — подсказку не показываем.
 */
export function activeMentionQuery(text: string, caret: number): { start: number; query: string } | null {
  const end = Math.max(0, Math.min(caret, text.length));
  for (let i = end - 1; i >= 0 && end - i <= 41; i--) {
    const ch = text[i];
    if (ch === "@") {
      if (i > 0 && !MENTION_STOP.test(text[i - 1])) return null;
      return { start: i, query: text.slice(i + 1, end) };
    }
    if (MENTION_STOP.test(ch)) return null;
  }
  return null;
}

/** Подставляет выбранного участника вместо набранного «@…»; курсор — после пробела. */
export function applyMention(
  text: string,
  start: number,
  caret: number,
  mention: string,
  max = Infinity,
): { text: string; caret: number } {
  const insert = `@${mention} `;
  const rest = text.slice(caret).replace(/^\S*/, "").replace(/^ /, "");
  const next = (text.slice(0, start) + insert + rest).slice(0, max);
  return { text: next, caret: Math.min(start + insert.length, next.length) };
}

export function parseMentionCandidates(raw: unknown): MentionCandidate[] {
  if (!Array.isArray(raw)) return [];
  const out: MentionCandidate[] = [];
  for (const it of raw) {
    if (!it || typeof it !== "object") continue;
    const r = it as Record<string, unknown>;
    const mention = typeof r.mention === "string" ? r.mention.trim() : "";
    const userId = typeof r.user_id === "number" ? r.user_id : 0;
    if (!mention || !userId) continue;
    const name = typeof r.name === "string" && r.name.trim() ? r.name.trim() : mention;
    out.push({ userId, name, mention });
  }
  return out;
}

/** Участники стаи, чей ник или имя начинается с query. Ошибка сети — пустой список. */
export async function searchMentionCandidates(initData: string, query: string): Promise<MentionCandidate[]> {
  if (!apiBase || !initData.trim()) return [];
  try {
    const res = await fetch(`${apiBase}/api/miniapp/friends/mention-search`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ init_data: initData, query }),
    });
    const j = (await res.json().catch(() => ({}))) as { ok?: boolean; members?: unknown };
    if (!res.ok || !j.ok) return [];
    return parseMentionCandidates(j.members);
  } catch {
    return [];
  }
}

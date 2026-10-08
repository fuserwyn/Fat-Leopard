/**
 * «Позвать в стаю»: личная ссылка t.me/<бот>?start=ref-<id> и счётчики приглашений.
 * Попытка спасти стрик выдаётся за каждых rewardEvery друзей, которые записали
 * первую тренировку, — переход по ссылке сам по себе не считается (ms_leo/internal/bot/referrals.go).
 */

const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

export const REFERRAL_REWARD_EVERY = 10;

export type ReferralState = {
  link: string;
  joined: number;
  qualified: number;
  rewardEvery: number;
  rewards: number;
  nextIn: number;
};

const num = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) && v > 0 ? Math.floor(v) : 0);

/** Разбор ответа сервера; null — показывать нечего (нет ссылки). */
export function parseReferralState(raw: unknown): ReferralState | null {
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Record<string, unknown>;
  const link = typeof r.link === "string" ? r.link.trim() : "";
  if (!/^https:\/\/t\.me\//.test(link)) return null;
  const rewardEvery = num(r.reward_every) || REFERRAL_REWARD_EVERY;
  const qualified = num(r.qualified);
  return {
    link,
    joined: Math.max(num(r.joined), qualified),
    qualified,
    rewardEvery,
    rewards: num(r.rewards),
    nextIn: num(r.next_in) || rewardEvery - (qualified % rewardEvery),
  };
}

export async function fetchReferralState(initData: string): Promise<ReferralState | null> {
  if (!apiBase || !initData.trim()) return null;
  try {
    const res = await fetch(`${apiBase}/api/miniapp/referral/state`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ init_data: initData }),
    });
    const j = (await res.json().catch(() => ({}))) as { ok?: boolean; referral?: unknown };
    if (!res.ok || !j.ok) return null;
    return parseReferralState(j.referral);
  } catch {
    return null;
  }
}

/** Склонение «друг» по числу. */
export function friendsWord(n: number): string {
  const n10 = n % 10;
  const n100 = n % 100;
  if (n10 === 1 && n100 !== 11) return "друг";
  if (n10 >= 2 && n10 <= 4 && (n100 < 12 || n100 > 14)) return "друга";
  return "друзей";
}

/** Строка прогресса к следующей попытке спасти стрик. */
export function referralProgressLine(s: ReferralState): string {
  const done = s.qualified % s.rewardEvery;
  return `До попытки спасти стрик: ${done} из ${s.rewardEvery} — ещё ${s.nextIn} ${friendsWord(s.nextIn)} с первой тренировкой`;
}

/** Ссылка на окно Telegram «Поделиться» с приглашением. */
export function referralShareUrl(link: string): string {
  const text = "Тренируйся со мной в стае Fat Leopard — отмечаем тренировки каждый день 🐆";
  return `https://t.me/share/url?url=${encodeURIComponent(link)}&text=${encodeURIComponent(text)}`;
}

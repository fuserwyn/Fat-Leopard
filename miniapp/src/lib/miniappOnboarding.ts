/**
 * Идемпотентный хендшейк мини-аппа после оплаты:
 * на первом открытии стартует таймер неактивности и пишет pack_join/pack_rejoin в ленту.
 * Следующие вызовы — no-op, можно дёргать без бойлерплейта.
 */

const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

export type OnboardingEnsureResult = {
  ok: boolean;
  inPack: boolean;
  deleted: boolean;
  accessState: "active" | "deleted" | "out" | "unknown";
  justOnboarded: boolean;
  isRejoin: boolean;
};

export async function ensureMiniappOnboarding(initData: string): Promise<OnboardingEnsureResult> {
  const empty: OnboardingEnsureResult = {
    ok: false,
    inPack: false,
    deleted: false,
    accessState: "unknown",
    justOnboarded: false,
    isRejoin: false,
  };
  if (!apiBase || !initData.trim()) return empty;
  try {
    const res = await fetch(`${apiBase}/api/miniapp/onboarding/ensure`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ init_data: initData }),
    });
    const j = (await res.json().catch(() => ({}))) as {
      ok?: boolean;
      in_pack?: boolean;
      deleted?: boolean;
      access_state?: string;
      just_onboarded?: boolean;
      is_rejoin?: boolean;
    };
    if (!res.ok || !j.ok) return empty;
    return {
      ok: true,
      inPack: !!j.in_pack,
      deleted: !!j.deleted,
      accessState: j.access_state === "active" || j.access_state === "deleted" || j.access_state === "out" ? j.access_state : "unknown",
      justOnboarded: !!j.just_onboarded,
      isRejoin: !!j.is_rejoin,
    };
  } catch {
    return empty;
  }
}

/**
 * Что показать после хендшейка: выбывшему — экран блокировки, тому, кого в стае
 * ещё нет (сервер ответил и сказал «out»), — гостевую ленту. Если сервер не
 * ответил, приложение открывается как раньше: отказывать из-за сети нельзя.
 */
export function miniappAccessGate(res: OnboardingEnsureResult): "ok" | "deleted" | "guest" {
  if (res.deleted || res.accessState === "deleted") return "deleted";
  if (res.ok && !res.inPack && res.accessState === "out") return "guest";
  return "ok";
}

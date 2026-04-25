export type Msg = { id: string; role: "user" | "system"; text: string; time: number };

const STORAGE_VERSION = 1;
const defaultWelcome = (): Msg[] => [
  {
    id: "h",
    role: "system",
    time: Date.now(),
    text:
      "Как в личке с ботом: ИИ отвечает на любой текст, есть #training_done и /start. Сообщения из мини-аппа копируются в Telegram (метка «Мини-апп»). «Стая» — чужие отчёты.",
  },
];

function keyForUser(userId: number) {
  return `leo_private_chat_v${STORAGE_VERSION}_${userId > 0 ? String(userId) : "local"}`;
}

function isValidMsg(m: unknown): m is Msg {
  if (!m || typeof m !== "object") return false;
  const o = m as Record<string, unknown>;
  return (
    typeof o.id === "string" &&
    (o.role === "user" || o.role === "system") &&
    typeof o.text === "string" &&
    typeof o.time === "number"
  );
}

export function loadLeoChat(userId: number): Msg[] {
  if (typeof window === "undefined") return defaultWelcome();
  try {
    const raw = localStorage.getItem(keyForUser(userId));
    if (!raw) return defaultWelcome();
    const p = JSON.parse(raw) as unknown;
    if (!Array.isArray(p) || p.length === 0) return defaultWelcome();
    const out = p.filter(isValidMsg);
    return out.length > 0 ? out : defaultWelcome();
  } catch {
    return defaultWelcome();
  }
}

export function saveLeoChat(userId: number, items: Msg[]) {
  if (typeof window === "undefined") return;
  try {
    localStorage.setItem(keyForUser(userId), JSON.stringify(items));
  } catch (e) {
    console.warn("leo chat save", e);
  }
}

/** id пользователя в Telegram; 0 = не из Mini App (браузер) — общий ключ local. */
export function getTelegramUserId(): number {
  if (typeof window === "undefined") return 0;
  return window.Telegram?.WebApp?.initDataUnsafe?.user?.id ?? 0;
}

export { defaultWelcome };

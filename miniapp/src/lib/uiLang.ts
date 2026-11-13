import { isUILang, langFromTelegram, type UILang } from "./i18n";

const STORAGE_KEY = "leo-ui-lang";
const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

let langLockedFromServer = false;

export function resetUILangRuntimeForTests(): void {
  langLockedFromServer = false;
}

export function getStoredUILang(fallback?: UILang): UILang {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (isUILang(raw)) return raw;
  } catch {
    /* private mode */
  }
  // Стая исторически русская: без сохранённого языка и без Telegram — ru.
  return fallback ?? "ru";
}

export function persistUILang(lang: UILang): void {
  try {
    localStorage.setItem(STORAGE_KEY, lang);
  } catch {
    /* private mode */
  }
}

export function applyUILang(lang: UILang): void {
  document.documentElement.lang = lang;
  window.dispatchEvent(new CustomEvent("leo-ui-lang", { detail: lang }));
}

export function setUILang(lang: UILang): void {
  persistUILang(lang);
  applyUILang(lang);
}

export async function persistUILangToServer(initData: string, lang: UILang): Promise<void> {
  if (!apiBase || !initData.trim()) return;
  try {
    await fetch(`${apiBase}/api/miniapp/profile/save`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ init_data: initData, ui_lang: lang }),
    });
  } catch {
    /* keep local */
  }
}

/** Язык из профиля. Пустой ответ не затирает кэш. */
export function hydrateUILangFromServer(raw: unknown, telegramCode?: string | null): UILang {
  if (isUILang(raw)) {
    langLockedFromServer = true;
    setUILang(raw);
    return raw;
  }
  const next = getStoredUILang(langFromTelegram(telegramCode));
  setUILang(next);
  return next;
}

export function bootstrapUILang(telegramCode?: string | null): UILang {
  if (langLockedFromServer) return getStoredUILang();
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (isUILang(raw)) {
      applyUILang(raw);
      return raw;
    }
  } catch {
    /* */
  }
  const next = langFromTelegram(telegramCode);
  setUILang(next);
  return next;
}

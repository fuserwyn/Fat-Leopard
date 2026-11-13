import { isUILang, type UILang } from "./i18n";

/** v2 — сброс кэша `en` после английского шипа (старый ключ leo-ui-lang игнорируем). */
const STORAGE_KEY = "leo-ui-lang-v2";
const LEGACY_STORAGE_KEY = "leo-ui-lang";
const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

let langLockedFromServer = false;

function clearLegacyUILangCache(): void {
  try {
    localStorage.removeItem(LEGACY_STORAGE_KEY);
  } catch {
    /* private mode */
  }
}

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
  // Стая исторически русская: без сохранённого языка — ru (не language_code Telegram).
  return fallback ?? "ru";
}

export function persistUILang(lang: UILang): void {
  try {
    localStorage.setItem(STORAGE_KEY, lang);
    clearLegacyUILangCache();
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

/** Язык из профиля. Пустой ответ → ru (стая), не Telegram и не старый localStorage en. */
export function hydrateUILangFromServer(raw: unknown, _telegramCode?: string | null): UILang {
  clearLegacyUILangCache();
  const next: UILang = isUILang(raw) ? raw : "ru";
  langLockedFromServer = true;
  setUILang(next);
  return next;
}

/** До ответа профиля всегда стартуем с ru; сервер потом подтвердит/переключит. */
export function bootstrapUILang(_telegramCode?: string | null): UILang {
  clearLegacyUILangCache();
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
  setUILang("ru");
  return "ru";
}

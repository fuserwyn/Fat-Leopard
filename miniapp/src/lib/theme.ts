/** Тема оформления. По умолчанию — тёмная (как до редизайна); светлая — опция пользователя.
 *  Источник правды — профиль на сервере. localStorage и Telegram CloudStorage — кэш:
 *  WebView после закрытия Mini App часто их теряет. */
export type ThemeMode = "light" | "dark" | "leopard" | "wild" | "pack";

/** Розовая леопардовая тема — с 5 уровня (Лев). */
export const LEOPARD_THEME_MIN_LEVEL = 5;

/** Дикая тёмная леопардовая тема — стрик 365 дней или 1000 тренировок (админ — всегда). */
export const WILD_THEME_MIN_STREAK = 365;
export const WILD_THEME_MIN_WORKOUTS = 1000;

export type ThemeUnlock = {
  streakDays?: number;
  maxStreakDays?: number;
  workoutsTotal?: number;
  isAdmin?: boolean;
  /** Временная тема «Стая» после достижения недельной цели. */
  packBonusThemeActive?: boolean;
};

const STORAGE_KEY = "leo-theme";
const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

/** Сервер уже отдал тему — CloudStorage не должен затереть её устаревшим значением. */
let themeLockedFromServer = false;

export function resetThemeRuntimeForTests(): void {
  themeLockedFromServer = false;
}
const THEME_COLOR: Record<ThemeMode, string> = {
  light: "#f5f1f5",
  dark: "#0d0d12",
  leopard: "#f6d4de",
  wild: "#0a0a0a",
  pack: "#12180f",
};

export function isThemeMode(raw: unknown): raw is ThemeMode {
  return raw === "light" || raw === "dark" || raw === "leopard" || raw === "wild" || raw === "pack";
}

export function canUseLeopardTheme(level: number): boolean {
  return level >= LEOPARD_THEME_MIN_LEVEL;
}

export function canUseWildTheme(unlock: ThemeUnlock = {}): boolean {
  if (unlock.isAdmin) return true;
  const streak = Math.max(0, unlock.streakDays ?? 0, unlock.maxStreakDays ?? 0);
  const workouts = Math.max(0, unlock.workoutsTotal ?? 0);
  return streak >= WILD_THEME_MIN_STREAK || workouts >= WILD_THEME_MIN_WORKOUTS;
}

export function canUsePackTheme(unlock: ThemeUnlock = {}): boolean {
  return Boolean(unlock.packBonusThemeActive);
}

export function themeAllowedForLevel(mode: ThemeMode, level: number, unlock: ThemeUnlock = {}): ThemeMode {
  if (mode === "leopard" && !canUseLeopardTheme(level)) {
    return "dark";
  }
  if (mode === "wild" && !canUseWildTheme(unlock)) {
    return "dark";
  }
  if (mode === "pack" && !canUsePackTheme(unlock)) {
    return "dark";
  }
  return mode;
}

export function enforceThemeForLevel(level: number, unlock: ThemeUnlock = {}): ThemeMode {
  const stored = getStoredTheme();
  const next = themeAllowedForLevel(stored, level, unlock);
  if (next !== stored) {
    setTheme(next);
  }
  return next;
}

export function parseTheme(raw: string | null | undefined): ThemeMode {
  if (isThemeMode(raw)) {
    return raw;
  }
  return "dark";
}

export function getStoredTheme(): ThemeMode {
  try {
    return parseTheme(localStorage.getItem(STORAGE_KEY));
  } catch {
    return "dark";
  }
}

export function hasStoredTheme(): boolean {
  try {
    return isThemeMode(localStorage.getItem(STORAGE_KEY));
  } catch {
    return false;
  }
}

function cloudStorage() {
  return window.Telegram?.WebApp?.CloudStorage;
}

export function persistTheme(mode: ThemeMode): void {
  try {
    localStorage.setItem(STORAGE_KEY, mode);
  } catch {
    /* приватный режим */
  }
  try {
    cloudStorage()?.setItem(STORAGE_KEY, mode);
  } catch {
    /* старый клиент без CloudStorage */
  }
}

export function applyTheme(mode: ThemeMode): void {
  document.documentElement.setAttribute("data-theme", mode);
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute("content", THEME_COLOR[mode]);
  window.dispatchEvent(new CustomEvent("leo-theme", { detail: mode }));
}

export function setTheme(mode: ThemeMode): void {
  persistTheme(mode);
  applyTheme(mode);
}

export async function persistThemeToServer(initData: string, mode: ThemeMode): Promise<void> {
  if (!apiBase || !initData.trim()) return;
  try {
    await fetch(`${apiBase}/api/miniapp/profile/save`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ init_data: initData, theme: mode }),
    });
  } catch {
    /* тема останется в localStorage / CloudStorage */
  }
}

/** Тема из профиля. Пустой ответ не затирает кэш и не пишет dark на сервер. */
export function hydrateThemeFromServer(raw: unknown, level: number, unlock: ThemeUnlock = {}): ThemeMode {
  if (isThemeMode(raw)) {
    themeLockedFromServer = true;
    const next = themeAllowedForLevel(raw, level, unlock);
    setTheme(next);
    return next;
  }
  return enforceThemeForLevel(level, unlock);
}

/** Достаёт тему из CloudStorage Telegram — она живёт после закрытия Mini App. */
export function hydrateThemeFromCloud(onDone?: (mode: ThemeMode) => void): void {
  const cs = cloudStorage();
  if (!cs?.getItem) {
    onDone?.(getStoredTheme());
    return;
  }
  try {
    cs.getItem(STORAGE_KEY, (err, value) => {
      if (themeLockedFromServer) {
        onDone?.(getStoredTheme());
        return;
      }
      const cloud = parseTheme(typeof value === "string" ? value : null);
      const localRaw = (() => {
        try {
          return localStorage.getItem(STORAGE_KEY);
        } catch {
          return null;
        }
      })();
      if (!err && isThemeMode(value)) {
        persistTheme(cloud);
        applyTheme(cloud);
        onDone?.(cloud);
        return;
      }
      if (localRaw) {
        persistTheme(parseTheme(localRaw));
      }
      onDone?.(getStoredTheme());
    });
  } catch {
    onDone?.(getStoredTheme());
  }
}

/**
 * Часовой пояс пользователя определяется автоматически из устройства (системная зона телефона)
 * и синхронизируется на бэкенд при каждом запуске мини-аппа и при каждом возврате в него
 * (Telegram держит WebView живым, поэтому одного синка на старте мало: человек перелетел
 * Москва → Белград, открыл мини-апп из фона — и пояс должен обновиться). Это единственный
 * источник правды — ручной команды (#timezone) больше нет.
 *
 * Бэкенд хранит смещение в часах относительно МСК (UTC+3); конвертим из браузерного offset.
 * IANA-зону не храним — вся серверная логика (стрик, таймеры, дедлайны, напоминания)
 * работает по int-смещению.
 */

const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

const MOSCOW_UTC_OFFSET_HOURS = 3;

/** Как часто перепроверять пояс, пока мини-апп открыт (переход на зимнее/летнее время, перелёт). */
export const TIMEZONE_RECHECK_INTERVAL_MS = 10 * 60 * 1000;

/**
 * Смещение пользователя относительно Москвы в часах, округлённое к ближайшему целому
 * (бэкенд хранит int -12..+12). На зонах с 30/45-мин offset стрик считается с погрешностью
 * до часа — это приемлемо для текущей логики.
 */
export function detectOffsetFromMoscow(now: Date = new Date()): number {
  // getTimezoneOffset() возвращает разницу UTC - local в минутах, поэтому со знаком минус.
  const browserOffsetMinutes = -now.getTimezoneOffset();
  const browserOffsetHours = browserOffsetMinutes / 60;
  const offset = Math.round(browserOffsetHours - MOSCOW_UTC_OFFSET_HOURS);
  if (offset < -12) return -12;
  if (offset > 12) return 12;
  return offset;
}

export function detectTimezoneInfo(): { offsetFromMoscow: number; ianaName: string } {
  let ianaName = "";
  try {
    ianaName = Intl.DateTimeFormat().resolvedOptions().timeZone || "";
  } catch {
    ianaName = "";
  }
  return { offsetFromMoscow: detectOffsetFromMoscow(), ianaName };
}

/** Последнее смещение, которое точно лежит на бэкенде (из /profile/load или после успешного save). */
let knownServerOffset: number | null = null;
let inFlight: Promise<void> | null = null;

/** Для тестов: сбросить запомненное серверное смещение. */
export function resetTimezoneSyncState(): void {
  knownServerOffset = null;
  inFlight = null;
}

/** Последнее известное смещение на бэкенде (null — ещё не знаем). */
export function getKnownServerOffset(): number | null {
  return knownServerOffset;
}

async function pushOffset(initData: string, offsetFromMoscow: number): Promise<void> {
  try {
    const res = await fetch(`${apiBase}/api/miniapp/profile/save`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ init_data: initData, timezone_offset: offsetFromMoscow }),
    });
    if (res.ok) knownServerOffset = offsetFromMoscow;
  } catch {
    /* TZ-синк необязателен; ошибка не должна ломать мини-апп — повторим при следующей проверке */
  }
}

/**
 * Приводит хранимое на бэке смещение к фактическому часовому поясу устройства.
 * Шлём profile/save только если оно отличается от сохранённого, чтобы не дёргать сеть зря.
 * @param currentOffset — текущее смещение пользователя из /profile/load.
 */
export async function syncDeviceTimezone(initData: string, currentOffset: number): Promise<void> {
  if (!apiBase || !initData.trim()) return;
  knownServerOffset = currentOffset;
  const { offsetFromMoscow } = detectTimezoneInfo();
  if (offsetFromMoscow === currentOffset) return;
  await pushOffset(initData, offsetFromMoscow);
}

/**
 * Перепроверка без запроса /profile/load: сравниваем пояс устройства с последним известным
 * серверным значением. Если серверное ещё неизвестно — ничего не делаем (его даст syncDeviceTimezone).
 */
export async function recheckDeviceTimezone(initData: string): Promise<void> {
  if (!apiBase || !initData.trim() || knownServerOffset === null) return;
  if (inFlight) return inFlight;
  const { offsetFromMoscow } = detectTimezoneInfo();
  if (offsetFromMoscow === knownServerOffset) return;
  inFlight = pushOffset(initData, offsetFromMoscow).finally(() => {
    inFlight = null;
  });
  return inFlight;
}

/**
 * Следит за поясом устройства, пока мини-апп открыт: при возврате в приложение
 * (visibilitychange/focus) и раз в TIMEZONE_RECHECK_INTERVAL_MS. Возвращает функцию отписки.
 */
export function watchDeviceTimezone(initData: string): () => void {
  if (typeof window === "undefined" || !apiBase || !initData.trim()) return () => {};
  const check = () => {
    if (typeof document !== "undefined" && document.visibilityState === "hidden") return;
    void recheckDeviceTimezone(initData);
  };
  document.addEventListener("visibilitychange", check);
  window.addEventListener("focus", check);
  const timer = window.setInterval(check, TIMEZONE_RECHECK_INTERVAL_MS);
  return () => {
    document.removeEventListener("visibilitychange", check);
    window.removeEventListener("focus", check);
    window.clearInterval(timer);
  };
}

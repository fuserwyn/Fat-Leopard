/**
 * Периоды раздела «Оплаты»: день, неделя (пн–вс), месяц или свой диапазон.
 * Даты — строки «ГГГГ-ММ-ДД» по Москве; сервер берёт их включительно.
 * Арифметика идёт в UTC, чтобы часовой пояс телефона не сдвигал дни.
 */

export type PaymentsPeriodMode = "all" | "day" | "week" | "month" | "custom";

export const PAYMENTS_PERIOD_MODES: { mode: PaymentsPeriodMode; label: string }[] = [
  { mode: "all", label: "Всё время" },
  { mode: "day", label: "День" },
  { mode: "week", label: "Неделя" },
  { mode: "month", label: "Месяц" },
  { mode: "custom", label: "Период" },
];

const MONTHS = [
  "Январь",
  "Февраль",
  "Март",
  "Апрель",
  "Май",
  "Июнь",
  "Июль",
  "Август",
  "Сентябрь",
  "Октябрь",
  "Ноябрь",
  "Декабрь",
];

const MSK_OFFSET_MS = 3 * 3600 * 1000;

function parseDay(day: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day);
  if (!m) return null;
  const d = new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])));
  return Number.isNaN(d.getTime()) ? null : d;
}

function formatDay(d: Date): string {
  const y = d.getUTCFullYear();
  const m = String(d.getUTCMonth() + 1).padStart(2, "0");
  const dd = String(d.getUTCDate()).padStart(2, "0");
  return `${y}-${m}-${dd}`;
}

function addDays(d: Date, n: number): Date {
  return new Date(d.getTime() + n * 86400000);
}

function short(d: Date): string {
  return `${String(d.getUTCDate()).padStart(2, "0")}.${String(d.getUTCMonth() + 1).padStart(2, "0")}`;
}

/** Сегодняшняя дата по Москве. */
export function todayMsk(now: Date = new Date()): string {
  return formatDay(new Date(now.getTime() + MSK_OFFSET_MS));
}

/** Границы периода (обе включительно). Для «всё время» и «период» — пустые строки. */
export function paymentsPeriodRange(mode: PaymentsPeriodMode, anchor: string): { from: string; to: string } {
  const d = parseDay(anchor);
  if (!d || mode === "all" || mode === "custom") return { from: "", to: "" };
  if (mode === "day") return { from: anchor, to: anchor };
  if (mode === "week") {
    const shift = (d.getUTCDay() + 6) % 7; // понедельник — 0
    const start = addDays(d, -shift);
    return { from: formatDay(start), to: formatDay(addDays(start, 6)) };
  }
  const start = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), 1));
  const end = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 0));
  return { from: formatDay(start), to: formatDay(end) };
}

/** Соседний день, неделя или месяц: dir = -1 назад, +1 вперёд. */
export function shiftPaymentsAnchor(mode: PaymentsPeriodMode, anchor: string, dir: -1 | 1): string {
  const d = parseDay(anchor);
  if (!d) return anchor;
  if (mode === "day") return formatDay(addDays(d, dir));
  if (mode === "week") return formatDay(addDays(d, 7 * dir));
  if (mode === "month") {
    // Берём 1-е число, чтобы 31 марта не перескочило через апрель.
    return formatDay(new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + dir, 1)));
  }
  return anchor;
}

/** Подпись текущего периода: «05.10.2026», «29.09–05.10.2026», «Октябрь 2026». */
export function paymentsPeriodTitle(mode: PaymentsPeriodMode, anchor: string): string {
  const { from, to } = paymentsPeriodRange(mode, anchor);
  const f = parseDay(from);
  const t = parseDay(to);
  if (!f || !t) return "";
  if (mode === "day") return `${short(f)}.${f.getUTCFullYear()}`;
  if (mode === "week") return `${short(f)}–${short(t)}.${t.getUTCFullYear()}`;
  return `${MONTHS[f.getUTCMonth()]} ${f.getUTCFullYear()}`;
}

/** Можно ли листать вперёд: следующий период не начинается позже сегодняшнего дня. */
export function canShiftPaymentsForward(mode: PaymentsPeriodMode, anchor: string, today: string): boolean {
  const next = paymentsPeriodRange(mode, shiftPaymentsAnchor(mode, anchor, 1));
  return next.from !== "" && next.from <= today;
}

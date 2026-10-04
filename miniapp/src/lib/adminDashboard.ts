/** Мелкая математика дашбордов админки — отдельно от разметки, чтобы тестировать. */

const MONTHS = ["янв", "фев", "мар", "апр", "мая", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"];

/** «2026-10-05» → «5 окт»; для недели — «с 5 окт». Непонятную строку отдаём как есть. */
export function dashBucketLabel(bucket: string, weekly: boolean): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(bucket);
  if (!m) return bucket;
  const month = MONTHS[Number(m[2]) - 1];
  if (!month) return bucket;
  const label = `${Number(m[3])} ${month}`;
  return weekly ? `с ${label}` : label;
}

/**
 * Высоты столбиков в процентах от максимума. Ненулевому значению оставляем
 * минимум 4%, иначе единица рядом с сотней не видна вовсе.
 */
export function dashBarHeights(values: number[]): number[] {
  const safe = values.map((v) => (Number.isFinite(v) && v > 0 ? v : 0));
  const max = Math.max(0, ...safe);
  if (max === 0) return safe.map(() => 0);
  return safe.map((v) => (v === 0 ? 0 : Math.max(4, (v / max) * 100)));
}

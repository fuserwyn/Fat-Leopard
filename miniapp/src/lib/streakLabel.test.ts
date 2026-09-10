import { describe, expect, it } from "vitest";
import {
  daysWordRu,
  streakStreakAriaLabel,
  cupsWordRu,
  streakBurnRemainingMs,
  formatStreakBurnRemaining,
  streakBurnLabel,
  effectiveStreakDays,
  canUseStreakSave,
  streakSaveHint,
  streakSaveWindowError,
} from "./streakLabel";

describe("daysWordRu", () => {
  it("singular for 1, 21, 101", () => {
    expect(daysWordRu(1)).toBe("день");
    expect(daysWordRu(21)).toBe("день");
    expect(daysWordRu(101)).toBe("день");
  });

  it("few (2-4) form", () => {
    expect(daysWordRu(2)).toBe("дня");
    expect(daysWordRu(3)).toBe("дня");
    expect(daysWordRu(4)).toBe("дня");
    expect(daysWordRu(22)).toBe("дня");
  });

  it("many form for 5-20, 0", () => {
    expect(daysWordRu(0)).toBe("дней");
    expect(daysWordRu(5)).toBe("дней");
    expect(daysWordRu(10)).toBe("дней");
    expect(daysWordRu(25)).toBe("дней");
    expect(daysWordRu(100)).toBe("дней");
  });

  it("special case 11-14 always many", () => {
    expect(daysWordRu(11)).toBe("дней");
    expect(daysWordRu(12)).toBe("дней");
    expect(daysWordRu(13)).toBe("дней");
    expect(daysWordRu(14)).toBe("дней");
    expect(daysWordRu(111)).toBe("дней");
    expect(daysWordRu(112)).toBe("дней");
  });
});

describe("cupsWordRu", () => {
  it("declensions", () => {
    expect(cupsWordRu(1)).toBe("кубок");
    expect(cupsWordRu(2)).toBe("кубка");
    expect(cupsWordRu(5)).toBe("кубков");
    expect(cupsWordRu(11)).toBe("кубков");
    expect(cupsWordRu(21)).toBe("кубок");
    expect(cupsWordRu(42)).toBe("кубка");
    expect(cupsWordRu(420)).toBe("кубков");
  });
});

describe("streakStreakAriaLabel", () => {
  it("empty state for 0 and negatives", () => {
    expect(streakStreakAriaLabel(0)).toBe("Стрик: пока нет дней подряд с тренировками");
    expect(streakStreakAriaLabel(-3)).toBe("Стрик: пока нет дней подряд с тренировками");
  });

  it("builds label with correct declension", () => {
    expect(streakStreakAriaLabel(1)).toBe("Стрик: 1 день подряд");
    expect(streakStreakAriaLabel(3)).toBe("Стрик: 3 дня подряд");
    expect(streakStreakAriaLabel(7)).toBe("Стрик: 7 дней подряд");
    expect(streakStreakAriaLabel(11)).toBe("Стрик: 11 дней подряд");
    expect(streakStreakAriaLabel(21)).toBe("Стрик: 21 день подряд");
  });
});

describe("streakBurnRemainingMs", () => {
  // 29 мая 2026, 18:00 локального времени.
  const now = new Date(2026, 4, 29, 18, 0, 0);
  const H = 3_600_000;

  it("нет стрика → null", () => {
    expect(streakBurnRemainingMs(0, 1, now)).toBeNull();
    expect(streakBurnRemainingMs(-1, 0, now)).toBeNull();
  });

  it("дата последней тренировки неизвестна (−1) → null", () => {
    expect(streakBurnRemainingMs(5, -1, now)).toBeNull();
  });

  it("стрик уже должен сгореть (≥2 дня без тренировки) → null", () => {
    expect(streakBurnRemainingMs(5, 2, now)).toBeNull();
    expect(streakBurnRemainingMs(5, 7, now)).toBeNull();
  });

  it("тренировался вчера → горит в конце сегодня (через 6 ч от 18:00)", () => {
    expect(streakBurnRemainingMs(5, 1, now)).toBe(6 * H);
  });

  it("тренировался сегодня → горит в конце завтра (через 30 ч от 18:00)", () => {
    expect(streakBurnRemainingMs(5, 0, now)).toBe(30 * H);
  });
});

describe("formatStreakBurnRemaining", () => {
  const H = 3_600_000;
  const M = 60_000;

  it("только минуты", () => {
    expect(formatStreakBurnRemaining(18 * M)).toBe("18 мин");
  });

  it("часы и минуты", () => {
    expect(formatStreakBurnRemaining(5 * H + 20 * M)).toBe("5 ч 20 мин");
  });

  it("дни и часы в компактном формате", () => {
    expect(formatStreakBurnRemaining(24 * H + 4 * H)).toBe("1д 4ч");
    expect(formatStreakBurnRemaining(2 * 24 * H + 3 * H)).toBe("2д 3ч");
    expect(formatStreakBurnRemaining(24 * H + 10 * H)).toBe("1д 10ч");
  });

  it("отрицательное/ноль → 0 мин", () => {
    expect(formatStreakBurnRemaining(-5000)).toBe("0 мин");
  });
});

describe("effectiveStreakDays", () => {
  const now = new Date(2026, 4, 29, 18, 0, 0);

  it("0 при сгорании по календарю (≥2 дня)", () => {
    expect(effectiveStreakDays(420, 2, now)).toBe(0);
    expect(effectiveStreakDays(420, 7, now)).toBe(0);
  });

  it("сохраняет стрик в grace-периоде", () => {
    expect(effectiveStreakDays(420, 0, now)).toBe(420);
    expect(effectiveStreakDays(420, 1, now)).toBe(420);
  });

  it("0 после дедлайна по lastTrainingDate", () => {
    const pastDeadline = new Date(2026, 4, 30, 0, 1, 0);
    expect(effectiveStreakDays(420, 1, pastDeadline, "2026-05-28")).toBe(0);
  });

  it("неизвестная дата — показываем сохранённый", () => {
    expect(effectiveStreakDays(10, -1, now)).toBe(10);
  });

  it("больничный: API days_since меньше сырой даты — стрик заморожен", () => {
    // Тренировка 01-05, сегодня 06-05, больничный со 02-05: API вернёт days_since=1, last_training_date=2026-05-01.
    const sickNow = new Date(2026, 4, 6, 12, 0, 0);
    expect(effectiveStreakDays(20, 1, sickNow, "2026-05-01")).toBe(20);
    expect(streakBurnLabel(20, 1, sickNow, "2026-05-01")).toBeNull();
  });
});

describe("streakBurnLabel", () => {
  const now = new Date(2026, 4, 29, 18, 0, 0);

  it("собирает подпись для живого стрика", () => {
    expect(streakBurnLabel(5, 1, now)).toBe("сгорит через 6 ч 0 мин");
  });

  it("null, когда стрик не горит", () => {
    expect(streakBurnLabel(0, 1, now)).toBeNull();
    expect(streakBurnLabel(5, 2, now)).toBeNull();
  });
});

describe("streakSaveWindow", () => {
  it("открыто только при daysSince === 2 и avail > 0", () => {
    expect(canUseStreakSave(2, 1)).toBe(true);
    expect(streakSaveWindowError(2, 1)).toBeNull();
    expect(canUseStreakSave(1, 1)).toBe(false);
    expect(streakSaveWindowError(1, 1)).toBe("not_needed");
    expect(streakSaveWindowError(3, 1)).toBe("too_late");
    expect(streakSaveWindowError(2, 0)).toBe("no_attempts");
  });

  it("подсказка для живого стрика", () => {
    expect(streakSaveHint(1, 1)).toMatch(/ещё жив/i);
  });
});

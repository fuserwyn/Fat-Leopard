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
  it("singular and plural", () => {
    expect(daysWordRu(1)).toBe("day");
    expect(daysWordRu(2)).toBe("days");
    expect(daysWordRu(0)).toBe("days");
    expect(daysWordRu(42)).toBe("days");
  });
});

describe("cupsWordRu", () => {
  it("declensions", () => {
    expect(cupsWordRu(1)).toBe("cup");
    expect(cupsWordRu(2)).toBe("cups");
    expect(cupsWordRu(420)).toBe("cups");
  });
});

describe("streakStreakAriaLabel", () => {
  it("empty state for 0 and negatives", () => {
    expect(streakStreakAriaLabel(0)).toBe("Streak: no consecutive workout days yet");
    expect(streakStreakAriaLabel(-3)).toBe("Streak: no consecutive workout days yet");
  });

  it("builds label", () => {
    expect(streakStreakAriaLabel(1)).toBe("Streak: 1 day in a row");
    expect(streakStreakAriaLabel(7)).toBe("Streak: 7 days in a row");
  });
});

describe("streakBurnRemainingMs", () => {
  const now = new Date(2026, 4, 29, 18, 0, 0);
  const H = 3_600_000;

  it("no streak → null", () => {
    expect(streakBurnRemainingMs(0, 1, now)).toBeNull();
  });

  it("streak already burned → null", () => {
    expect(streakBurnRemainingMs(5, 2, now)).toBeNull();
  });

  it("trained yesterday → burns end of today", () => {
    expect(streakBurnRemainingMs(5, 1, now)).toBe(6 * H);
  });

  it("trained today → burns end of tomorrow", () => {
    expect(streakBurnRemainingMs(5, 0, now)).toBe(30 * H);
  });
});

describe("formatStreakBurnRemaining", () => {
  const H = 3_600_000;
  const M = 60_000;

  it("minutes only", () => {
    expect(formatStreakBurnRemaining(18 * M)).toBe("18m");
  });

  it("hours and minutes", () => {
    expect(formatStreakBurnRemaining(5 * H + 20 * M)).toBe("5h 20m");
  });

  it("days and hours", () => {
    expect(formatStreakBurnRemaining(24 * H + 4 * H)).toBe("1d 4h");
  });
});

describe("effectiveStreakDays", () => {
  const now = new Date(2026, 4, 29, 18, 0, 0);

  it("0 when burned", () => {
    expect(effectiveStreakDays(420, 2, now)).toBe(0);
  });

  it("keeps streak in grace", () => {
    expect(effectiveStreakDays(420, 0, now)).toBe(420);
    expect(effectiveStreakDays(420, 1, now)).toBe(420);
  });
});

describe("streakBurnLabel", () => {
  const now = new Date(2026, 4, 29, 18, 0, 0);

  it("label for live streak", () => {
    expect(streakBurnLabel(5, 1, now)).toBe("streak resets in 6h 0m");
  });
});

describe("streakSaveWindow", () => {
  it("open only at daysSince === 2 with saves", () => {
    expect(canUseStreakSave(2, 1)).toBe(true);
    expect(streakSaveWindowError(1, 1)).toBe("not_needed");
  });

  it("hint for live streak", () => {
    expect(streakSaveHint(1, 1)).toMatch(/still alive/i);
  });
});

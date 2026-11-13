import { describe, expect, it } from "vitest";
import {
  canUseStreakSave,
  streakSaveHint,
  streakSaveWindowError,
} from "./streakLabel";
import { effectiveStreakDays } from "./streakLabel";

/**
 * Streak save window contract — mirrors StreakSaveWindowError / UseStreakSaveAttemptForAPI.
 * daysSince === 2 → streak already 0 in UI; one missed day can be closed.
 */
describe("streak save window contract", () => {
  const cases: Array<{
    days: number;
    avail: number;
    error: ReturnType<typeof streakSaveWindowError>;
    can: boolean;
  }> = [
    { days: 2, avail: 1, error: null, can: true },
    { days: 2, avail: 3, error: null, can: true },
    { days: 0, avail: 1, error: "not_needed", can: false },
    { days: 1, avail: 1, error: "not_needed", can: false },
    { days: 3, avail: 1, error: "too_late", can: false },
    { days: 10, avail: 2, error: "too_late", can: false },
    { days: 2, avail: 0, error: "no_attempts", can: false },
    { days: 1, avail: 0, error: "no_attempts", can: false },
    { days: -1, avail: 1, error: "no_training_history", can: false },
    { days: -1, avail: 0, error: "no_attempts", can: false },
  ];

  it.each(cases)("days=$days avail=$avail → $error", ({ days, avail, error, can }) => {
    expect(streakSaveWindowError(days, avail)).toBe(error);
    expect(canUseStreakSave(days, avail)).toBe(can);
  });
});

describe("streakSaveHint", () => {
  it("all reasons have user-facing copy", () => {
    expect(streakSaveHint(2, 1)).toMatch(/restore.*streak/i);
    expect(streakSaveHint(1, 1)).toMatch(/still alive|alive/i);
    expect(streakSaveHint(0, 1)).toMatch(/still alive|alive/i);
    expect(streakSaveHint(3, 1)).toMatch(/more than one day/i);
    expect(streakSaveHint(2, 0)).toMatch(/no saves|attempts/i);
    expect(streakSaveHint(-1, 1)).toMatch(/no workouts|training/i);
  });
});

describe("streak still alive → save disabled", () => {
  it("18.07, last training 17.07, attempts left", () => {
    const stored = 107;
    const daysSince = 1;
    const avail = 1;
    expect(effectiveStreakDays(stored, daysSince)).toBe(107);
    expect(canUseStreakSave(daysSince, avail)).toBe(false);
    expect(streakSaveWindowError(daysSince, avail)).toBe("not_needed");
    expect(streakSaveHint(daysSince, avail)).toMatch(/log a workout/i);
  });

  it("morning after a miss (days=2) — window open", () => {
    const stored = 107;
    const daysSince = 2;
    expect(effectiveStreakDays(stored, daysSince)).toBe(0);
    expect(canUseStreakSave(daysSince, 1)).toBe(true);
    expect(streakSaveHint(daysSince, 1)).toMatch(/restore.*streak/i);
  });
});

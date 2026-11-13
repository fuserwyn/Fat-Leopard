import { describe, expect, it } from "vitest";
import { achievementLabel, earnedAchievementKeys, freshAchievementKeys } from "./achievements";

describe("earnedAchievementKeys", () => {
  it("без прогресса — пусто", () => {
    expect(earnedAchievementKeys(0, 0)).toEqual([]);
  });

  it("стрик-ачивки берёт первые N порогов по achievementCount", () => {
    expect(earnedAchievementKeys(2, 0)).toEqual(["streak-7", "streak-14"]);
  });

  it("первая же тренировка открывает ачивку", () => {
    expect(earnedAchievementKeys(0, 1)).toEqual(["workout-1"]);
  });

  it("ачивки за тренировки открываются по достижению порога", () => {
    expect(earnedAchievementKeys(0, 10)).toEqual(["workout-1", "workout-10"]);
    expect(earnedAchievementKeys(0, 42)).toEqual(["workout-1", "workout-10", "workout-20", "workout-42"]);
  });

  it("комбинирует стрик- и тренировочные ачивки", () => {
    expect(earnedAchievementKeys(1, 20)).toEqual(["streak-7", "workout-1", "workout-10", "workout-20"]);
  });

  it("achievementCount сверх числа порогов не ломает результат", () => {
    expect(earnedAchievementKeys(99, 0)).toHaveLength(9);
  });
});

describe("freshAchievementKeys", () => {
  it("новичок празднует первую тренировку", () => {
    expect(freshAchievementKeys(["workout-1"], [])).toEqual(["workout-1"]);
  });

  it("уже виденное не празднуется повторно", () => {
    expect(freshAchievementKeys(["workout-1", "workout-10"], ["workout-1", "workout-10"])).toEqual([]);
  });

  it("ветеран не получает тост за порог, добавленный в каталог задним числом", () => {
    // "workout-1" появился в каталоге позже, когда у человека уже было 300 тренировок.
    const earned = ["workout-1", "workout-10", "workout-20"];
    const seen = ["workout-10", "workout-20"];
    expect(freshAchievementKeys(earned, seen)).toEqual([]);
  });

  it("настоящий новый рекорд празднуется, даже если рядом есть старый пропущенный порог", () => {
    const earned = ["workout-1", "workout-10", "workout-20", "workout-42"];
    const seen = ["workout-10", "workout-20"];
    expect(freshAchievementKeys(earned, seen)).toEqual(["workout-42"]);
  });

  it("виды не смешиваются: стрик не глушит тренировочную ачивку", () => {
    const earned = ["streak-7", "streak-14", "workout-1"];
    const seen = ["streak-7"];
    expect(freshAchievementKeys(earned, seen)).toEqual(["streak-14", "workout-1"]);
  });
});

describe("achievementLabel", () => {
  it("English labels", () => {
    expect(achievementLabel("streak-7")).toBe("7-day streak");
    expect(achievementLabel("streak-42")).toBe("42-day streak");
    expect(achievementLabel("workout-1")).toBe("1 workout");
    expect(achievementLabel("workout-10")).toBe("10 workouts");
    expect(achievementLabel("workout-42")).toBe("42 workouts");
  });
});

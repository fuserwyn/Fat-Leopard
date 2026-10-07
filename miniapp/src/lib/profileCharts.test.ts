import { describe, expect, it } from "vitest";
import {
  buildStreakByDay,
  computeStreakDaysAfterReport,
  effectiveStreakAtDay,
  type WorkoutDayPoint,
} from "./profileCharts";

describe("computeStreakDaysAfterReport", () => {
  it("starts at 1 without history", () => {
    expect(computeStreakDaysAfterReport(null, 0, "2026-09-01")).toBe(1);
  });

  it("increments on consecutive days", () => {
    expect(computeStreakDaysAfterReport("2026-08-31", 3, "2026-09-01")).toBe(4);
  });

  it("resets after a gap", () => {
    expect(computeStreakDaysAfterReport("2026-08-29", 5, "2026-09-01")).toBe(1);
  });
});

describe("effectiveStreakAtDay", () => {
  it("burns after two days without training", () => {
    expect(effectiveStreakAtDay(10, 2)).toBe(0);
    expect(effectiveStreakAtDay(10, 1)).toBe(10);
  });
});

describe("buildStreakByDay", () => {
  it("tracks streak across workout and rest days", () => {
    const days: WorkoutDayPoint[] = [
      { date: "2026-08-29", count: 1 },
      { date: "2026-08-30", count: 0 },
      { date: "2026-08-31", count: 1 },
      { date: "2026-09-01", count: 0 },
      { date: "2026-09-02", count: 0 },
      { date: "2026-09-03", count: 1 },
    ];
    const streak = buildStreakByDay(days);
    // Как на бэкенде (ComputeStreakDays): тренировка после пропущенного дня
    // начинает стрик с 1; без тренировки он держится один день и сгорает на второй.
    expect(streak.map((p) => p.value)).toEqual([1, 1, 1, 1, 0, 1]);
  });
});

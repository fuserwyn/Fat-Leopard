import { describe, expect, it } from "vitest";
import {
  PACK_WEEKLY_GOAL_HINT,
  packWeeklyProgressBarPct,
  packWeeklyProgressLabel,
  packWeeklyStepFillPcts,
} from "./packWeeklyGoal";

describe("packWeeklyGoal", () => {
  it("computes progress bar percentage capped at 100", () => {
    expect(packWeeklyProgressBarPct(0, 50)).toBe(0);
    expect(packWeeklyProgressBarPct(25, 50)).toBe(50);
    expect(packWeeklyProgressBarPct(50, 50)).toBe(100);
    expect(packWeeklyProgressBarPct(75, 50)).toBe(100);
  });

  it("formats label", () => {
    expect(packWeeklyProgressLabel(42, 50)).toBe("42/50");
    expect(packWeeklyProgressLabel(-1, 0)).toBe("0/50");
  });

  it("computes 5-step segment fills", () => {
    expect(packWeeklyStepFillPcts(0, 50)).toEqual([0, 0, 0, 0, 0]);
    expect(packWeeklyStepFillPcts(10, 50)).toEqual([100, 0, 0, 0, 0]);
    expect(packWeeklyStepFillPcts(25, 50)).toEqual([100, 100, 50, 0, 0]);
    expect(packWeeklyStepFillPcts(42, 50)).toEqual([100, 100, 100, 100, 20]);
    expect(packWeeklyStepFillPcts(50, 50)).toEqual([100, 100, 100, 100, 100]);
    expect(packWeeklyStepFillPcts(60, 50)).toEqual([100, 100, 100, 100, 100]);
  });

  it("describes weekly goal reward in hint", () => {
    expect(PACK_WEEKLY_GOAL_HINT).toMatch(/50 тренировок/i);
    expect(PACK_WEEKLY_GOAL_HINT).toMatch(/50 дополнительных кубков/i);
  });
});

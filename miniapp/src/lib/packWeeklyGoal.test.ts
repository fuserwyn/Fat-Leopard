import { describe, expect, it } from "vitest";
import {
  PACK_WEEKLY_GOAL_HINT,
  packWeeklyProgressBarPct,
  packWeeklyProgressLabel,
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

  it("describes weekly goal reward in hint", () => {
    expect(PACK_WEEKLY_GOAL_HINT).toMatch(/50 тренировок/i);
    expect(PACK_WEEKLY_GOAL_HINT).toMatch(/50 дополнительных кубков/i);
  });
});

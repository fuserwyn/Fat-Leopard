import { describe, expect, it } from "vitest";
import { packWeeklyProgressBarPct, packWeeklyProgressLabel } from "./packWeeklyGoal";

describe("packWeeklyGoal", () => {
  it("computes progress bar percentage capped at 100", () => {
    expect(packWeeklyProgressBarPct(0, 100)).toBe(0);
    expect(packWeeklyProgressBarPct(50, 100)).toBe(50);
    expect(packWeeklyProgressBarPct(100, 100)).toBe(100);
    expect(packWeeklyProgressBarPct(150, 100)).toBe(100);
  });

  it("formats label", () => {
    expect(packWeeklyProgressLabel(42, 100)).toBe("42/100");
    expect(packWeeklyProgressLabel(-1, 0)).toBe("0/100");
  });
});

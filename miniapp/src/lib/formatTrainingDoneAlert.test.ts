import { describe, expect, it } from "vitest";
import { formatTrainingDoneAlert } from "./formatTrainingDoneAlert";

describe("formatTrainingDoneAlert", () => {
  it("returns non-workout text unchanged", () => {
    expect(formatTrainingDoneAlert(["Hello from Leo"])).toBe("Hello from Leo");
  });

  it("compacts English report accepted", () => {
    const parts = [
      "✅ Report accepted! 💪",
      "🦁 Streak: 3 days",
      "🏆 +1 cup (total: 10)",
    ];
    const out = formatTrainingDoneAlert(parts);
    expect(out).toContain("✅ Report accepted!");
    expect(out).toContain("Streak: 3 days");
    expect(out).toContain("+1 cup");
  });

  it("normalizes legacy Russian title and streak", () => {
    const parts = [
      "✅ Отчёт принят! 💪",
      "🦁 Стрик: 2 дня",
      "🏆 +1 кубок за тренировку!",
      "🏆 Всего кубков: 5",
    ];
    const out = formatTrainingDoneAlert(parts);
    expect(out).toMatch(/^✅ Workout logged!/);
    expect(out).toContain("Streak: 2 days");
    expect(out).toContain("+1 cup for this workout");
    expect(out).toContain("Total cups: 5");
  });
});

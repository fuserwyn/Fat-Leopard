import { describe, expect, it } from "vitest";
import { WORKOUT_NOTE_PLACEHOLDER } from "./workoutNote";

describe("WORKOUT_NOTE_PLACEHOLDER", () => {
  it("подсказывает отметить напарника на совместной тренировке через @ник", () => {
    expect(WORKOUT_NOTE_PLACEHOLDER).toContain("@ник");
    expect(WORKOUT_NOTE_PLACEHOLDER).toMatch(/вместе/);
  });
});

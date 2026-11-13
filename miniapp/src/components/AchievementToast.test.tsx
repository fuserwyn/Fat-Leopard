// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import { AchievementToast, leoCheer } from "./AchievementToast";

afterEach(cleanup);

describe("AchievementToast", () => {
  it("celebrates first workout with a dedicated phrase", () => {
    const { getByText } = render(<AchievementToast achievementKey="workout-1" onDone={() => {}} />);
    expect(getByText("Achievement unlocked!")).toBeTruthy();
    expect(getByText("1 workout")).toBeTruthy();
    expect(getByText(/First workout/)).toBeTruthy();
  });

  it("uses standard cheers for other thresholds", () => {
    expect(leoCheer("workout-10")).toMatch(/beast.*Leo gives you a paw/);
    expect(leoCheer("streak-7")).toMatch(/Keep it up.*proud of you/);
  });
});

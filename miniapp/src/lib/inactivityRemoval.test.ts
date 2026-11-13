import { describe, expect, it } from "vitest";
import {
  formatDurationToDays,
  inactiveDaysFromRemovalRemaining,
  removalRemainingUntil,
} from "./inactivityRemoval";

describe("formatDurationToDays", () => {
  it("days + hours", () => {
    expect(formatDurationToDays(6 * 24 * 60 * 60_000 + 22 * 60 * 60_000)).toBe("6 days 22 hr");
  });

  it("days only", () => {
    expect(formatDurationToDays(2 * 24 * 60 * 60_000)).toBe("2 days");
    expect(formatDurationToDays(1 * 24 * 60 * 60_000)).toBe("1 day");
    expect(formatDurationToDays(5 * 24 * 60 * 60_000)).toBe("5 days");
  });

  it("hours and minutes", () => {
    expect(formatDurationToDays(22 * 60 * 60_000)).toBe("22 hr");
    expect(formatDurationToDays(90 * 60_000)).toBe("1 hr 30 min");
    expect(formatDurationToDays(45 * 60_000)).toBe("45 min");
  });

  it("zero / negative → 0 min", () => {
    expect(formatDurationToDays(0)).toBe("0 min");
    expect(formatDurationToDays(-1000)).toBe("0 min");
  });
});

describe("removalRemainingUntil", () => {
  it("returns null for past deadline / invalid", () => {
    expect(removalRemainingUntil("2020-01-01T00:00:00+03:00", new Date("2025-01-01"))).toBeNull();
    expect(removalRemainingUntil("not-a-date", new Date())).toBeNull();
  });

  it("computes remaining from RFC3339", () => {
    const now = new Date(2026, 6, 1, 1, 12);
    const at = new Date(2026, 6, 8, 0, 0).toISOString();
    const r = removalRemainingUntil(at, now);
    expect(r).not.toBeNull();
    expect(r!.text).toBe("6 days 22 hr");
    expect(r!.ms).toBeGreaterThan(0);
    expect(r!.deadlineLocal).toMatch(/07\/08\/2026/);
  });

  it("22h until midnight", () => {
    const now = new Date("2026-07-01T02:00:00+03:00");
    const at = "2026-07-02T00:00:00+03:00";
    const r = removalRemainingUntil(at, now);
    expect(r).not.toBeNull();
    expect(r!.text).toBe("22 hr");
  });
});

describe("inactiveDaysFromRemovalRemaining", () => {
  const H = 3_600_000;

  it("maps 72/48/24h milestones", () => {
    expect(inactiveDaysFromRemovalRemaining(72 * H)).toBe(5);
    expect(inactiveDaysFromRemovalRemaining(48 * H)).toBe(6);
    expect(inactiveDaysFromRemovalRemaining(24 * H)).toBe(7);
  });
});

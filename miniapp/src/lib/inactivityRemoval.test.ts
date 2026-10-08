import { describe, expect, it } from "vitest";
import {
  formatDurationToDays,
  inactiveDaysFromRemovalRemaining,
  removalRemainingUntil,
} from "./inactivityRemoval";

describe("formatDurationToDays", () => {
  it("days + hours (кейс больничного 6д 22ч)", () => {
    expect(formatDurationToDays(6 * 24 * 60 * 60_000 + 22 * 60 * 60_000)).toBe("6 дней 22 ч.");
  });

  it("только дни", () => {
    expect(formatDurationToDays(2 * 24 * 60 * 60_000)).toBe("2 дня");
    expect(formatDurationToDays(1 * 24 * 60 * 60_000)).toBe("1 день");
    expect(formatDurationToDays(5 * 24 * 60 * 60_000)).toBe("5 дней");
  });

  it("часы и минуты", () => {
    expect(formatDurationToDays(22 * 60 * 60_000)).toBe("22 ч.");
    expect(formatDurationToDays(90 * 60_000)).toBe("1 ч. 30 мин.");
    expect(formatDurationToDays(45 * 60_000)).toBe("45 мин.");
  });

  it("отрицательное / ноль → 0 мин.", () => {
    expect(formatDurationToDays(0)).toBe("0 мин.");
    expect(formatDurationToDays(-1000)).toBe("0 мин.");
  });
});

describe("removalRemainingUntil", () => {
  it("returns null for past deadline / invalid", () => {
    expect(removalRemainingUntil("2020-01-01T00:00:00+03:00", new Date("2025-01-01"))).toBeNull();
    expect(removalRemainingUntil("not-a-date", new Date())).toBeNull();
  });

  it("computes remaining from RFC3339", () => {
    // Локальное время устройства: тест не зависит от часового пояса контейнера.
    const now = new Date(2026, 6, 1, 1, 12);
    const at = new Date(2026, 6, 8, 0, 0).toISOString();
    const r = removalRemainingUntil(at, now);
    expect(r).not.toBeNull();
    expect(r!.text).toBe("6 дней 22 ч.");
    expect(r!.ms).toBeGreaterThan(0);
    expect(r!.deadlineLocal).toMatch(/08\.07\.2026/);
  });

  it("frozen 22ч (ровно до полуночи)", () => {
    const now = new Date("2026-07-01T02:00:00+03:00");
    const at = "2026-07-02T00:00:00+03:00";
    const r = removalRemainingUntil(at, now);
    expect(r).not.toBeNull();
    expect(r!.text).toBe("22 ч.");
  });
});

describe("inactiveDaysFromRemovalRemaining", () => {
  const H = 3_600_000;

  it("maps 72/48/24h milestones to profile days 5/6/7", () => {
    expect(inactiveDaysFromRemovalRemaining(80 * H)).toBeNull();
    expect(inactiveDaysFromRemovalRemaining(72.1 * H)).toBeNull();
    expect(inactiveDaysFromRemovalRemaining(72 * H)).toBe(5); // ровно 72ч → day 5
    expect(inactiveDaysFromRemovalRemaining(60 * H)).toBe(5);
    expect(inactiveDaysFromRemovalRemaining(48.1 * H)).toBe(5);
    expect(inactiveDaysFromRemovalRemaining(48 * H)).toBe(6);
    expect(inactiveDaysFromRemovalRemaining(36 * H)).toBe(6);
    expect(inactiveDaysFromRemovalRemaining(24.1 * H)).toBe(6);
    expect(inactiveDaysFromRemovalRemaining(24 * H)).toBe(7);
    expect(inactiveDaysFromRemovalRemaining(12 * H)).toBe(7);
    expect(inactiveDaysFromRemovalRemaining(1)).toBe(7);
  });

  it("sync with kick banner: only day 7 shows kick UI", () => {
    // 6д 22ч остатка → ещё не в окне предупреждений профиля
    expect(inactiveDaysFromRemovalRemaining(6 * 24 * H + 22 * H)).toBeNull();
    // 22ч → day 7 → баннер
    expect(inactiveDaysFromRemovalRemaining(22 * H)).toBe(7);
  });
});

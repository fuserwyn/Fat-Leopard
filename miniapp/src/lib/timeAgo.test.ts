import { describe, expect, it } from "vitest";
import { formatChatTime, timeAgoFromISO } from "./timeAgo";

describe("timeAgoFromISO", () => {
  const now = Date.parse("2026-07-18T15:00:00+03:00");

  it("buckets", () => {
    expect(timeAgoFromISO("2026-07-18T14:59:30+03:00", now)).toBe("только что");
    expect(timeAgoFromISO("2026-07-18T14:45:00+03:00", now)).toBe("15 мин назад");
    expect(timeAgoFromISO("2026-07-18T12:00:00+03:00", now)).toBe("3 ч назад");
    expect(timeAgoFromISO("2026-07-16T15:00:00+03:00", now)).toBe("2 дня назад");
  });

  it("invalid → dash", () => {
    expect(timeAgoFromISO("nope", now)).toBe("—");
  });
});

describe("formatChatTime", () => {
  it("formats ISO and millis", () => {
    // Локальное время устройства: тест не зависит от часового пояса контейнера.
    const iso = new Date(2026, 6, 18, 15, 30).toISOString();
    const fromIso = formatChatTime(iso);
    const fromMs = formatChatTime(Date.parse(iso));
    expect(fromIso).toMatch(/18\.07\.2026/);
    expect(fromIso).toMatch(/15:30/);
    expect(fromMs).toBe(fromIso);
  });

  it("invalid → empty", () => {
    expect(formatChatTime("bad")).toBe("");
  });
});

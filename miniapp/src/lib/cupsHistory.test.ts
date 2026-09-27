import { describe, expect, it } from "vitest";
import {
  CUPS_HISTORY_CAPTION,
  CUPS_HISTORY_LIMIT,
  PACK_WEEKLY_CUPS_LABEL,
  buildCupsHistoryRows,
  formatCupsHistoryDate,
} from "./cupsHistory";

describe("formatCupsHistoryDate", () => {
  it("formats a calendar day without shifting timezone", () => {
    expect(formatCupsHistoryDate("2026-09-27")).toBe("27.09.2026");
  });
});

describe("buildCupsHistoryRows", () => {
  it("keeps the caption limit of 42 newest workouts", () => {
    expect(CUPS_HISTORY_CAPTION).toBe("42 последние тренировки");
    expect(CUPS_HISTORY_LIMIT).toBe(42);
    const workouts = Array.from({ length: 43 }, (_, i) => ({
      date: "2026-01-01",
      messageText: "бег, 30 мин, интенсивность 3/5",
      cups: 10,
      createdAt: new Date(Date.UTC(2026, 0, 1, 0, i)).toISOString(),
    }));
    const rows = buildCupsHistoryRows(workouts, []);
    expect(rows).toHaveLength(42);
    expect(rows[0]?.sortAt).toBeGreaterThan(rows[41]?.sortAt ?? 0);
    expect(rows.every((row) => row.kind === "workout")).toBe(true);
  });

  it("formats date, type, intensity, duration and cups", () => {
    const [row] = buildCupsHistoryRows(
      [
        {
          date: "2026-09-27",
          messageText: "бег + плавание, 40 мин, интенсивность 4/5",
          cups: 22,
          createdAt: "2026-09-27T10:00:00Z",
        },
      ],
      [],
    );
    expect(row?.line).toBe("27.09.2026 — бег + плавание — 4/5 — 40 мин — +22 кубка");
  });

  it("keeps every pack weekly bonus while the workout list is shorter than the limit", () => {
    const rows = buildCupsHistoryRows(
      [
        {
          date: "2026-09-27",
          messageText: "силовая, 45 мин, интенсивность 5/5",
          cups: 45,
          createdAt: "2026-09-27T18:00:00Z",
        },
      ],
      [{ date: "2026-09-01", cups: 50, createdAt: "2026-09-01T12:00:00Z" }],
    );
    expect(rows.filter((row) => row.kind === "pack_weekly")).toHaveLength(1);
    expect(rows[1]?.line).toBe(`01.09.2026 — ${PACK_WEEKLY_CUPS_LABEL} — — — — — +50 кубков`);
  });

  it("drops a pack weekly bonus older than the last 42 workouts", () => {
    const workouts = Array.from({ length: 43 }, (_, i) => ({
      date: "2026-08-01",
      messageText: "бег, 30 мин, интенсивность 3/5",
      cups: 10,
      createdAt: new Date(Date.UTC(2026, 7, 1, 0, i)).toISOString(),
    }));
    const oldestKept = workouts[1]?.createdAt ?? "";
    const rows = buildCupsHistoryRows(
      workouts,
      [
        { date: "2026-07-01", cups: 50, createdAt: "2026-07-01T00:00:00Z" },
        { date: "2026-08-01", cups: 50, createdAt: oldestKept },
      ],
      42,
    );
    const weekly = rows.filter((row) => row.kind === "pack_weekly");
    expect(rows.filter((row) => row.kind === "workout")).toHaveLength(42);
    expect(weekly).toHaveLength(1);
    expect(weekly[0]?.cupsLabel).toBe("+50 кубков");
  });

  it("shows weekly cups when there are no workouts yet", () => {
    const rows = buildCupsHistoryRows([], [{ date: "2026-09-22", cups: 1, createdAt: "2026-09-22T15:00:00Z" }]);
    expect(rows).toHaveLength(1);
    expect(rows[0]?.line).toBe(`22.09.2026 — ${PACK_WEEKLY_CUPS_LABEL} — — — — — +1 кубок`);
  });
});

import { afterEach, describe, expect, it, vi } from "vitest";
import { contractProblems, loadBackendRoutes } from "./backendContract";
import { importWithApi, mockFetch } from "./testApi";
import {
  packWeekRangeLabel,
  packWeekSummaryHeadline,
  packWeekSummaryNextLine,
  parsePackWeekSummary,
  workoutsWord,
  type PackWeekSummary,
} from "./packWeekSummary";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

const raw = {
  week_start: "2026-10-05",
  week_end: "2026-10-11",
  workouts: 78,
  goal: 75,
  goal_reached: true,
  next_goal: 80,
  participants: 12,
  my_workouts: 4,
};

const closed: PackWeekSummary = {
  weekStart: "2026-10-05",
  weekEnd: "2026-10-11",
  workouts: 78,
  goal: 75,
  goalReached: true,
  nextGoal: 80,
  participants: 12,
  myWorkouts: 4,
};

describe("pack week summary", () => {
  it("parses the server answer", () => {
    expect(parsePackWeekSummary(raw)).toEqual(closed);
    expect(parsePackWeekSummary(null)).toBeNull();
    expect(parsePackWeekSummary({ ...raw, week_start: "вчера" })).toBeNull();
    expect(parsePackWeekSummary({ ...raw, goal: 0 })).toBeNull();
    expect(parsePackWeekSummary({ week_start: "2026-10-05", goal: 75 })).toEqual({
      weekStart: "2026-10-05", weekEnd: "", workouts: 0, goal: 75, goalReached: false, nextGoal: 75, participants: 0, myWorkouts: 0,
    });
  });

  it("labels the week range", () => {
    expect(packWeekRangeLabel("2026-10-05", "2026-10-11")).toBe("5–11 октября");
    expect(packWeekRangeLabel("2026-09-28", "2026-10-04")).toBe("28 сентября – 4 октября");
    expect(packWeekRangeLabel("2026-09-28", "")).toBe("с 28 сентября");
    expect(packWeekRangeLabel("", "")).toBe("");
    expect(packWeekRangeLabel("2026-13-01", "")).toBe("");
  });

  it("declines «тренировка»", () => {
    expect([1, 2, 5, 11, 12, 21, 22, 75, 80, 104].map(workoutsWord)).toEqual([
      "тренировка", "тренировки", "тренировок", "тренировок", "тренировок",
      "тренировка", "тренировки", "тренировок", "тренировок", "тренировки",
    ]);
  });

  it("explains what happens next", () => {
    expect(packWeekSummaryHeadline(closed)).toBe("Цель недели закрыта!");
    expect(packWeekSummaryNextLine(closed)).toBe("Планка растёт: цель новой недели — 80 тренировок (+5).");
    expect(packWeekSummaryNextLine({ ...closed, nextGoal: 75 })).toBe("Цель новой недели — 75 тренировок.");
    const open = { ...closed, workouts: 70, goalReached: false, nextGoal: 75 };
    expect(packWeekSummaryHeadline(open)).toBe("Цель недели не закрыта");
    expect(packWeekSummaryNextLine(open)).toMatch(/Не хватило 5 тренировок\. Цель новой недели та же — 75/);
  });
});

describe("pack week summary API", () => {
  const backend = loadBackendRoutes();
  const load = () => importWithApi(() => import("./packWeekSummary"));

  it("loads the pending summary", async () => {
    const api = await load();
    const calls = mockFetch(() => ({ json: { ok: true, summary: raw } }));
    expect(await api.fetchPackWeekSummary("i")).toEqual(closed);
    expect(calls[0].path).toBe("/api/miniapp/pack/week-summary");
    expect(calls[0].body).toEqual({ init_data: "i" });
    if (backend) expect(contractProblems(backend, calls[0])).toEqual([]);

    mockFetch(() => ({ json: { ok: true, summary: null } }));
    expect(await api.fetchPackWeekSummary("i")).toBeNull();
    mockFetch(() => ({ status: 500, json: { error: "summary_failed" } }));
    expect(await api.fetchPackWeekSummary("i")).toBeNull();
    mockFetch(() => ({ fail: true }));
    expect(await api.fetchPackWeekSummary("i")).toBeNull();
    expect(await api.fetchPackWeekSummary(" ")).toBeNull();
  });

  it("marks the summary as seen", async () => {
    const api = await load();
    const calls = mockFetch(() => ({ json: { ok: true } }));
    expect(await api.markPackWeekSummarySeen("i", "2026-10-05")).toBe(true);
    expect(calls[0].path).toBe("/api/miniapp/pack/week-summary/seen");
    expect(calls[0].body).toEqual({ init_data: "i", week_start: "2026-10-05" });
    if (backend) expect(contractProblems(backend, calls[0])).toEqual([]);

    mockFetch(() => ({ status: 404, json: { error: "summary_not_found" } }));
    expect(await api.markPackWeekSummarySeen("i", "2026-10-05")).toBe(false);
    mockFetch(() => ({ fail: true }));
    expect(await api.markPackWeekSummarySeen("i", "2026-10-05")).toBe(false);
    expect(await api.markPackWeekSummarySeen("i", "")).toBe(false);
  });

  it("does nothing without an API address", async () => {
    const api = await importWithApi(() => import("./packWeekSummary"), "");
    const calls = mockFetch(() => ({ json: { ok: true } }));
    expect(await api.fetchPackWeekSummary("i")).toBeNull();
    expect(await api.markPackWeekSummarySeen("i", "2026-10-05")).toBe(false);
    expect(calls).toHaveLength(0);
  });
});

// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import type { AdminAnalytics } from "../lib/adminApi";

const fetchAdminAnalytics = vi.fn();
vi.mock("../lib/adminApi", () => ({ fetchAdminAnalytics: (...a: unknown[]) => fetchAdminAnalytics(...a) }));

import { AdminDashboardScreen } from "./AdminDashboardScreen";

afterEach(() => {
  cleanup();
  fetchAdminAnalytics.mockReset();
});

const full: AdminAnalytics = {
  period: "30д",
  last_event_at: "04.10 08:00",
  tables: [
    {
      kind: "funnel",
      title: "1️⃣ Воронка: бот → оплата",
      subtitle: "",
      columns: ["Стадия", "Юзеры", "Конв"],
      rows: [
        ["Старт бота", "10", "—"],
        ["Оплатил", "4", "40%"],
      ],
    },
    // Заголовок с «KPI» без kind=funnel не должен рисоваться воронкой.
    { kind: "kpi", title: "⭐ Сводка KPI", subtitle: "", columns: ["Метрика", "Знач"], rows: [["Активация", "40%"]] },
  ],
  dashboard: {
    compared: true,
    kpis: [
      { key: "active", label: "Тренировались", value: "12", delta: "+20%", trend: "up" },
      { key: "activation", label: "Активация (трен/опл)", value: "40%", delta: "−5.0 п.п.", trend: "down", target: ">35%", tone: "good" },
    ],
    series_bucket: "day",
    series: [
      {
        key: "workouts",
        label: "Тренировок",
        total: 5,
        points: [
          { bucket: "2026-10-03", value: 2 },
          { bucket: "2026-10-04", value: 3 },
        ],
      },
      { key: "payments", label: "Оплат", total: 0, points: [{ bucket: "2026-10-03", value: 0 }] },
    ],
    active: { day: 3, week: 8, month: 12, stickiness: "25%" },
    cohorts: [{ week_start: "2026-09-28", size: 4, d1: "75%", d7: "—", d30: "—" }],
    money: [{ label: "Доступ ₽", count: "4", amount: "840 ₽" }],
    pack_weeks: [
      { week_start: "2026-09-28", workouts: 60, goal: 50, reached: true, current: true },
      { week_start: "2026-09-21", workouts: 30, goal: 50, reached: false, current: false },
    ],
    channels: [{ source: "organic", started: 10, paid: 4, conv: "40%" }],
    visits: { visits: 40, unique: 15 },
  },
};

describe("AdminDashboardScreen", () => {
  it("requests dashboard data and renders every block", async () => {
    fetchAdminAnalytics.mockResolvedValue({ analytics: full });
    render(<AdminDashboardScreen initData="x" showAlert={() => {}} />);
    await waitFor(() => expect(screen.getByText("KPI")).toBeTruthy());

    expect(fetchAdminAnalytics).toHaveBeenCalledWith("x", 30, true);
    expect(screen.getByText("+20%").className).toContain("dash-kpi__delta--up");
    expect(screen.getByText("−5.0 п.п.").className).toContain("dash-kpi__delta--down");
    expect(screen.getByText("Динамика по дням")).toBeTruthy();
    expect(screen.getByText("Воронка: бот → оплата")).toBeTruthy();
    expect(screen.queryByText("Сводка KPI")).toBeNull();
    expect(screen.getByText("Удержание по когортам")).toBeTruthy();
    expect(screen.getByText("840 ₽")).toBeTruthy();
    expect(screen.getByText("Недельная цель стаи")).toBeTruthy();
    expect(screen.getByText("organic")).toBeTruthy();
    expect(screen.getByText(/Посещения бота за период: 40/)).toBeTruthy();
  });

  it("shows «нет данных» instead of empty charts", async () => {
    fetchAdminAnalytics.mockResolvedValue({
      analytics: {
        ...full,
        tables: [],
        dashboard: {
          ...full.dashboard!,
          compared: false,
          kpis: null,
          series: null,
          cohorts: null,
          money: null,
          pack_weeks: null,
          channels: null,
        },
      },
    });
    render(<AdminDashboardScreen initData="x" showAlert={() => {}} />);
    await waitFor(() => expect(screen.getByText("Деньги")).toBeTruthy());
    expect(screen.getAllByText("Нет данных за этот период.").length).toBe(5);
  });
});

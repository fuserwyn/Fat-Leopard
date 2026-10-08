// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import { PackWeekSummaryModal } from "./PackWeekSummaryModal";
import type { PackWeekSummary } from "../lib/packWeekSummary";

afterEach(cleanup);

const summary: PackWeekSummary = {
  weekStart: "2026-10-05",
  weekEnd: "2026-10-11",
  workouts: 78,
  goal: 75,
  goalReached: true,
  nextGoal: 80,
  participants: 12,
  myWorkouts: 4,
};

describe("PackWeekSummaryModal", () => {
  it("shows the week result, own contribution and the raised goal", () => {
    const { getByText, getByRole, container } = render(<PackWeekSummaryModal summary={summary} onClose={() => {}} />);
    expect(getByRole("dialog").className).toContain("is-reached");
    expect(getByText("Цель недели закрыта!")).toBeTruthy();
    expect(getByText(/5–11 октября/)).toBeTruthy();
    expect(container.querySelector(".pack-week-summary__count")?.textContent).toBe("78");
    expect(container.querySelector(".pack-week-summary__goal")?.textContent).toBe("/ 75");
    expect(getByText("твой вклад").previousSibling?.textContent).toBe("4");
    expect(getByText("леопардов в зачёте").previousSibling?.textContent).toBe("12");
    expect(getByText(/цель новой недели — 80 тренировок \(\+5\)/)).toBeTruthy();
    expect((container.querySelector(".pack-week-summary__bar-fill") as HTMLElement).style.width).toBe("100%");
  });

  it("shows an unreached week without the glow", () => {
    const open = { ...summary, workouts: 60, goalReached: false, nextGoal: 75 };
    const { getByText, getByRole } = render(<PackWeekSummaryModal summary={open} onClose={() => {}} />);
    expect(getByRole("dialog").className).not.toContain("is-reached");
    expect(getByText("Цель недели не закрыта")).toBeTruthy();
    expect(getByText(/Не хватило 15 тренировок/)).toBeTruthy();
  });

  it("closes by the button, the backdrop and Escape, but not by a tap on the card", () => {
    const onClose = vi.fn();
    const { getByText, getByRole, container } = render(<PackWeekSummaryModal summary={summary} onClose={onClose} />);
    fireEvent.click(getByRole("dialog"));
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(getByText("Вперёд, стая!"));
    fireEvent.click(container.querySelector(".pack-week-summary-overlay")!);
    fireEvent.keyDown(window, { key: "Escape" });
    fireEvent.keyDown(window, { key: "Enter" });
    expect(onClose).toHaveBeenCalledTimes(3);
  });
});

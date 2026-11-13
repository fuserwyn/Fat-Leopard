// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { RulesScreen } from "./RulesScreen";

beforeEach(() => {
  localStorage.setItem("leo-ui-lang", "en");
});

afterEach(() => {
  cleanup();
  localStorage.removeItem("leo-ui-lang");
});

describe("RulesScreen", () => {
  it("starts with the page title and accordion list", () => {
    const { container } = render(<RulesScreen />);
    expect(screen.getByRole("heading", { level: 1, name: "Pack rules" })).toBeTruthy();
    expect(container.querySelector(".rules__accordion-list")).toBeTruthy();
    expect(container.firstElementChild?.firstElementChild?.className).toBe("rules__title");
  });

  it("uses a decorative CSS chevron instead of plus/minus", () => {
    const { container } = render(<RulesScreen />);
    const triggers = container.querySelectorAll(".rules__accordion-trigger");
    expect(triggers).toHaveLength(11);
    for (const trigger of triggers) {
      expect(trigger.getAttribute("aria-expanded")).toBe("false");
      expect(trigger.textContent).not.toMatch(/[+\u2212−]/);
      const chevron = trigger.querySelector(".rules__accordion-chevron");
      expect(chevron).toBeTruthy();
      expect(chevron?.getAttribute("aria-hidden")).toBe("true");
    }
  });

  it("opens one section at a time and toggles closed on a second tap", () => {
    render(<RulesScreen />);
    const first = screen.getByRole("button", { name: /1 — Why this app/i });
    const second = screen.getByRole("button", { name: /2 — Principles/i });

    fireEvent.click(first);
    expect(first.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByText(/The pack is where movement finally fits into your life/)).toBeTruthy();

    fireEvent.click(second);
    expect(first.getAttribute("aria-expanded")).toBe("false");
    expect(second.getAttribute("aria-expanded")).toBe("true");
    expect(screen.queryByText(/The pack is where movement finally fits into your life/)).toBeNull();
    expect(screen.getByText(/Any movement counts/)).toBeTruthy();

    fireEvent.click(second);
    expect(second.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByText(/Any movement counts/)).toBeNull();
  });

  it("keeps all 11 sections", () => {
    render(<RulesScreen />);
    expect(screen.getByRole("button", { name: /1 — Why this app/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /2 — Principles/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /3 — Who is Leo/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /4 — What the pack is/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /5 — How you progress/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /6 — How to save a streak/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /7 — What the pack sees/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /8 — If you miss workouts/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /9 — If you.re removed and how to return/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /10 — Sick leave/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /11 — FAQ/i })).toBeTruthy();
  });

  describe("lang=ru", () => {
    beforeEach(() => {
      localStorage.setItem("leo-ui-lang", "ru");
    });

    it("renders Russian title and all 11 section triggers", () => {
      const { container } = render(<RulesScreen />);
      expect(screen.getByRole("heading", { level: 1, name: "Правила Стаи" })).toBeTruthy();
      expect(container.querySelectorAll(".rules__accordion-trigger")).toHaveLength(11);
      expect(screen.getByRole("button", { name: /1 — Зачем это приложение/ })).toBeTruthy();
      expect(screen.getByRole("button", { name: /9 — Если ты удалён и как вернуться/ })).toBeTruthy();
      expect(screen.getByRole("button", { name: /11 — FAQ/ })).toBeTruthy();
      expect(screen.queryByText("Pack rules")).toBeNull();
    });

    it("shows Russian body and Russian level names in the table", () => {
      render(<RulesScreen />);
      fireEvent.click(screen.getByRole("button", { name: /5 — Как ты движешься/ }));
      expect(screen.getByText("Всего кубков")).toBeTruthy();
      expect(screen.getByText(/Сурикат/)).toBeTruthy();
      expect(screen.getByText(/Слон/)).toBeTruthy();
      expect(screen.queryByText(/Meerkat/)).toBeNull();
    });
  });
});

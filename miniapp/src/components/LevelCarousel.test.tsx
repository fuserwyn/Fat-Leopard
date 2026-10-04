// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import { LevelCarousel } from "./LevelCarousel";

afterEach(cleanup);

describe("LevelCarousel", () => {
  it("starts on the current level with emoji and min cups", () => {
    const { container, getByText } = render(<LevelCarousel currentLevel={3} />);
    expect(getByText(/Уровень 3 · Зебра/)).toBeTruthy();
    expect(container.querySelector(".level-carousel__emoji")?.textContent).toBe("🦓");
    expect(container.querySelector(".level-carousel__hint")?.textContent).toMatch(/от 1\s260/);
    expect(container.querySelector(".level-carousel__card.is-current.is-unlocked")).toBeTruthy();
  });

  it("scrolls to locked levels (dimmed) and back to passed ones", () => {
    const { container, getByLabelText, getByText } = render(<LevelCarousel currentLevel={3} />);
    fireEvent.click(getByLabelText("Следующий уровень"));
    expect(getByText(/Уровень 4 · Гепард/)).toBeTruthy();
    expect(container.querySelector(".level-carousel__card.is-locked")).toBeTruthy();

    fireEvent.click(getByLabelText("Предыдущий уровень"));
    fireEvent.click(getByLabelText("Предыдущий уровень"));
    fireEvent.click(getByLabelText("Предыдущий уровень"));
    expect(getByText(/Уровень 1 · Сурикат/)).toBeTruthy();
    expect(container.querySelector(".level-carousel__card.is-unlocked")).toBeTruthy();
    expect(container.querySelector(".level-carousel__hint")?.textContent).toMatch(/старт/);
    expect((getByLabelText("Предыдущий уровень") as HTMLButtonElement).disabled).toBe(true);
  });

  it("disables the next arrow on the last level", () => {
    const { getByLabelText } = render(<LevelCarousel currentLevel={6} />);
    expect((getByLabelText("Следующий уровень") as HTMLButtonElement).disabled).toBe(true);
  });
});

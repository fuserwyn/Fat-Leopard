// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import { LevelCarousel } from "./LevelCarousel";

afterEach(cleanup);

describe("LevelCarousel", () => {
  it("starts on the current level with emoji and min cups", () => {
    const { container, getByText } = render(<LevelCarousel currentLevel={3} />);
    expect(getByText(/Level 3 · Zebra/)).toBeTruthy();
    expect(container.querySelector(".level-carousel__emoji")?.textContent).toBe("🦓");
    expect(container.querySelector(".level-carousel__hint")?.textContent).toMatch(/from 1,?260/);
    expect(container.querySelector(".level-carousel__card.is-current.is-unlocked")).toBeTruthy();
  });

  it("scrolls to locked levels (dimmed) and back to passed ones", () => {
    const { container, getByLabelText, getByText } = render(<LevelCarousel currentLevel={3} />);
    fireEvent.click(getByLabelText("Next level"));
    expect(getByText(/Level 4 · Cheetah/)).toBeTruthy();
    expect(container.querySelector(".level-carousel__card.is-locked")).toBeTruthy();

    fireEvent.click(getByLabelText("Previous level"));
    fireEvent.click(getByLabelText("Previous level"));
    fireEvent.click(getByLabelText("Previous level"));
    expect(getByText(/Level 1 · Meerkat/)).toBeTruthy();
    expect(container.querySelector(".level-carousel__card.is-unlocked")).toBeTruthy();
    expect(container.querySelector(".level-carousel__hint")?.textContent).toMatch(/start/);
    expect((getByLabelText("Previous level") as HTMLButtonElement).disabled).toBe(true);
  });

  it("disables the next arrow on the last level", () => {
    const { getByLabelText } = render(<LevelCarousel currentLevel={6} />);
    expect((getByLabelText("Next level") as HTMLButtonElement).disabled).toBe(true);
  });
});

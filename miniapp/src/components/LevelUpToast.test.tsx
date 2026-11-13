// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, fireEvent, act } from "@testing-library/react";
import { LevelUpToast } from "./LevelUpToast";

afterEach(cleanup);

describe("LevelUpToast", () => {
  it("shows the new-level title, number chip and animal name", () => {
    const { container, getByText } = render(<LevelUpToast level={4} onDone={() => {}} />);
    expect(getByText("New level!")).toBeTruthy();
    // Гепард — 4-й уровень (см. miniappLevel.ts).
    expect(getByText(/Cheetah/)).toBeTruthy();
    expect(container.querySelector(".achievement-toast__level-chip")?.textContent).toBe("4");
  });

  it("dismisses on tap and calls onDone", () => {
    vi.useFakeTimers();
    const onDone = vi.fn();
    const { container } = render(<LevelUpToast level={2} onDone={onDone} />);
    act(() => {
      fireEvent.click(container.querySelector(".achievement-overlay")!);
    });
    act(() => {
      vi.advanceTimersByTime(400);
    });
    expect(onDone).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });
});

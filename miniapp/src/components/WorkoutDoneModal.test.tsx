// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render } from "@testing-library/react";
import { WorkoutDoneModal } from "./WorkoutDoneModal";
import { AchievementToast } from "./AchievementToast";
import { LevelUpToast } from "./LevelUpToast";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("WorkoutDoneModal", () => {
  it("показывает итог и кнопку «Поделиться в сторис»", () => {
    const onShare = vi.fn();
    const onClose = vi.fn();
    const { getByText, container } = render(
      <WorkoutDoneModal message={"Стрик 5 дней\n+12 кубков"} onShare={onShare} onClose={onClose} />,
    );
    expect(getByText("Тренировка засчитана!")).toBeTruthy();
    expect(getByText(/\+12 кубков/)).toBeTruthy();
    fireEvent.click(getByText("Поделиться в сторис"));
    expect(onShare).toHaveBeenCalledTimes(1);
    fireEvent.click(container.querySelector(".workout-done")!);
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(getByText("Готово"));
    fireEvent.keyDown(window, { key: "Escape" });
    fireEvent.keyDown(window, { key: "Enter" });
    fireEvent.click(container.querySelector(".workout-done-overlay")!);
    expect(onClose).toHaveBeenCalledTimes(3);
  });
});

describe("«Поделиться» в празднованиях", () => {
  it("ачивка: кнопка не закрывает тост тапом, а открывает карточку", () => {
    vi.useFakeTimers();
    const onShare = vi.fn();
    const onDone = vi.fn();
    const { getByText } = render(<AchievementToast achievementKey="streak-30" onDone={onDone} onShare={onShare} />);
    fireEvent.click(getByText("Поделиться"));
    expect(onShare).toHaveBeenCalledTimes(1);
    act(() => {
      vi.advanceTimersByTime(6000);
    });
    expect(onDone).not.toHaveBeenCalled(); // с кнопкой тост висит дольше
    act(() => {
      vi.advanceTimersByTime(2500);
    });
    act(() => {
      vi.advanceTimersByTime(400);
    });
    expect(onDone).toHaveBeenCalledTimes(1);
  });

  it("уровень: кнопка есть только с onShare", () => {
    const onShare = vi.fn();
    const { getByText, rerender, queryByText } = render(<LevelUpToast level={3} onDone={() => {}} onShare={onShare} />);
    fireEvent.click(getByText("Поделиться"));
    expect(onShare).toHaveBeenCalledTimes(1);
    rerender(<LevelUpToast level={3} onDone={() => {}} />);
    expect(queryByText("Поделиться")).toBeNull();
  });
});

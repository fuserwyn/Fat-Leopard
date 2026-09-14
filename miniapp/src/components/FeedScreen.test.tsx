// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { FeedScreen } from "./FeedScreen";

class ROStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
class IOStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
(globalThis as unknown as { ResizeObserver: typeof ROStub }).ResizeObserver = ROStub;
(globalThis as unknown as { IntersectionObserver: typeof IOStub }).IntersectionObserver = IOStub;
(globalThis as unknown as { matchMedia: (q: string) => MediaQueryList }).matchMedia = (q: string) =>
  ({
    matches: false,
    media: q,
    addEventListener() {},
    removeEventListener() {},
  }) as unknown as MediaQueryList;

afterEach(cleanup);

const baseProps = {
  streak: 1,
  userId: 1,
  initData: "",
  inTelegram: false,
  showAlert: () => {},
};

describe("FeedScreen tab keep-alive", () => {
  it("does not show Загрузка… when leaving and returning to the tab", async () => {
    const { rerender } = render(<FeedScreen {...baseProps} active refreshToken={0} />);
    await waitFor(() => expect(screen.queryByText("Загрузка…")).toBeNull());

    rerender(<FeedScreen {...baseProps} active={false} refreshToken={0} />);
    rerender(<FeedScreen {...baseProps} active refreshToken={0} />);
    expect(screen.queryByText("Загрузка…")).toBeNull();
  });

  it("does not show Загрузка… when only onOptimisticConsumed identity changes", async () => {
    const { rerender } = render(
      <FeedScreen {...baseProps} active refreshToken={0} onOptimisticConsumed={() => {}} />,
    );
    await waitFor(() => expect(screen.queryByText("Загрузка…")).toBeNull());

    rerender(
      <FeedScreen {...baseProps} active refreshToken={0} onOptimisticConsumed={() => {}} />,
    );
    expect(screen.queryByText("Загрузка…")).toBeNull();
  });
});

describe("FeedScreen pack weekly progress", () => {
  it("shows weekly pack progress bar", async () => {
    render(
      <FeedScreen
        {...baseProps}
        active
        refreshToken={0}
        packWorkoutsWeek={42}
        packWorkoutsGoal={100}
      />,
    );
    await waitFor(() => expect(screen.queryByText("Загрузка…")).toBeNull());
    expect(screen.getByLabelText("Стая: 42/100 тренировок за неделю")).toBeTruthy();
    expect(screen.getByText("42/100")).toBeTruthy();
  });
});

describe("FeedScreen workout type dropdown", () => {
  async function openAllTypesList() {
    render(<FeedScreen {...baseProps} active refreshToken={0} />);
    await waitFor(() => expect(screen.queryByText("Загрузка…")).toBeNull());
    fireEvent.click(screen.getByRole("button", { name: /Все типы/ }));
    expect(screen.getByRole("listbox", { name: "Все типы тренировок" })).toBeTruthy();
  }

  it("closes the list and applies the filter on a second tap of Все типы", async () => {
    await openAllTypesList();
    fireEvent.click(screen.getByRole("option", { name: /Все типы/ }));
    expect(screen.queryByRole("listbox", { name: "Все типы тренировок" })).toBeNull();
  });

  it("closes the list and applies the filter on tap outside", async () => {
    await openAllTypesList();
    fireEvent.click(screen.getByRole("button", { name: "Применить фильтр типов" }));
    expect(screen.queryByRole("listbox", { name: "Все типы тренировок" })).toBeNull();
  });

  it("closes the list on a second tap of the Все типы chip", async () => {
    await openAllTypesList();
    fireEvent.click(screen.getByRole("button", { name: /Все типы/ }));
    expect(screen.queryByRole("listbox", { name: "Все типы тренировок" })).toBeNull();
  });
});

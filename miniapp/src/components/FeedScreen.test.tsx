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
  it("does not show Loading… when leaving and returning to the tab", async () => {
    const { rerender } = render(<FeedScreen {...baseProps} active refreshToken={0} />);
    await waitFor(() => expect(screen.queryByText("Loading…")).toBeNull());

    rerender(<FeedScreen {...baseProps} active={false} refreshToken={0} />);
    rerender(<FeedScreen {...baseProps} active refreshToken={0} />);
    expect(screen.queryByText("Loading…")).toBeNull();
  });

  it("does not show Loading… when only onOptimisticConsumed identity changes", async () => {
    const { rerender } = render(
      <FeedScreen {...baseProps} active refreshToken={0} onOptimisticConsumed={() => {}} />,
    );
    await waitFor(() => expect(screen.queryByText("Loading…")).toBeNull());

    rerender(
      <FeedScreen {...baseProps} active refreshToken={0} onOptimisticConsumed={() => {}} />,
    );
    expect(screen.queryByText("Loading…")).toBeNull();
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
        packWorkoutsGoal={50}
      />,
    );
    await waitFor(() => expect(screen.queryByText("Loading…")).toBeNull());
    expect(screen.getByLabelText("Pack: 42/50 workouts this week")).toBeTruthy();
    expect(screen.getByText("42/50")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Pack weekly goal hint" })).toBeTruthy();
  });

  it("shows weekly goal hint on help button click", async () => {
    let alert = "";
    render(
      <FeedScreen
        {...baseProps}
        active
        refreshToken={0}
        packWorkoutsWeek={42}
        packWorkoutsGoal={50}
        showAlert={(m) => {
          alert = m;
        }}
      />,
    );
    await waitFor(() => expect(screen.queryByText("Loading…")).toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Pack weekly goal hint" }));
    expect(alert).toMatch(/bonus cups/i);
  });
});

describe("FeedScreen workout type dropdown", () => {
  async function openAllTypesList() {
    render(<FeedScreen {...baseProps} active refreshToken={0} />);
    await waitFor(() => expect(screen.queryByText("Loading…")).toBeNull());
    fireEvent.click(screen.getByRole("button", { name: /All types/ }));
    expect(screen.getByRole("listbox", { name: "All workout types" })).toBeTruthy();
  }

  it("closes the list and applies the filter on a second tap of All types", async () => {
    await openAllTypesList();
    fireEvent.click(screen.getByRole("option", { name: /All types/ }));
    expect(screen.queryByRole("listbox", { name: "All workout types" })).toBeNull();
  });

  it("closes the list and applies the filter on tap outside", async () => {
    await openAllTypesList();
    fireEvent.click(screen.getByRole("button", { name: "Apply type filters" }));
    expect(screen.queryByRole("listbox", { name: "All workout types" })).toBeNull();
  });

  it("closes the list on a second tap of the All types chip", async () => {
    await openAllTypesList();
    fireEvent.click(screen.getByRole("button", { name: /All types/ }));
    expect(screen.queryByRole("listbox", { name: "All workout types" })).toBeNull();
  });
});

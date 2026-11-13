// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ActivityCard } from "./ActivityCard";

class ROStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
(globalThis as unknown as { ResizeObserver: typeof ROStub }).ResizeObserver = ROStub;
(globalThis as unknown as { matchMedia: (q: string) => MediaQueryList }).matchMedia = (q: string) =>
  ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} }) as unknown as MediaQueryList;

afterEach(cleanup);

const leoProps = {
  avatar: "🐆",
  name: "Leo",
  timeAgo: "4 h ago",
  emoji: "🌅",
  activity: "Daily wisdom",
  details: "",
  hideStreak: true,
  streak: 0,
};

const wisdom =
  "Balance isn't the absence of movement, but control over it. Today focus on " +
  "technique: do every move, even the simplest, with intent. Discipline beats impulse.";

describe("Leo card in feed", () => {
  it("shows daily wisdom in full without Show more", () => {
    render(<ActivityCard {...leoProps} comment={wisdom} commentAlwaysFull />);
    expect(screen.getByText(wisdom)).toBeTruthy();
    expect(screen.queryByText("Show more")).toBeNull();
  });

  it("other long Leo posts still collapse", () => {
    render(<ActivityCard {...leoProps} activity="Leo" comment={wisdom} />);
    expect(screen.getByText("Show more")).toBeTruthy();
  });

  it("comment under daily wisdom works", () => {
    render(
      <ActivityCard
        {...leoProps}
        comment={wisdom}
        commentAlwaysFull
        threadComposer={{
          draft: "",
          onDraftChange: () => {},
          onSubmit: () => {},
          posting: false,
        }}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /Comments/i }));
    expect(screen.getByPlaceholderText("Write a comment…")).toBeTruthy();
  });
});

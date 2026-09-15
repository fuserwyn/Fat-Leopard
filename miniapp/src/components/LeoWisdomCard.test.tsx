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
  name: "Лео",
  timeAgo: "4 ч назад",
  emoji: "🌅",
  activity: "Мудрость дня",
  details: "",
  hideStreak: true,
  streak: 0,
};

const wisdom =
  "Равновесие — это не отсутствие движения, а контроль над ним. Сегодня фокусируйся " +
  "на технике: каждое действие, даже самое простое, делай осознанно. Дисциплина важнее порыва.";

describe("Карточка Лео в ленте", () => {
  it("мудрость дня видна целиком, без «Показать полностью»", () => {
    render(<ActivityCard {...leoProps} comment={wisdom} commentAlwaysFull />);
    expect(screen.getByText(wisdom)).toBeTruthy();
    expect(screen.queryByText("Показать полностью")).toBeNull();
  });

  it("прочие длинные посты Лео по-прежнему сворачиваются", () => {
    render(<ActivityCard {...leoProps} activity="Лео" comment={wisdom} />);
    expect(screen.getByText("Показать полностью")).toBeTruthy();
  });

  it("под мудростью дня можно оставить комментарий", () => {
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
    fireEvent.click(screen.getByRole("button", { name: /Комментарии/i }));
    expect(screen.getByPlaceholderText("Написать комментарий…")).toBeTruthy();
  });
});

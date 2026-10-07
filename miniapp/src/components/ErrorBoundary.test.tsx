// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ErrorBoundary } from "./ErrorBoundary";

afterEach(cleanup);

let broken = true;
function Flaky() {
  if (broken) throw new Error("boom");
  return <p>лента на месте</p>;
}

describe("ErrorBoundary", () => {
  it("keeps the rest of the app alive and recovers on retry", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    broken = true;
    render(
      <>
        <ErrorBoundary label="Лента">
          <Flaky />
        </ErrorBoundary>
        <p>профиль жив</p>
      </>,
    );
    expect(screen.getByRole("alert").textContent).toContain("Раздел «Лента» не открылся");
    expect(screen.getByText("профиль жив")).toBeTruthy();

    broken = false;
    fireEvent.click(screen.getByRole("button", { name: "Попробовать снова" }));
    expect(screen.getByText("лента на месте")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    spy.mockRestore();
  });

  it("offers a reload when the whole app failed", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    broken = true;
    render(
      <ErrorBoundary>
        <Flaky />
      </ErrorBoundary>,
    );
    expect(screen.getByText("Что-то пошло не так")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Перезагрузить" })).toBeTruthy();
    spy.mockRestore();
  });
});

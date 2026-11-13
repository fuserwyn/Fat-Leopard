// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ErrorBoundary } from "./ErrorBoundary";

afterEach(cleanup);

let broken = true;
function Flaky() {
  if (broken) throw new Error("boom");
  return <p>feed ok</p>;
}

describe("ErrorBoundary", () => {
  it("keeps the rest of the app alive and recovers on retry", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    broken = true;
    render(
      <>
        <ErrorBoundary label="Feed">
          <Flaky />
        </ErrorBoundary>
        <p>profile ok</p>
      </>,
    );
    expect(screen.getByRole("alert").textContent).toContain("Feed");
    expect(screen.getByRole("alert").textContent).toContain("didn't load");
    expect(screen.getByText("profile ok")).toBeTruthy();

    broken = false;
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(screen.getByText("feed ok")).toBeTruthy();
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
    expect(screen.getByText("Something went wrong")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Reload" })).toBeTruthy();
    spy.mockRestore();
  });
});

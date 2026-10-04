// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { CUPS_HISTORY_CAPTION, PACK_WEEKLY_CUPS_LABEL } from "../lib/cupsHistory";
import { CupsHistorySheet } from "./CupsHistorySheet";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("CupsHistorySheet", () => {
  it("shows the last-42 caption, a workout row, a pack bonus and closes on the cross", async () => {
    const onClose = vi.fn();
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        json: async () => ({
          ok: true,
          workouts: [
            {
              date: "2026-09-27",
              message_text: "бег, 40 мин, интенсивность 3/5",
              cups: 29,
              created_at: "2026-09-27T10:00:00Z",
            },
          ],
          pack_weekly: [{ date: "2026-09-22", cups: 50, created_at: "2026-09-22T12:00:00Z" }],
        }),
      })),
    );

    const { getByRole, getByText } = render(
      <CupsHistorySheet apiUrl="https://api.example" initData="init" onClose={onClose} />,
    );

    expect(getByRole("dialog", { name: "История кубков" })).toBeTruthy();
    expect(getByText(CUPS_HISTORY_CAPTION)).toBeTruthy();
    await waitFor(() => {
      expect(getByText("Бег")).toBeTruthy();
    });
    expect(getByText("27.09.2026")).toBeTruthy();
    expect(getByText("3/5")).toBeTruthy();
    expect(getByText("40 мин")).toBeTruthy();
    expect(getByText("+29 кубков")).toBeTruthy();
    expect(getByText(PACK_WEEKLY_CUPS_LABEL)).toBeTruthy();
    expect(getByText("+50 кубков")).toBeTruthy();

    expect(getByRole("button", { name: /Дата/ }).getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(getByRole("button", { name: /Кубки/ }));
    expect(getByRole("button", { name: /Кубки/ }).getAttribute("aria-pressed")).toBe("true");
    const cupsCells = Array.from(document.querySelectorAll(".cups-history__cups")).map((el) => el.textContent);
    expect(cupsCells).toEqual(["+50 кубков", "+29 кубков"]);
    expect(document.querySelector(".cups-history__sep")).toBeNull();

    fireEvent.click(getByRole("button", { name: "Закрыть" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

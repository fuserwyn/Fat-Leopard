// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { InviteFriendsSection } from "./InviteFriendsSection";
import type { ReferralState } from "../lib/referral";

afterEach(() => {
  cleanup();
  delete (window as { Telegram?: unknown }).Telegram;
});

const state: ReferralState = {
  link: "https://t.me/leo_bot?start=ref-42",
  joined: 14,
  qualified: 12,
  rewardEvery: 10,
  rewards: 1,
  nextIn: 8,
};

describe("InviteFriendsSection", () => {
  it("shows counters and progress", () => {
    const { getByText, getAllByText } = render(<InviteFriendsSection initData="x" initialState={state} />);
    expect(getAllByText("Позвать в стаю").length).toBe(2);
    expect(getByText("пришли по ссылке").previousSibling?.textContent).toBe("14");
    expect(getByText("записали тренировку").previousSibling?.textContent).toBe("12");
    expect(getByText("попыток получено").previousSibling?.textContent).toBe("1");
    expect(getByText(/2 из 10 — ещё 8 друзей/)).toBeTruthy();
    expect(getByText(state.link)).toBeTruthy();
  });

  it("renders nothing without a link", () => {
    const { container } = render(<InviteFriendsSection initData="x" initialState={null} />);
    expect(container.innerHTML).toBe("");
  });

  it("opens the Telegram share window", () => {
    const openTelegramLink = vi.fn();
    (window as unknown as { Telegram: unknown }).Telegram = { WebApp: { openTelegramLink } };
    const { getByRole } = render(<InviteFriendsSection initData="x" initialState={state} />);
    fireEvent.click(getByRole("button", { name: "Позвать в стаю" }));
    expect(openTelegramLink).toHaveBeenCalledWith(expect.stringContaining("https://t.me/share/url?url="));
  });

  it("falls back to openLink and copies the link", async () => {
    const openLink = vi.fn();
    (window as unknown as { Telegram: unknown }).Telegram = { WebApp: { openLink } };
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    const { getByRole, getByText } = render(<InviteFriendsSection initData="x" initialState={state} />);
    fireEvent.click(getByRole("button", { name: "Позвать в стаю" }));
    expect(openLink).toHaveBeenCalled();
    delete (window as { Telegram?: unknown }).Telegram;
    fireEvent.click(getByRole("button", { name: "Позвать в стаю" }));
    await waitFor(() => expect(getByText("скопировано")).toBeTruthy());
    expect(writeText).toHaveBeenCalledWith(state.link);
  });

  it("loads the state from the server", async () => {
    vi.stubEnv("VITE_MINIAPP_API_URL", "https://api.test");
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({ ok: true, json: async () => ({ ok: true, referral: { link: state.link, joined: 3 } }) })),
    );
    vi.resetModules();
    const { InviteFriendsSection: Fresh } = await import("./InviteFriendsSection");
    const { findByText } = render(<Fresh initData="x" />);
    expect((await findByText("пришли по ссылке")).previousSibling?.textContent).toBe("3");
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
  });

  it("refreshes the counter when the app becomes visible again", async () => {
    vi.stubEnv("VITE_MINIAPP_API_URL", "https://api.test");
    let joined = 0;
    let fail = false;
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        if (fail) throw new Error("offline");
        return { ok: true, json: async () => ({ ok: true, referral: { link: state.link, joined } }) };
      }),
    );
    vi.resetModules();
    const { InviteFriendsSection: Fresh } = await import("./InviteFriendsSection");
    const { findByText, getByText } = render(<Fresh initData="x" />);
    expect((await findByText("пришли по ссылке")).previousSibling?.textContent).toBe("0");

    joined = 1;
    document.dispatchEvent(new Event("visibilitychange"));
    await waitFor(() => expect(getByText("пришли по ссылке").previousSibling?.textContent).toBe("1"));

    // Сбой сети при обновлении не прячет блок.
    fail = true;
    document.dispatchEvent(new Event("visibilitychange"));
    await new Promise((r) => setTimeout(r, 0));
    expect(getByText("пришли по ссылке").previousSibling?.textContent).toBe("1");
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
  });
});

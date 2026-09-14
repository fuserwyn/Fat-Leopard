// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import {
  applyTheme,
  canUseLeopardTheme,
  canUsePackTheme,
  canUseWildTheme,
  enforceThemeForLevel,
  getStoredTheme,
  hasStoredTheme,
  hydrateThemeFromServer,
  resetThemeRuntimeForTests,
  setTheme,
  themeAllowedForLevel,
} from "./theme";

describe("theme", () => {
  afterEach(() => {
    localStorage.removeItem("leo-theme");
    document.documentElement.removeAttribute("data-theme");
    resetThemeRuntimeForTests();
  });

  it("defaults to dark when nothing stored", () => {
    expect(getStoredTheme()).toBe("dark");
  });

  it("setTheme persists and applyTheme sets data-theme", () => {
    setTheme("light");
    expect(localStorage.getItem("leo-theme")).toBe("light");
    expect(getStoredTheme()).toBe("light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
    applyTheme("dark");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
  });

  it("stores leopard theme", () => {
    document.head.insertAdjacentHTML("beforeend", '<meta name="theme-color" content="#0d0d12" />');
    setTheme("leopard");
    expect(localStorage.getItem("leo-theme")).toBe("leopard");
    expect(getStoredTheme()).toBe("leopard");
    expect(document.documentElement.getAttribute("data-theme")).toBe("leopard");
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute("content")).toBe("#f6d4de");
  });

  it("locks leopard theme below level 5", () => {
    expect(canUseLeopardTheme(4)).toBe(false);
    expect(canUseLeopardTheme(5)).toBe(true);
    expect(themeAllowedForLevel("leopard", 4)).toBe("dark");
    expect(themeAllowedForLevel("leopard", 5)).toBe("leopard");
    expect(themeAllowedForLevel("light", 2)).toBe("light");
    setTheme("leopard");
    expect(enforceThemeForLevel(3)).toBe("dark");
    expect(getStoredTheme()).toBe("dark");
  });

  it("keeps stored theme until a real level is enforced", () => {
    setTheme("leopard");
    expect(getStoredTheme()).toBe("leopard");
    expect(themeAllowedForLevel("leopard", 5)).toBe("leopard");
    expect(themeAllowedForLevel("light", 1)).toBe("light");
  });

  it("applies explicit server theme and ignores empty server theme", () => {
    setTheme("leopard");
    expect(hydrateThemeFromServer("", 5)).toBe("leopard");
    expect(getStoredTheme()).toBe("leopard");
    expect(hasStoredTheme()).toBe(true);
    expect(hydrateThemeFromServer("leopard", 5)).toBe("leopard");
    expect(hydrateThemeFromServer("leopard", 3)).toBe("dark");
  });

  it("unlocks wild theme for admin, 365-day streak, or 1000 workouts", () => {
    expect(canUseWildTheme({})).toBe(false);
    expect(canUseWildTheme({ streakDays: 364 })).toBe(false);
    expect(canUseWildTheme({ streakDays: 365 })).toBe(true);
    expect(canUseWildTheme({ maxStreakDays: 365 })).toBe(true);
    expect(canUseWildTheme({ workoutsTotal: 999 })).toBe(false);
    expect(canUseWildTheme({ workoutsTotal: 1000 })).toBe(true);
    expect(canUseWildTheme({ streakDays: 10, isAdmin: true })).toBe(true);
    expect(themeAllowedForLevel("wild", 1)).toBe("dark");
    expect(themeAllowedForLevel("wild", 1, { streakDays: 365 })).toBe("wild");
    expect(themeAllowedForLevel("wild", 1, { workoutsTotal: 1000 })).toBe("wild");
    expect(themeAllowedForLevel("wild", 1, { isAdmin: true })).toBe("wild");
    expect(themeAllowedForLevel("leopard", 5, { streakDays: 0 })).toBe("leopard");
    setTheme("wild");
    expect(enforceThemeForLevel(6)).toBe("dark");
    expect(getStoredTheme()).toBe("dark");
    setTheme("wild");
    expect(enforceThemeForLevel(2, { maxStreakDays: 400 })).toBe("wild");
    setTheme("wild");
    expect(enforceThemeForLevel(2, { workoutsTotal: 1200 })).toBe("wild");
  });

  it("unlocks pack theme only while pack bonus is active", () => {
    expect(canUsePackTheme({})).toBe(false);
    expect(canUsePackTheme({ packBonusThemeActive: true })).toBe(true);
    expect(themeAllowedForLevel("pack", 1)).toBe("dark");
    expect(themeAllowedForLevel("pack", 1, { packBonusThemeActive: true })).toBe("pack");
  });

  it("stores wild theme and hydrates it only when unlocked", () => {
    document.head.insertAdjacentHTML("beforeend", '<meta name="theme-color" content="#0d0d12" />');
    setTheme("wild");
    expect(localStorage.getItem("leo-theme")).toBe("wild");
    expect(getStoredTheme()).toBe("wild");
    expect(document.documentElement.getAttribute("data-theme")).toBe("wild");
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute("content")).toBe("#0a0a0a");
    expect(hydrateThemeFromServer("wild", 3)).toBe("dark");
    setTheme("wild");
    expect(hydrateThemeFromServer("wild", 3, { isAdmin: true })).toBe("wild");
  });
});

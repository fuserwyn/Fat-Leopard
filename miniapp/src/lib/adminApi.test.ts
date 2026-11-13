import { describe, expect, it } from "vitest";
import { adminErrorLabel, adminSickLeaveActive, formatAdminSickLeave } from "./adminApi";

describe("adminErrorLabel", () => {
  it("maps known codes", () => {
    expect(adminErrorLabel("forbidden")).toBe("No admin rights");
    expect(adminErrorLabel("not_found")).toBe("Not found or already handled");
    expect(adminErrorLabel("empty_text")).toBe("Enter text");
    expect(adminErrorLabel("invalid_price")).toBe("Price must be 1–100000 ₽");
  });

  it("falls back to the raw code", () => {
    expect(adminErrorLabel("weird")).toBe("weird");
    expect(adminErrorLabel()).toBe("");
  });
});

describe("sick leave labels", () => {
  it("formats EN and legacy RU statuses", () => {
    expect(formatAdminSickLeave("active")).toBe("Active");
    expect(formatAdminSickLeave("активен")).toBe("Active");
    expect(formatAdminSickLeave("pending")).toBe("Pending");
    expect(formatAdminSickLeave("ожидает")).toBe("Pending");
    expect(formatAdminSickLeave("none")).toBe("None");
    expect(formatAdminSickLeave("нет")).toBe("None");
  });

  it("detects active sick leave (EN or legacy)", () => {
    expect(adminSickLeaveActive("active")).toBe(true);
    expect(adminSickLeaveActive("ожидает")).toBe(true);
    expect(adminSickLeaveActive("none")).toBe(false);
    expect(adminSickLeaveActive("нет")).toBe(false);
  });
});

import { describe, expect, it } from "vitest";
import { adminErrorLabel, adminSickLeaveActive, formatAdminSickLeave } from "./adminApi";

describe("adminErrorLabel", () => {
  it("maps known codes", () => {
    expect(adminErrorLabel("forbidden")).toBe("Нет прав администратора");
    expect(adminErrorLabel("not_found")).toBe("Не найдено или уже обработано");
    expect(adminErrorLabel("empty_text")).toBe("Напиши текст");
    expect(adminErrorLabel("invalid_price")).toBe("Цена должна быть от 1 до 100000 ₽");
  });

  it("falls back to the raw code", () => {
    expect(adminErrorLabel("weird")).toBe("weird");
    expect(adminErrorLabel()).toBe("");
  });
});

describe("formatAdminSickLeave", () => {
  it("shows sick leave in Russian for both API and legacy values", () => {
    expect(formatAdminSickLeave("active")).toBe("активен");
    expect(formatAdminSickLeave("активен")).toBe("активен");
    expect(formatAdminSickLeave("pending")).toBe("ожидает");
    expect(formatAdminSickLeave("awaiting approval")).toBe("ожидает");
    expect(formatAdminSickLeave("none")).toBe("нет");
    expect(formatAdminSickLeave("No")).toBe("нет");
    expect(formatAdminSickLeave("")).toBe("нет");
    expect(formatAdminSickLeave("странно")).toBe("странно");
  });

  it("detects active or pending leave", () => {
    expect(adminSickLeaveActive("active")).toBe(true);
    expect(adminSickLeaveActive("Pending")).toBe(true);
    expect(adminSickLeaveActive("ожидает")).toBe(true);
    expect(adminSickLeaveActive("none")).toBe(false);
    expect(adminSickLeaveActive("нет")).toBe(false);
  });
});

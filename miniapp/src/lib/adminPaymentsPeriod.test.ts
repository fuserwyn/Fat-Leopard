import { describe, expect, it } from "vitest";
import {
  canShiftPaymentsForward,
  paymentsPeriodRange,
  paymentsPeriodTitle,
  shiftPaymentsAnchor,
  todayMsk,
} from "./adminPaymentsPeriod";

describe("paymentsPeriodRange", () => {
  it("day, week (mon–sun) and month", () => {
    expect(paymentsPeriodRange("day", "2026-10-05")).toEqual({ from: "2026-10-05", to: "2026-10-05" });
    // 2026-10-07 — среда.
    expect(paymentsPeriodRange("week", "2026-10-07")).toEqual({ from: "2026-10-05", to: "2026-10-11" });
    // Воскресенье относится к неделе, начавшейся в понедельник до него.
    expect(paymentsPeriodRange("week", "2026-10-11")).toEqual({ from: "2026-10-05", to: "2026-10-11" });
    expect(paymentsPeriodRange("month", "2026-02-14")).toEqual({ from: "2026-02-01", to: "2026-02-28" });
    expect(paymentsPeriodRange("month", "2028-02-14")).toEqual({ from: "2028-02-01", to: "2028-02-29" });
  });

  it("all time, custom and broken anchor have no bounds", () => {
    expect(paymentsPeriodRange("all", "2026-10-05")).toEqual({ from: "", to: "" });
    expect(paymentsPeriodRange("custom", "2026-10-05")).toEqual({ from: "", to: "" });
    expect(paymentsPeriodRange("day", "05.10.2026")).toEqual({ from: "", to: "" });
  });
});

describe("shiftPaymentsAnchor", () => {
  it("moves by day, week and month", () => {
    expect(shiftPaymentsAnchor("day", "2026-01-01", -1)).toBe("2025-12-31");
    expect(shiftPaymentsAnchor("week", "2026-10-05", 1)).toBe("2026-10-12");
    expect(shiftPaymentsAnchor("month", "2026-03-31", -1)).toBe("2026-02-01");
    expect(shiftPaymentsAnchor("month", "2026-12-15", 1)).toBe("2027-01-01");
  });
});

describe("paymentsPeriodTitle", () => {
  it("formats each mode", () => {
    expect(paymentsPeriodTitle("day", "2026-10-05")).toBe("05.10.2026");
    expect(paymentsPeriodTitle("week", "2026-10-01")).toBe("28.09–04.10.2026");
    expect(paymentsPeriodTitle("month", "2026-10-05")).toBe("Октябрь 2026");
    expect(paymentsPeriodTitle("all", "2026-10-05")).toBe("");
  });
});

describe("canShiftPaymentsForward", () => {
  it("stops at the current period", () => {
    expect(canShiftPaymentsForward("day", "2026-10-05", "2026-10-05")).toBe(false);
    expect(canShiftPaymentsForward("day", "2026-10-04", "2026-10-05")).toBe(true);
    expect(canShiftPaymentsForward("month", "2026-10-01", "2026-10-05")).toBe(false);
    expect(canShiftPaymentsForward("month", "2026-09-01", "2026-10-05")).toBe(true);
  });
});

describe("todayMsk", () => {
  it("uses Moscow time", () => {
    expect(todayMsk(new Date("2026-10-05T22:30:00Z"))).toBe("2026-10-06");
    expect(todayMsk(new Date("2026-10-05T20:30:00Z"))).toBe("2026-10-05");
  });
});

import { describe, expect, it } from "vitest";
import { dashBarHeights, dashBucketLabel } from "./adminDashboard";

describe("dashBucketLabel", () => {
  it("formats a day and a week start", () => {
    expect(dashBucketLabel("2026-10-05", false)).toBe("5 окт");
    expect(dashBucketLabel("2026-10-05", true)).toBe("с 5 окт");
  });

  it("returns unknown input as is", () => {
    expect(dashBucketLabel("вчера", false)).toBe("вчера");
    expect(dashBucketLabel("2026-13-01", false)).toBe("2026-13-01");
  });
});

describe("dashBarHeights", () => {
  it("scales to the maximum and keeps small values visible", () => {
    expect(dashBarHeights([0, 1, 50, 100])).toEqual([0, 4, 50, 100]);
  });

  it("returns zeros for empty data", () => {
    expect(dashBarHeights([0, 0])).toEqual([0, 0]);
    expect(dashBarHeights([])).toEqual([]);
    expect(dashBarHeights([Number.NaN, -3])).toEqual([0, 0]);
  });
});

import { describe, expect, it } from "vitest";
import { formatInactivityRemovalHint } from "./formatInactivityRemoval";

describe("formatInactivityRemovalHint", () => {
  it("null/empty/invalid → null", () => {
    expect(formatInactivityRemovalHint(null)).toBeNull();
    expect(formatInactivityRemovalHint("")).toBeNull();
    expect(formatInactivityRemovalHint("   ")).toBeNull();
    expect(formatInactivityRemovalHint("not-iso")).toBeNull();
  });

  it("formats Moscow local for RFC3339", () => {
    const hint = formatInactivityRemovalHint("2026-07-08T00:00:00+03:00");
    expect(hint).toBeTruthy();
    expect(hint!).toMatch(/8/);
    expect(hint!).toMatch(/12:00 AM|00:00|0:00/);
  });
});

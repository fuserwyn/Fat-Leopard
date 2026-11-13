import { describe, expect, it } from "vitest";
import { isModerationError, moderationUserMessage } from "./moderationMessages";

describe("moderationUserMessage", () => {
  it("maps known codes", () => {
    expect(moderationUserMessage("moderation_profanity")).toMatch(/disallowed words/i);
    expect(moderationUserMessage("moderation_link")).toMatch(/links/i);
    expect(moderationUserMessage("user_muted")).toMatch(/limited/i);
    expect(moderationUserMessage("leo_daily_limited")).toMatch(/20 messages/i);
  });

  it("fallback / default", () => {
    expect(moderationUserMessage("unknown", " Custom text ")).toBe("Custom text");
    expect(moderationUserMessage(undefined)).toMatch(/didn.t pass review/i);
  });
});

describe("isModerationError", () => {
  it("detects moderation family", () => {
    expect(isModerationError("moderation_blocked")).toBe(true);
    expect(isModerationError("user_muted")).toBe(true);
    expect(isModerationError("leo_daily_limited")).toBe(true);
    expect(isModerationError("save_failed")).toBe(false);
    expect(isModerationError(undefined)).toBe(false);
  });
});

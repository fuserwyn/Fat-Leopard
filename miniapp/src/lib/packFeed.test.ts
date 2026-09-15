import { describe, expect, it } from "vitest";
import {
  buildFeedPostTextForSave,
  extractEditableFeedPostText,
  feedItemKey,
  feedPostEditable,
  feedSupportsThread,
  isFeedMessage,
  packMessageCommentReplyToId,
  mapFeedThreadQuote,
  mergePackFeedReactions,
  resolveVoterPhotoUrl,
  votersToLikers,
  optimisticTogglePackFeedReaction,
  optimisticToggleThreadReplyLike,
  reconcilePinnedFeed,
  sortPackFeedItemsDesc,
  type PackFeedItemDTO,
  type PackFeedThreadReplyDTO,
} from "./packFeed";

function item(partial: Partial<PackFeedItemDTO> & Pick<PackFeedItemDTO, "id">): PackFeedItemDTO {
  return {
    type: "training_done",
    created_at: "2026-07-01T12:00:00Z",
    text: "x",
    user_id: 1,
    username: "u",
    streak_days: 0,
    is_you: false,
    ...partial,
  };
}

describe("feedPostEditable", () => {
  it("only own training/healthy/pack_message", () => {
    expect(feedPostEditable("training_done", true)).toBe(true);
    expect(feedPostEditable("healthy", true)).toBe(true);
    expect(feedPostEditable("pack_message", true)).toBe(true);
    expect(feedPostEditable("training_done", false)).toBe(false);
    expect(feedPostEditable("sick_leave", true)).toBe(false);
  });
});

describe("feedSupportsThread", () => {
  it("includes daily_wisdom and other social cards", () => {
    expect(feedSupportsThread("daily_wisdom")).toBe(true);
    expect(feedSupportsThread("training_done")).toBe(true);
    expect(feedSupportsThread("admin_post")).toBe(true);
    expect(feedSupportsThread("pack_join")).toBe(false);
    expect(feedSupportsThread("pack_message")).toBe(false);
  });
});

describe("extractEditableFeedPostText / buildFeedPostTextForSave", () => {
  it("strips #healthy for edit and restores prefix on save", () => {
    const raw = "#healthy\nУра, выздоровел!";
    expect(extractEditableFeedPostText(raw, "healthy")).toBe("Ура, выздоровел!");
    expect(buildFeedPostTextForSave(raw, "healthy", "Снова в строю")).toBe("#healthy\nСнова в строю");
  });

  it("keeps training header kind when editing body", () => {
    const raw = "#training_done — танцы, 90 мин, инт. 2/5\nТанцы!";
    expect(extractEditableFeedPostText(raw, "training_done")).toContain("Танцы!");
    const saved = buildFeedPostTextForSave(raw, "training_done", "Новый комментарий");
    expect(saved).toMatch(/^#training_done/);
    expect(saved).toContain("танцы,");
    expect(saved).toContain("Новый комментарий");
  });

  it("pack_message is plain text", () => {
    expect(extractEditableFeedPostText("привет", "pack_message")).toBe("привет");
    expect(buildFeedPostTextForSave("привет", "pack_message", "пока")).toBe("пока");
  });
});

describe("feed keys / message detection", () => {
  it("feedItemKey namespaces by source", () => {
    expect(feedItemKey({ id: 5 })).toBe("feed:5");
    expect(feedItemKey({ id: 5, source: "message" })).toBe("message:5");
  });

  it("isFeedMessage", () => {
    expect(isFeedMessage({ type: "pack_message" })).toBe(true);
    expect(isFeedMessage({ type: "training_done", source: "message" })).toBe(true);
    expect(isFeedMessage({ type: "training_done", source: "feed" })).toBe(false);
  });

  it("packMessageCommentReplyToId targets a comment or the card", () => {
    expect(packMessageCommentReplyToId(10)).toBe(10);
    expect(packMessageCommentReplyToId(10, 0)).toBe(10);
    expect(packMessageCommentReplyToId(10, 77)).toBe(77);
  });

  it("mapFeedThreadQuote keeps a reply to Leo even without text", () => {
    expect(
      mapFeedThreadQuote({
        user_id: 3,
        reply_to_id: 77,
        reply_to_is_leo: true,
        reply_to_text: "",
        reply_to_username: "",
      }),
    ).toEqual({ author: "Лео", text: "", isLeo: true, isAdmin: false });
  });

  it("mapFeedThreadQuote keeps a reply to a participant", () => {
    expect(
      mapFeedThreadQuote({
        user_id: 3,
        reply_to_id: 12,
        reply_to_username: "Аня",
        reply_to_text: "огонь",
      }),
    ).toEqual({ author: "Аня", text: "огонь", isLeo: false, isAdmin: false });
  });

  it("mapFeedThreadQuote hides a bare reply_to without author or text", () => {
    expect(mapFeedThreadQuote({ user_id: 3, reply_to_id: 12 })).toBeUndefined();
  });
});

describe("reactions", () => {
  it("resolveVoterPhotoUrl uses miniapp static for Leo even when backend sends API-relative path", () => {
    expect(resolveVoterPhotoUrl({ name: "Лео", photo_url: "/leo-avatar.png" })).toMatch(/leo-avatar\.png$/);
    expect(resolveVoterPhotoUrl({ name: "Лео", photo_url: "https://app.example/leo-avatar.png" })).toMatch(
      /leo-avatar\.png$/,
    );
    expect(resolveVoterPhotoUrl({ name: "Лео" })).toMatch(/leo-avatar\.png$/);
    expect(resolveVoterPhotoUrl({ name: "Аня", photo_url: "/api/miniapp/user-avatar?user_id=1" })).toContain(
      "/api/miniapp/user-avatar",
    );
  });

  it("votersToLikers maps Leo string voter to avatar", () => {
    expect(votersToLikers(["Лео"])).toEqual([{ name: "Лео", photoUrl: expect.stringMatching(/leo-avatar\.png$/) }]);
  });

  it("mergePackFeedReactions puts mine first", () => {
    const merged = mergePackFeedReactions(["💪", "❤️", "🔥"], [
      { emoji: "🔥", count: 2, me: true },
      { emoji: "💪", count: 1, me: false },
    ]);
    expect(merged[0]?.emoji).toBe("🔥");
    expect(merged[0]?.me).toBe(true);
    expect(merged.find((r) => r.emoji === "❤️")?.count).toBe(0);
  });

  it("optimisticTogglePackFeedReaction toggles same emoji off", () => {
    const after = optimisticTogglePackFeedReaction([{ emoji: "❤️", count: 1, me: true }], "❤️");
    expect(after).toEqual([]);
  });

  it("optimisticTogglePackFeedReaction switches emoji", () => {
    const after = optimisticTogglePackFeedReaction([{ emoji: "❤️", count: 2, me: true }], "🔥");
    expect(after.find((r) => r.emoji === "❤️")).toEqual({ emoji: "❤️", count: 1, me: false, voters: undefined });
    expect(after.find((r) => r.emoji === "🔥")).toEqual({ emoji: "🔥", count: 1, me: true });
  });

  it("optimisticToggleThreadReplyLike", () => {
    const thread: PackFeedThreadReplyDTO[] = [
      {
        id: 1,
        user_id: 1,
        username: "a",
        text: "a",
        created_at: "2026-07-01T00:00:00Z",
        is_you: false,
        like_count: 0,
        like_me: false,
      },
      {
        id: 2,
        user_id: 2,
        username: "b",
        text: "b",
        created_at: "2026-07-01T00:00:00Z",
        is_you: true,
        like_count: 3,
        like_me: true,
      },
    ];
    const next = optimisticToggleThreadReplyLike(thread, 2);
    expect(next[1]?.like_me).toBe(false);
    expect(next[1]?.like_count).toBe(2);
    expect(next[0]?.like_count).toBe(0);
  });
});

describe("pinned / sort", () => {
  it("reconcilePinnedFeed pins and unpins", () => {
    const prev = [
      item({ id: 1, is_pinned: true, pinned_at: "2026-07-01T10:00:00Z" }),
      item({ id: 2, is_pinned: false }),
    ];
    const pinned = [item({ id: 2, is_pinned: true, pinned_at: "2026-07-02T10:00:00Z" })];
    const next = reconcilePinnedFeed(prev, pinned);
    expect(next.find((x) => x.id === 1)?.is_pinned).toBe(false);
    expect(next.find((x) => x.id === 2)?.is_pinned).toBe(true);
  });

  it("sortPackFeedItemsDesc: newer created_at first", () => {
    const items = [
      item({ id: 1, created_at: "2026-07-03T00:00:00Z" }),
      item({ id: 2, created_at: "2026-07-01T00:00:00Z" }),
      item({ id: 3, created_at: "2026-07-04T00:00:00Z" }),
    ];
    const sorted = sortPackFeedItemsDesc(items);
    expect(sorted.map((x) => x.id)).toEqual([3, 1, 2]);
  });
});

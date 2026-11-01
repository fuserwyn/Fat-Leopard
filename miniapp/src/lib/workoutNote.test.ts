import { afterEach, describe, expect, it, vi } from "vitest";
import { contractProblems, loadBackendRoutes } from "./backendContract";
import { importWithApi, mockFetch } from "./testApi";
import { WORKOUT_NOTE_PLACEHOLDER, activeMentionQuery, applyMention, parseMentionCandidates } from "./workoutNote";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("WORKOUT_NOTE_PLACEHOLDER", () => {
  it("подсказывает отметить напарника на совместной тренировке через @ник", () => {
    expect(WORKOUT_NOTE_PLACEHOLDER).toContain("@ник");
    expect(WORKOUT_NOTE_PLACEHOLDER).toMatch(/вместе/);
  });
});

describe("activeMentionQuery", () => {
  it("находит набираемую отметку у курсора", () => {
    expect(activeMentionQuery("@", 1)).toEqual({ start: 0, query: "" });
    expect(activeMentionQuery("бег с @вас", 10)).toEqual({ start: 6, query: "вас" });
    expect(activeMentionQuery("бег (@vas", 9)).toEqual({ start: 5, query: "vas" });
    expect(activeMentionQuery("бег с @вася и", 11)).toEqual({ start: 6, query: "вася" });
  });

  it("не срабатывает вне отметки", () => {
    expect(activeMentionQuery("", 0)).toBeNull();
    expect(activeMentionQuery("бег с @вася и", 13)).toBeNull();
    expect(activeMentionQuery("mail@vasya", 10)).toBeNull();
    expect(activeMentionQuery("без отметки", 5)).toBeNull();
    expect(activeMentionQuery("@" + "я".repeat(45), 46)).toBeNull();
  });
});

describe("applyMention", () => {
  it("заменяет набранное на ник и ставит курсор после пробела", () => {
    expect(applyMention("бег с @ва", 6, 9, "vasya")).toEqual({ text: "бег с @vasya ", caret: 13 });
    expect(applyMention("@ва и Петя", 0, 3, "vasya")).toEqual({ text: "@vasya и Петя", caret: 7 });
    expect(applyMention("@васил и", 0, 3, "Аня")).toEqual({ text: "@Аня и", caret: 5 });
  });

  it("не превышает лимит длины", () => {
    const r = applyMention("ab @v", 3, 5, "vasya", 8);
    expect(r.text).toBe("ab @vasy");
    expect(r.caret).toBe(8);
  });
});

describe("parseMentionCandidates", () => {
  it("берёт только участников с отметкой", () => {
    expect(
      parseMentionCandidates([
        { user_id: 1, name: "Вася", mention: "vasya" },
        { user_id: 2, name: " ", mention: "Аня" },
        { user_id: 3, name: "Без ника", mention: "" },
        { mention: "nouser" },
        null,
        "x",
      ]),
    ).toEqual([
      { userId: 1, name: "Вася", mention: "vasya" },
      { userId: 2, name: "Аня", mention: "Аня" },
    ]);
    expect(parseMentionCandidates({})).toEqual([]);
  });
});

describe("searchMentionCandidates", () => {
  const backend = loadBackendRoutes();
  const load = () => importWithApi(() => import("./workoutNote"));

  it("ищет участников стаи по набранному", async () => {
    const api = await load();
    const calls = mockFetch(() => ({ json: { ok: true, members: [{ user_id: 7, name: "Вася", mention: "vasya" }] } }));
    expect(await api.searchMentionCandidates("i", "va")).toEqual([{ userId: 7, name: "Вася", mention: "vasya" }]);
    expect(calls[0].path).toBe("/api/miniapp/friends/mention-search");
    expect(calls[0].body).toEqual({ init_data: "i", query: "va" });
    if (backend) expect(contractProblems(backend, calls[0])).toEqual([]);

    mockFetch(() => ({ status: 403, json: { error: "forbidden" } }));
    expect(await api.searchMentionCandidates("i", "va")).toEqual([]);
    mockFetch(() => ({ fail: true }));
    expect(await api.searchMentionCandidates("i", "va")).toEqual([]);
    expect(await api.searchMentionCandidates(" ", "va")).toEqual([]);
  });

  it("без адреса API ничего не запрашивает", async () => {
    const api = await importWithApi(() => import("./workoutNote"), "");
    const calls = mockFetch(() => ({ json: { ok: true } }));
    expect(await api.searchMentionCandidates("i", "va")).toEqual([]);
    expect(calls).toHaveLength(0);
  });
});

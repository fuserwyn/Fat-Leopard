// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { importWithApi, mockFetch } from "./testApi";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

function textarea(value: string): HTMLTextAreaElement {
  const ta = document.createElement("textarea");
  document.body.appendChild(ta);
  ta.value = value;
  ta.setSelectionRange(value.length, value.length);
  return ta;
}

describe("useWorkoutMention", () => {
  it("подсказывает участников стаи у «@» и вставляет выбранного", async () => {
    const { useWorkoutMention } = await importWithApi(() => import("./useWorkoutMention"));
    const calls = mockFetch(() => ({ json: { ok: true, members: [{ user_id: 7, name: "Вася", mention: "vasya_run" }] } }));
    const ta = textarea("бег с @va");
    const ref = { current: ta };
    const setNote = vi.fn();
    const { result } = renderHook(() => useWorkoutMention(ref, ta.value, setNote, "init", 1500));

    act(() => result.current.track(ta));
    await waitFor(() => expect(result.current.hits).toHaveLength(1));
    expect(calls[calls.length - 1].body).toEqual({ init_data: "init", query: "va" });

    act(() => result.current.pick(result.current.hits[0]));
    expect(setNote).toHaveBeenCalledWith("бег с @vasya_run ");
    expect(result.current.hits).toEqual([]);
  });

  it("закрывается вне отметки и без initData ничего не запрашивает", async () => {
    const { useWorkoutMention } = await importWithApi(() => import("./useWorkoutMention"));
    const calls = mockFetch(() => ({ json: { ok: true, members: [{ user_id: 7, name: "Вася", mention: "vasya" }] } }));
    const ta = textarea("@va");
    const ref = { current: ta };
    const { result } = renderHook(() => useWorkoutMention(ref, ta.value, vi.fn(), undefined, 1500));
    act(() => result.current.track(ta));
    await new Promise((r) => setTimeout(r, 250));
    expect(calls).toHaveLength(0);
    expect(result.current.hits).toEqual([]);

    const withInit = renderHook(() => useWorkoutMention(ref, ta.value, vi.fn(), "init", 1500));
    act(() => withInit.result.current.track(ta));
    await waitFor(() => expect(withInit.result.current.hits).toHaveLength(1));
    act(() => withInit.result.current.close());
    expect(withInit.result.current.hits).toEqual([]);
    // pick без активной отметки ничего не делает.
    act(() => withInit.result.current.pick({ userId: 7, name: "Вася", mention: "vasya" }));
  });
});

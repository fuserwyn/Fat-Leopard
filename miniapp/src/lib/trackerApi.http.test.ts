import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { contractProblems, loadBackendRoutes } from "./backendContract";
import { importWithApi, mockFetch } from "./testApi";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

const load = () => importWithApi(() => import("./trackerApi"));
type Api = Awaited<ReturnType<typeof load>>;

/** Операции доски, которые знает сервер (switch в trackerRequest). */
function serverOps(): Set<string> | null {
  const file = join(__dirname, "../../../ms_leo/internal/bot/miniapp_tracker.go");
  if (!existsSync(file)) return null;
  const src = readFileSync(file, "utf8");
  const body = src.slice(src.indexOf("func (b *Bot) trackerRequest("));
  const ops = new Set<string>();
  for (const line of body.slice(0, body.indexOf("\n}\n")).matchAll(/case ((?:"[a-z_]+"(?:, )?)+):/g)) {
    for (const op of line[1].matchAll(/"([a-z_]+)"/g)) ops.add(op[1]);
  }
  return ops;
}

/** Каждая операция доски: вызов клиента → что уходит на сервер. */
const CALLS: [name: string, run: (a: Api) => Promise<unknown>, sent: Record<string, unknown>][] = [
  ["list", (a) => a.trackerList("i"), { op: "list" }],
  ["refresh", (a) => a.trackerRefresh("i"), { op: "refresh" }],
  [
    "create",
    (a) => a.trackerCreate("i", { when: "сейчас", prompt: "Починить кнопку", needs_approval: true }),
    { op: "create", payload: { auto_push: true, when: "сейчас", prompt: "Починить кнопку", needs_approval: true } },
  ],
  ["create without push", (a) => a.trackerCreate("i", { when: "в 21:30", prompt: "x", auto_push: false }), { op: "create", payload: { auto_push: false, when: "в 21:30", prompt: "x" } }],
  ["task", (a) => a.trackerTask("i", 7), { op: "task", task_id: 7 }],
  ["cancel", (a) => a.trackerCancel("i", 7), { op: "cancel", payload: { id: 7 } }],
  ["delete", (a) => a.trackerDelete("i", 7), { op: "delete", task_id: 7 }],
  ["approve", (a) => a.trackerApprove("i", 7, "approve"), { op: "approve", payload: { id: 7, action: "approve" } }],
  ["reject with comment", (a) => a.trackerApprove("i", 7, "reject", "дорого"), { op: "approve", payload: { id: 7, action: "reject", comment: "дорого" } }],
  ["qa", (a) => a.trackerQa("i", 7, "pass"), { op: "qa", payload: { id: 7, action: "pass" } }],
  ["prompt", (a) => a.trackerPrompt("i", 7, "Новый текст"), { op: "prompt", task_id: 7, payload: { id: 7, prompt: "Новый текст" } }],
  ["reschedule", (a) => a.trackerReschedule("i", 7, "через 10 минут"), { op: "reschedule", payload: { id: 7, when: "через 10 минут" } }],
  ["move", (a) => a.trackerMove("i", 7, "next"), { op: "move", task_id: 7, payload: { id: 7, column: "next" } }],
  ["restart", (a) => a.trackerRestart("i", 7), { op: "restart", task_id: 7, payload: { id: 7 } }],
  ["run now", (a) => a.trackerRunNow("i", 7), { op: "restart", task_id: 7, payload: { id: 7 } }],
  ["auto qa", (a) => a.trackerAutoQa("i", 7), { op: "auto_qa", payload: { id: 7 } }],
  ["review", (a) => a.trackerReview("i", 7), { op: "review", payload: { id: 7 } }],
  ["auto test", (a) => a.trackerAutoTest("i", 7), { op: "auto_test", payload: { id: 7 } }],
  ["promote", (a) => a.trackerPromote("i", 7), { op: "promote", payload: { id: 7 } }],
  ["revert", (a) => a.trackerRevert("i", 7), { op: "revert", payload: { id: 7 } }],
  ["ship", (a) => a.trackerShip("i", 7), { op: "ship", task_id: 7, payload: { id: 7 } }],
  ["deploy", (a) => a.trackerDeployNow("i", 7), { op: "deploy", task_id: 7, payload: { id: 7 } }],
  ["deploy settings", (a) => a.trackerDeploySettings("i", "on"), { op: "deploy_settings", payload: { action: "on" } }],
  ["deploy status", (a) => a.trackerDeploySettings("i"), { op: "deploy_settings", payload: { action: "status" } }],
];

describe("tracker board client", () => {
  const backend = loadBackendRoutes();
  const ops = serverOps();

  it.each(CALLS)("%s", async (_name, run, sent) => {
    const api = await load();
    const calls = mockFetch(() => ({ json: { ok: true, data: {} } }));
    await run(api);
    expect(calls).toHaveLength(1);
    expect(calls[0].path).toBe("/api/miniapp/admin/tracker");
    expect(JSON.parse(JSON.stringify(calls[0].body))).toEqual({ init_data: "i", ...sent });
    if (backend) expect(contractProblems(backend, calls[0])).toEqual([]);
    // Операция должна быть известна серверу, иначе доска ответит «недоступно».
    if (ops) expect([...ops]).toContain(sent.op);
  });

  it("unwraps the board answer whether it comes as an object or a JSON string", async () => {
    const api = await load();
    mockFetch(() => ({ json: { ok: true, data: { tasks: [{ id: 1 }], repo: "o/r" } } }));
    expect(await api.trackerList("i")).toEqual({ tasks: [{ id: 1 }], repo: "o/r" });
    mockFetch(() => ({ json: { ok: true, data: JSON.stringify({ tasks: [], repo: null }) } }));
    expect(await api.trackerList("i")).toEqual({ tasks: [], repo: null });
    mockFetch(() => ({ json: { ok: true, data: "не json" } }));
    expect(await api.trackerList("i")).toEqual({});
    mockFetch(() => ({ json: { ok: true } }));
    expect(await api.trackerList("i")).toEqual({});
  });

  it("explains board errors", async () => {
    const api = await load();
    mockFetch(() => ({ status: 403, json: { error: "forbidden" } }));
    await expect(api.trackerList("i")).rejects.toThrow("Нет прав администратора");
    mockFetch(() => ({ status: 503, json: { error: "tracker_not_configured" } }));
    await expect(api.trackerList("i")).rejects.toThrow("Доска недоступна");
    mockFetch(() => ({ status: 400, json: { error: "admin_error", message: "нужно 2 аппрува, сейчас 1" } }));
    await expect(api.trackerMove("i", 7, "doing")).rejects.toThrow("нужно 2 аппрува, сейчас 1");
    mockFetch(() => ({ status: 400, json: { error: "invalid_action" } }));
    await expect(api.trackerMove("i", 7, "куда-то")).rejects.toThrow("Такое действие доске недоступно");
    mockFetch(() => ({ json: { ok: false, error: "admin_error" } }));
    await expect(api.trackerCreate("i", { when: "сейчас", prompt: "" })).rejects.toThrow("Доска не приняла задачу");
    mockFetch(() => ({ status: 500 }));
    await expect(api.trackerList("i")).rejects.toThrow("Ошибка 500");
    const noApi = await importWithApi(() => import("./trackerApi"), "");
    await expect(noApi.trackerList("i")).rejects.toThrow("API не настроен");
  });

  it("builds avatar links and loads author names", async () => {
    const api = await load();
    expect(api.trackerAvatarUrl("a=1&b=2", 42)).toBe("https://api.test/api/miniapp/user-avatar?init_data=a%3D1%26b%3D2&user_id=42");
    expect(api.trackerAvatarUrl("i", 0)).toBe("");

    const calls = mockFetch(() => ({ json: { ok: true, people: [{ user_id: 42, username: "anna", display_name: "Анна" }] } }));
    expect(await api.trackerAuthors("i", [42])).toEqual([{ user_id: 42, username: "anna", display_name: "Анна" }]);
    expect(calls[0]).toMatchObject({ path: "/api/miniapp/admin/tracker/authors", body: { init_data: "i", ids: [42] } });
    if (backend) expect(contractProblems(backend, calls[0])).toEqual([]);
    // Пустой список и ошибка сервера доску не роняют.
    expect(await api.trackerAuthors("i", [])).toEqual([]);
    expect(calls).toHaveLength(1);
    mockFetch(() => ({ status: 500, json: {} }));
    expect(await api.trackerAuthors("i", [42])).toEqual([]);
  });
});

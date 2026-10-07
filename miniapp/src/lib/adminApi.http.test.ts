import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { contractProblems, loadBackendRoutes } from "./backendContract";
import { importWithApi, mockFetch, type Sent } from "./testApi";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

const load = () => importWithApi(() => import("./adminApi"));
type Api = Awaited<ReturnType<typeof load>>;

const photo = new File([new Uint8Array([1])], "shot.png", { type: "image/png" });

/** Каждый вызов клиента админки: что вызываем, куда и с чем он должен уйти. */
const CALLS: [name: string, run: (a: Api) => Promise<unknown>, path: string, body: Record<string, unknown>][] = [
  ["overview", (a) => a.fetchAdminOverview("i"), "/admin/overview", {}],
  ["support inbox", (a) => a.fetchAdminSupportInbox("i"), "/admin/support/inbox", {}],
  ["support thread", (a) => a.fetchAdminSupportThread("i", 5), "/admin/support/thread", { target_user_id: 5 }],
  ["support reply", (a) => a.sendAdminSupportReply("i", 5, "ответ"), "/admin/support/reply", { target_user_id: 5, text: "ответ" }],
  ["reports", (a) => a.fetchAdminReports("i"), "/admin/reports", {}],
  ["report action", (a) => a.sendAdminReportAction("i", 3, "hide"), "/admin/reports/action", { report_id: 3, action: "hide" }],
  ["hidden", (a) => a.fetchAdminHidden("i"), "/admin/hidden", {}],
  ["restore hidden", (a) => a.restoreAdminHidden("i", "thread_reply", 9), "/admin/hidden/restore", { kind: "thread_reply", id: 9 }],
  ["users", (a) => a.fetchAdminUsers("i", "anna", 20, "kicked"), "/admin/users", { query: "anna", offset: 20, filter: "kicked" }],
  ["users defaults", (a) => a.fetchAdminUsers("i"), "/admin/users", { query: "", offset: 0, filter: "all" }],
  ["user card", (a) => a.fetchAdminUserCard("i", 5), "/admin/users/card", { target_user_id: 5 }],
  ["user stat", (a) => a.setAdminUserStat("i", 5, "cups", "add", -10), "/admin/users/stat", { target_user_id: 5, field: "cups", mode: "add", value: -10 }],
  ["user action", (a) => a.sendAdminUserAction("i", 5, "kick"), "/admin/users/action", { target_user_id: 5, action: "kick" }],
  ["publish", (a) => a.publishAdminPost("i", "Объявление", "leo"), "/admin/publish", { text: "Объявление", author: "leo" }],
  ["price", (a) => a.fetchAdminPaywallPrice("i"), "/admin/paywall-price", {}],
  ["price set", (a) => a.saveAdminPaywallPrice("i", 499), "/admin/paywall-price/set", { amount_rub: 499 }],
  ["price reset", (a) => a.resetAdminPaywallPrice("i"), "/admin/paywall-price/set", { reset: true }],
  ["pack goal", (a) => a.fetchAdminPackGoal("i"), "/admin/pack-goal", {}],
  ["pack goal set", (a) => a.saveAdminPackGoal("i", 120), "/admin/pack-goal/set", { goal: 120 }],
  ["pack goal reset", (a) => a.resetAdminPackGoal("i"), "/admin/pack-goal/set", { reset: true }],
  ["analytics", (a) => a.fetchAdminAnalytics("i", 30), "/admin/analytics", { days: 30, dashboard: false }],
  ["dashboards", (a) => a.fetchAdminAnalytics("i", 0, true), "/admin/analytics", { days: 0, dashboard: true }],
  ["visits", (a) => a.fetchAdminVisits("i"), "/admin/visits", {}],
  ["payments defaults", (a) => a.fetchAdminPayments("i"), "/admin/payments", { offset: 0, limit: 20, kind: "", completed_only: false, query: "", from: "", to: "", order: "desc" }],
  [
    "payments filtered",
    (a) => a.fetchAdminPayments("i", 40, 10, { kind: "donation", completedOnly: true, query: "  мари ", from: "2026-10-01", to: "2026-10-07", order: "asc" }),
    "/admin/payments",
    { offset: 40, limit: 10, kind: "donation", completed_only: true, query: "мари", from: "2026-10-01", to: "2026-10-07", order: "asc" },
  ],
  ["admins", (a) => a.fetchAdminAdmins("i"), "/admin/admins", {}],
  ["admin add", (a) => a.addAdminPerson("i", "@boris"), "/admin/admins/add", { query: "@boris" }],
  ["admin remove", (a) => a.removeAdminPerson("i", 7), "/admin/admins/remove", { user_id: 7 }],
  ["scheduled", (a) => a.fetchAdminScheduledPosts("i"), "/admin/scheduled", {}],
  ["scheduled add", (a) => a.addAdminScheduledPost("i", "leo", "Позже", "2026-10-09T09:00"), "/admin/scheduled/add", { author: "leo", text: "Позже", at: "2026-10-09T09:00" }],
  ["scheduled cancel", (a) => a.cancelAdminScheduledPost("i", 4), "/admin/scheduled/cancel", { id: 4 }],
  ["poll", (a) => a.publishAdminPoll("i", "Бег или йога?", ["Бег", "Йога"]), "/admin/poll", { question: "Бег или йога?", options: ["Бег", "Йога"] }],
  ["wipe preview", (a) => a.wipePackFeed("i", false), "/admin/wipe", { confirm: false }],
  ["db tables", (a) => a.fetchAdminDbTables("i"), "/admin/db/tables", {}],
  ["db table", (a) => a.fetchAdminDbTable("i", "events", 25, 50, "id", true), "/admin/db/table", { table: "events", limit: 25, offset: 50, order_by: "id", desc: true }],
  ["db columns", (a) => a.fetchAdminDbColumns("i", "events"), "/admin/db/columns", { table: "events" }],
  ["db query", (a) => a.runAdminDbQuery("i", "SELECT 1"), "/admin/db/query", { sql: "SELECT 1" }],
];

describe("admin API client", () => {
  const backend = loadBackendRoutes();

  it.each(CALLS)("%s", async (_name, run, path, body) => {
    const api = await load();
    const calls = mockFetch(() => ({ json: { ok: true } }));
    await run(api);
    expect(calls).toHaveLength(1);
    expect(calls[0].path).toBe(`/api/miniapp${path}`);
    expect(calls[0].method).toBe("POST");
    expect(calls[0].body).toEqual({ init_data: "i", ...body });
    // Путь и каждое поле должны существовать на сервере.
    if (backend) expect(contractProblems(backend, calls[0])).toEqual([]);
  });

  it("uploads a support reply with a photo as a form", async () => {
    const api = await load();
    const calls = mockFetch(() => ({ json: { ok: true } }));
    await api.sendAdminSupportReplyPhoto("i", 5, "смотри", photo);
    const sent: Sent = calls[0];
    expect(sent.path).toBe("/api/miniapp/admin/support/reply/photo");
    expect(sent.form?.get("target_user_id")).toBe("5");
    expect(sent.form?.get("text")).toBe("смотри");
    expect((sent.form?.get("photo") as File).name).toBe("shot.png");
    if (backend) expect(contractProblems(backend, sent)).toEqual([]);

    mockFetch(() => ({ status: 403, json: { error: "forbidden" } }));
    await expect(api.sendAdminSupportReplyPhoto("i", 5, "", photo)).rejects.toThrow("Нет прав администратора");
  });

  it("turns server errors into readable messages", async () => {
    const api = await load();
    mockFetch(() => ({ status: 403, json: { error: "forbidden" } }));
    await expect(api.fetchAdminOverview("i")).rejects.toThrow("Нет прав администратора");
    // Текст от сервера важнее кода.
    mockFetch(() => ({ status: 400, json: { error: "admin_error", message: "цена должна быть от 1 до 100000 ₽" } }));
    await expect(api.saveAdminPaywallPrice("i", 0)).rejects.toThrow("цена должна быть от 1 до 100000 ₽");
    // Код 200, но ok=false — тоже ошибка.
    mockFetch(() => ({ json: { ok: false, error: "not_found" } }));
    await expect(api.fetchAdminUserCard("i", 1)).rejects.toThrow("Не найдено или уже обработано");
    mockFetch(() => ({ status: 502 }));
    await expect(api.fetchAdminReports("i")).rejects.toThrow("Ошибка 502");
    mockFetch(() => ({ status: 400, json: { error: "что-то новое" } }));
    await expect(api.fetchAdminReports("i")).rejects.toThrow("что-то новое");
  });

  it("refuses to call anything without an API address", async () => {
    const api = await importWithApi(() => import("./adminApi"), "");
    const calls = mockFetch(() => ({ json: { ok: true } }));
    await expect(api.fetchAdminOverview("i")).rejects.toThrow("API не настроен");
    expect(() => api.sendAdminSupportReplyPhoto("i", 1, "", photo)).toThrow("API не настроен");
    expect(calls).toHaveLength(0);
  });

  it("labels error codes and payment kinds", async () => {
    const api = await load();
    expect(api.adminErrorLabel("chat_mismatch")).toBe("Открой мини-апп из чата стаи");
    expect(api.adminErrorLabel("tracker_not_configured")).toBe("Доска недоступна");
    expect(api.adminErrorLabel()).toBe("");
    expect(api.moneyKindLabel("donation", "xtr")).toBe("Донат · ⭐");
    expect(api.moneyKindLabel("access", "RUB")).toBe("Доступ · ₽");
  });
});

describe("mini app ↔ backend routes", () => {
  it("every API path used in the client exists on the server", () => {
    const backend = loadBackendRoutes();
    if (!backend) return; // исходников бэкенда рядом нет
    const files: string[] = [];
    const walk = (dir: string) => {
      for (const name of readdirSync(dir)) {
        const p = join(dir, name);
        if (statSync(p).isDirectory()) walk(p);
        else if (/\.(ts|tsx)$/.test(name) && !/\.test\.tsx?$/.test(name)) files.push(p);
      }
    };
    walk(join(__dirname, ".."));
    // Маршруты с переменной частью сервер разбирает по префиксу, а не точным путём.
    const prefixes = ["/api/miniapp/media/"];
    const missing = new Set<string>();
    let found = 0;
    for (const file of files) {
      for (const m of readFileSync(file, "utf8").matchAll(/["'`](\/api\/miniapp\/[a-z0-9\-_/]+)/g)) {
        found++;
        const path = m[1].replace(/\/$/, "");
        if (!backend.has(path) && !prefixes.some((p) => m[1].startsWith(p))) missing.add(`${path}  (${file.split("/src/")[1]})`);
      }
    }
    expect(found).toBeGreaterThan(40);
    expect([...missing]).toEqual([]);
  });
});

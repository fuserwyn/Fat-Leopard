/**
 * Помощники для тестов клиентов API: адрес сервера читается из окружения при
 * загрузке модуля, поэтому модуль импортируется заново с подставленным адресом,
 * а запросы перехватывает поддельный fetch.
 */
import { vi } from "vitest";

export const TEST_API = "https://api.test";

export type Sent = { path: string; method: string; body: Record<string, unknown> | null; form: FormData | null };

type Reply = { status?: number; json?: unknown; fail?: boolean };

/** Подменяет fetch: отвечает тем, что вернёт answer, и запоминает запросы. */
export function mockFetch(answer: (sent: Sent) => Reply) {
  const calls: Sent[] = [];
  const fn = vi.fn(async (url: string | URL, init?: RequestInit) => {
    const raw = init?.body;
    const sent: Sent = {
      path: String(url).replace(TEST_API, ""),
      method: init?.method ?? "GET",
      body: typeof raw === "string" ? (JSON.parse(raw) as Record<string, unknown>) : null,
      form: raw instanceof FormData ? raw : null,
    };
    calls.push(sent);
    const r = answer(sent);
    if (r.fail) throw new TypeError("Failed to fetch");
    const status = r.status ?? 200;
    return {
      ok: status >= 200 && status < 300,
      status,
      json: async () => {
        if (r.json === undefined) throw new SyntaxError("not json");
        return r.json;
      },
    } as Response;
  });
  vi.stubGlobal("fetch", fn);
  return calls;
}

/** Импортирует модуль заново с адресом API из тестового окружения. */
export async function importWithApi<T>(load: () => Promise<T>, apiUrl: string = TEST_API): Promise<T> {
  vi.resetModules();
  vi.stubEnv("VITE_MINIAPP_API_URL", apiUrl);
  return load();
}

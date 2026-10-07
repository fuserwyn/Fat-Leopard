/**
 * Контракт с бэкендом для тестов: читает исходники ms_leo и достаёт оттуда
 * маршруты API и поля, которые каждый обработчик принимает в теле запроса.
 * Клиент мини-аппа и сервер лежат в одном репозитории, но связаны только
 * строками — опечатка в пути или поле иначе видна лишь на проде.
 */
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

export type BackendRoute = { method: string; handler: string; fields: string[] };

const API_DIR = join(__dirname, "../../../ms_leo/internal/miniappapi");

/** null — исходников бэкенда рядом нет (сборка образа мини-аппа отдельно от репозитория). */
export function loadBackendRoutes(): Map<string, BackendRoute> | null {
  if (!existsSync(join(API_DIR, "server.go"))) return null;
  const sources = readdirSync(API_DIR)
    .filter((f) => f.endsWith(".go") && !f.endsWith("_test.go"))
    .map((f) => readFileSync(join(API_DIR, f), "utf8"));

  const fieldsByHandler = new Map<string, string[]>();
  const fnRe = /func \(s \*Server\) (\w+)\(w http\.ResponseWriter, r \*http\.Request\) \{([\s\S]*?)\n\}\n/g;
  for (const src of sources) {
    for (const m of src.matchAll(fnRe)) {
      const body = m[2];
      const json = [...body.split("json.NewDecoder")[0].matchAll(/`json:"([a-z_0-9]+)[^"]*"`/g)].map((x) => x[1]);
      const form = [...body.matchAll(/FormValue\("([a-z_0-9]+)"\)/g)].map((x) => x[1]);
      fieldsByHandler.set(m[1], [...new Set([...json, ...form, "photo"])]);
    }
  }

  const routes = new Map<string, BackendRoute>();
  const routeRe = /path == "(\/api\/miniapp\/[^"]+)" && r\.Method == http\.Method(\w+):\s*\n\s*s\.(\w+)\(/g;
  for (const m of readFileSync(join(API_DIR, "server.go"), "utf8").matchAll(routeRe)) {
    routes.set(m[1], { method: m[2].toUpperCase(), handler: m[3], fields: fieldsByHandler.get(m[3]) ?? [] });
  }
  return routes;
}

/** Что в запросе не сходится с бэкендом; пустой массив — всё совпало. */
export function contractProblems(
  routes: Map<string, BackendRoute>,
  sent: { path: string; method: string; body: Record<string, unknown> | null; form: FormData | null },
): string[] {
  const path = sent.path.split("?")[0];
  const route = routes.get(path);
  if (!route) return [`маршрута ${path} нет в server.go`];
  const problems: string[] = [];
  if (route.method !== sent.method.toUpperCase()) problems.push(`${path}: метод ${sent.method}, сервер ждёт ${route.method}`);
  const keys = sent.body ? Object.keys(sent.body) : sent.form ? [...new Set([...sent.form.keys()])] : [];
  for (const key of keys) {
    if (!route.fields.includes(key)) problems.push(`${path}: поле «${key}» сервер не читает (читает: ${route.fields.join(", ")})`);
  }
  return problems;
}

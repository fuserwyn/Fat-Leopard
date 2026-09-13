/**
 * Своя доска задач в админке. Мини-апп ходит только к ms_leo
 * (/api/miniapp/admin/tracker): карточки лежат в нашей базе.
 */
const api = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

export type TrackerAttachment = {
  id: string;
  name: string;
  mime: string;
  size: number;
  url: string;
};

export type TrackerTask = {
  id: number;
  /** Номер внутри доски: id общий, участнику ничего не говорит. */
  num?: number;
  prompt: string;
  repo: string;
  when: string;
  repeat: string;
  kind: string;
  status: string;
  status_label: string;
  status_icon: string;
  done: boolean;
  active: boolean;
  can_delete: boolean;
  /** Можно перезапустить агента с доски. */
  can_restart?: boolean;
  /** Можно изменить текст задачи, пока агент не взял её в работу. */
  can_edit_prompt?: boolean;
  auto_review: boolean;
  manual_qa: boolean;
  fast_track: boolean;
  error: string;
  has_result: boolean;
  phase: string;
  qa_status: string | null;
  qa_label: string;
  qa_icon: string;
  auto_qa_running: boolean;
  dev_column: string;
  qa_column: string | null;
  handed_to_qa: boolean;
  attachments_count: number;
  has_attachments: boolean;
  /** Приходит только в детали задачи: что именно приложено. */
  attachments?: TrackerAttachment[];
  /** Ветка задачи: пусто — основной стенд, иначе тестовый. */
  branch?: string;
  /** Короткий sha коммита, которым закончилась задача. */
  commit?: string;
  /** Агент сам пушит результат. Выкл — карточка «выполнена», сервер без сборки. */
  auto_push?: boolean;
  needs_approval?: boolean;
  approvals_count?: number;
  approvals_needed?: number;
  author_id: number | null;
  steps?: string[];
  steps_running?: boolean;
  live_step?: string;
  result?: string;
  /** Краткий отчёт по завершённой задаче (выполнение, ревью, тест, сборка). */
  done_summary?: string;
  created_at?: string;
  last_run_at?: string;
  model_key?: string;
};

export type SprintIdea = {
  id?: string;
  title?: string;
  name?: string;
  summary?: string;
  description?: string;
  [key: string]: unknown;
};

export type SprintFeature = {
  title?: string;
  prompt?: string;
  sprint?: number;
  requirements?: unknown;
  acceptance?: unknown;
  test_criteria?: unknown;
  [key: string]: unknown;
};

export type TrackerOp =
  | "list"
  | "refresh"
  | "create"
  | "task"
  | "status"
  | "cancel"
  | "delete"
  | "qa"
  | "auto_qa"
  | "review"
  | "auto_test"
  | "prompt"
  | "reschedule"
  | "restart"
  | "promote"
  | "revert"
  | "ship"
  | "deploy"
  | "deploy_settings"
  | "move"
  | "sprint_ideas"
  | "sprint_generate"
  | "sprint_apply"
  | "approve";

type Envelope<T> = { ok?: boolean; data?: T | string; error?: string; message?: string };

async function call<T>(
  initData: string,
  op: TrackerOp,
  extra: { task_id?: number; payload?: Record<string, unknown> } = {},
): Promise<T> {
  if (!api) throw new Error("API не настроен");
  const res = await fetch(`${api}/api/miniapp/admin/tracker`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ init_data: initData, op, ...extra }),
  });
  const j = (await res.json().catch(() => ({}))) as Envelope<T>;
  if (!res.ok || j.ok === false) {
    throw new Error(j.message || trackerErrorLabel(j.error) || `Ошибка ${res.status}`);
  }
  return unwrapTrackerData<T>(j.data);
}

function unwrapTrackerData<T>(raw: T | string | undefined): T {
  if (raw && typeof raw === "object") return raw;
  if (typeof raw === "string") {
    try {
      return JSON.parse(raw) as T;
    } catch {
      /* строка не JSON */
    }
  }
  return {} as T;
}

function trackerErrorLabel(code?: string): string {
  switch (code) {
    case "forbidden":
      return "Нет прав администратора";
    case "chat_mismatch":
      return "Открой мини-апп из чата стаи";
    case "tracker not configured":
    case "tracker_not_configured":
      return "Доска недоступна";
    case "invalid_action":
      return "Такое действие доске недоступно";
    case "admin_error":
      return "Доска не приняла задачу";
    default:
      return code ?? "";
  }
}

export function trackerList(initData: string) {
  return call<{ tasks: TrackerTask[]; repo: string | null; started?: number }>(initData, "list");
}

/** Кнопка «Обновить»: снимает созревшие с очереди и отдаёт свежую доску. */
export function trackerRefresh(initData: string) {
  return call<{ tasks: TrackerTask[]; repo: string | null; started?: number }>(initData, "refresh");
}

export function trackerCreate(
  initData: string,
  payload: {
    when: string;
    prompt: string;
    auto_review?: boolean;
    manual_qa?: boolean;
    fast_track?: boolean;
    needs_approval?: boolean;
    /** Агент пушит сам, иначе «выполнено» не попадёт на сервер. */
    auto_push?: boolean;
    /** Задачу придумал Лео: на доске автором пишем его. */
    leo?: boolean;
  },
) {
  return call<{ id: number; when: string }>(initData, "create", {
    payload: { auto_push: true, ...payload },
  });
}

/** Забрать результат выполненной задачи со стенда в основную ветку. */
export function trackerPromote(initData: string, taskId: number) {
  return call<{ commit: string }>(initData, "promote", { payload: { id: taskId } });
}

/** Откатить результат выполненной задачи обратным коммитом. */
export function trackerRevert(initData: string, taskId: number) {
  return call<{ commit: string }>(initData, "revert", { payload: { id: taskId } });
}

/** Отметить задачу готовой к публикации. Выкатить код — отдельным «запушь». */
export function trackerShip(initData: string, taskId: number) {
  return call<{ promoted?: boolean; pushed?: boolean; deployed?: boolean; skipped?: boolean; error?: string }>(
    initData,
    "ship",
    { task_id: taskId, payload: { id: taskId } },
  );
}

/** Состояние автодеплоя: сам ли трекер просит Railway собраться после выката. */
export type TrackerDeploy = {
  enabled: boolean;
  configured: boolean;
  services?: string[];
  hint?: string;
};

/** Выкатить задачу на Railway прямо сейчас — даже если карточка уже «Выполнено». */
export function trackerDeployNow(initData: string, taskId: number) {
  return call<{ deploy?: boolean; busy?: boolean }>(initData, "deploy", {
    task_id: taskId,
    payload: { id: taskId },
  });
}

/** Прочитать (status) или переключить (on/off) автодеплой доски. */
export function trackerDeploySettings(initData: string, action: "status" | "on" | "off" = "status") {
  return call<{ deploy: TrackerDeploy }>(initData, "deploy_settings", { payload: { action } });
}

export function trackerTask(initData: string, taskId: number) {
  return call<{ task: TrackerTask }>(initData, "task", { task_id: taskId });
}

export function trackerCancel(initData: string, taskId: number) {
  return call<{ ok: boolean }>(initData, "cancel", { payload: { id: taskId } });
}

/** Аппрув или отклонение задачи на колонке «Аппрув». */
export function trackerApprove(
  initData: string,
  taskId: number,
  action: "approve" | "reject",
  comment?: string,
) {
  return call<{ ok: boolean; task?: TrackerTask }>(initData, "approve", {
    payload: { id: taskId, action, comment },
  });
}

export function trackerDelete(initData: string, taskId: number) {
  return call<{ ok: boolean }>(initData, "delete", { task_id: taskId });
}

/** Доска тестировщика: start — взять в тест, pass — принять, fail — вернуть, reset — в очередь. */
export function trackerQa(initData: string, taskId: number, action: "start" | "pass" | "fail" | "reset") {
  return call<{ ok: boolean }>(initData, "qa", { payload: { id: taskId, action } });
}

/** Обновить формулировку задачи, пока она ещё не началась. */
export function trackerPrompt(initData: string, taskId: number, prompt: string) {
  return call<{ ok: boolean; task?: TrackerTask }>(initData, "prompt", {
    task_id: taskId,
    payload: { id: taskId, prompt },
  });
}

/** Перенести запуск. Отменённую/завершённую этим же вернуть в «Ожидает» — время должно быть в будущем. */
export function trackerReschedule(initData: string, taskId: number, when: string) {
  return call<{ ok: boolean }>(initData, "reschedule", { payload: { id: taskId, when } });
}

/** Сдвинуть карточку: column — колонка или «next». */
export function trackerMove(initData: string, taskId: number, column: string) {
  return call<{ ok: boolean }>(initData, "move", { task_id: taskId, payload: { id: taskId, column } });
}

/** Перезапустить задачу: сбросить ошибку/QA и снова пустить агента. */
export function trackerRestart(initData: string, taskId: number) {
  return call<{ ok: boolean; task?: TrackerTask }>(initData, "restart", {
    task_id: taskId,
    payload: { id: taskId },
  });
}

/** @deprecated используй trackerRestart */
export function trackerRunNow(initData: string, taskId: number) {
  return trackerRestart(initData, taskId);
}

export function trackerAutoQa(initData: string, taskId: number) {
  return call<{ ok: boolean }>(initData, "auto_qa", { payload: { id: taskId } });
}

/** Ревью на Cursor Composer: читает код и двигает карточку на тест. */
export function trackerReview(initData: string, taskId: number) {
  return call<{ ok: boolean }>(initData, "review", { payload: { id: taskId } });
}

/** Тест на Cursor Composer: проверки по задаче, затем сборка. */
export function trackerAutoTest(initData: string, taskId: number) {
  return call<{ ok: boolean }>(initData, "auto_test", { payload: { id: taskId } });
}

/** Приложить картинку к задаче: base64 без префикса data:. */
export async function trackerAttachImage(
  initData: string,
  taskId: number,
  image: { data: string; filename: string; mime: string },
): Promise<void> {
  if (!api) throw new Error("API не настроен");
  const res = await fetch(`${api}/api/miniapp/admin/tracker/attach`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ init_data: initData, task_id: taskId, ...image }),
  });
  const j = (await res.json().catch(() => ({}))) as { ok?: boolean; error?: string; message?: string };
  if (!res.ok || j.ok === false) {
    throw new Error(j.message || trackerErrorLabel(j.error) || `Ошибка ${res.status}`);
  }
}

/** Лео сам придумывает задачу. hint пуст — тему выбирает сам; busy — что уже на доске и что отклонили. */
export async function leoProposeTask(
  initData: string,
  hint: string,
  busy: string[],
): Promise<{ reply: string; title: string; task: string }> {
  if (!api) throw new Error("API не настроен");
  const res = await fetch(`${api}/api/miniapp/admin/tracker/leo-propose`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ init_data: initData, hint, busy }),
  });
  const j = (await res.json().catch(() => ({}))) as {
    ok?: boolean;
    reply?: string;
    title?: string;
    task?: string;
    error?: string;
    message?: string;
  };
  if (!res.ok || j.ok === false) {
    throw new Error(j.message || trackerErrorLabel(j.error) || `Ошибка ${res.status}`);
  }
  return { reply: j.reply ?? "", title: j.title ?? "", task: j.task ?? "" };
}

/** Байты приложенного фото: отдаём через свой бэкенд, ссылкой их не показать. */
export async function trackerAttachmentGet(
  initData: string,
  taskId: number,
  attId: string,
): Promise<string> {
  if (!api) throw new Error("API не настроен");
  const res = await fetch(`${api}/api/miniapp/admin/tracker/attachment`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ init_data: initData, action: "get", task_id: taskId, att_id: attId }),
  });
  const j = (await res.json().catch(() => ({}))) as {
    ok?: boolean;
    mime?: string;
    data?: string;
    error?: string;
    message?: string;
  };
  if (!res.ok || j.ok === false || !j.data) {
    throw new Error(j.message || trackerErrorLabel(j.error) || `Ошибка ${res.status}`);
  }
  return `data:${j.mime || "image/jpeg"};base64,${j.data}`;
}

/** Снять фото с задачи. «Заменить» — это снять старое и приложить новое. */
export async function trackerAttachmentDelete(
  initData: string,
  taskId: number,
  attId: string,
): Promise<void> {
  if (!api) throw new Error("API не настроен");
  const res = await fetch(`${api}/api/miniapp/admin/tracker/attachment`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ init_data: initData, action: "delete", task_id: taskId, att_id: attId }),
  });
  const j = (await res.json().catch(() => ({}))) as { ok?: boolean; error?: string; message?: string };
  if (!res.ok || j.ok === false) {
    throw new Error(j.message || trackerErrorLabel(j.error) || `Ошибка ${res.status}`);
  }
}

/** Автономный режим: Лео сам придумывает спринты, пока админ его не выключит. */
export type LeoAutonomy = {
  active: boolean;
  active_until: string;
  next_run_at: string;
  every_hours: number;
  tasks_per_run: number;
  last_run_at: string;
  last_note: string;
  max_days: number;
};

/** action: status — только посмотреть; start — включить на days дней; stop — выключить. */
export async function leoAutonomy(
  initData: string,
  payload: { action: "status" | "start" | "stop"; days?: number; every_hours?: number; tasks_per_run?: number },
): Promise<LeoAutonomy> {
  if (!api) throw new Error("API не настроен");
  const res = await fetch(`${api}/api/miniapp/admin/tracker/leo-autonomy`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ init_data: initData, ...payload }),
  });
  const j = (await res.json().catch(() => ({}))) as {
    ok?: boolean;
    autonomy?: LeoAutonomy;
    error?: string;
    message?: string;
  };
  if (!res.ok || j.ok === false || !j.autonomy) {
    throw new Error(j.message || trackerErrorLabel(j.error) || `Ошибка ${res.status}`);
  }
  return j.autonomy;
}

/** Спринт глазами Лео: реплика, тема и набор задач. */
export async function leoSprint(
  initData: string,
  hint: string,
): Promise<{ reply: string; theme: string; tasks: string[] }> {
  if (!api) throw new Error("API не настроен");
  const res = await fetch(`${api}/api/miniapp/admin/tracker/leo-sprint`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ init_data: initData, hint }),
  });
  const j = (await res.json().catch(() => ({}))) as {
    ok?: boolean;
    reply?: string;
    theme?: string;
    tasks?: string[];
    error?: string;
    message?: string;
  };
  if (!res.ok || j.ok === false) {
    throw new Error(j.message || trackerErrorLabel(j.error) || `Ошибка ${res.status}`);
  }
  return { reply: j.reply ?? "", theme: j.theme ?? "", tasks: j.tasks ?? [] };
}

export type TrackerPerson = { user_id: number; username: string; display_name: string };

/** Кто ставил задачи: в трекере есть только author_id, ник живёт у нас. */
export async function trackerAuthors(initData: string, ids: number[]): Promise<TrackerPerson[]> {
  if (!api || ids.length === 0) return [];
  const res = await fetch(`${api}/api/miniapp/admin/tracker/authors`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ init_data: initData, ids }),
  });
  const j = (await res.json().catch(() => ({}))) as { ok?: boolean; people?: TrackerPerson[] };
  if (!res.ok || j.ok === false) return [];
  return j.people ?? [];
}

/** Аватарка участника: сервер отдаёт байты через Bot API, токен в клиент не уезжает. */
export function trackerAvatarUrl(initData: string, userId: number): string {
  if (!api || !userId) return "";
  return `${api}/api/miniapp/user-avatar?init_data=${encodeURIComponent(initData)}&user_id=${userId}`;
}

export function sprintIdeas(initData: string, hint: string) {
  return call<{ ideas: SprintIdea[]; recommended_id?: string; analysis?: string; topic_question?: string }>(
    initData,
    "sprint_ideas",
    { payload: { hint } },
  );
}

export function sprintGenerate(
  initData: string,
  payload: { hint: string; idea: SprintIdea; sprint_count: number; tasks_per_sprint: number },
) {
  return call<{ features: SprintFeature[]; plan?: Record<string, unknown>; analysis?: string }>(
    initData,
    "sprint_generate",
    { payload },
  );
}

export function sprintApply(
  initData: string,
  payload: { features: SprintFeature[]; sprint_count: number; tasks_per_sprint: number },
) {
  return call<{ created?: number; ok?: boolean }>(initData, "sprint_apply", {
    payload: { auto_push: true, ...payload },
  });
}

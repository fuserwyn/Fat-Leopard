import { useCallback, useEffect, useMemo, useRef, useState, type RefObject } from "react";
import {
  leoAutonomy,
  leoProposeTask,
  type LeoProposeResult,
  leoSprint,
  sprintApply,
  sprintGenerate,
  sprintIdeas,
  trackerAttachImage,
  trackerAttachmentDelete,
  trackerAttachmentGet,
  trackerAuthors,
  trackerAutoQa,
  trackerAvatarUrl,
  trackerCancel,
  trackerCreate,
  trackerDelete,
  trackerDeployNow,
  trackerDeploySettings,
  trackerList,
  trackerMove,
  trackerPrompt,
  trackerQa,
  trackerRefresh,
  trackerReschedule,
  trackerReview,
  trackerApprove,
  trackerRestart,
  trackerShip,
  trackerTask,
  trackerAutoTest,
  type LeoAutonomy,
  type SprintFeature,
  type TrackerDeploy,
  type SprintIdea,
  type TrackerTask,
} from "../lib/trackerApi";
import { TaskImageEditor, type TaskImage } from "./TaskImageEditor";
import { LEO_AVATAR_URL } from "../lib/leoAvatar";
import "./TrackerScreen.css";

type Props = {
  initData: string;
  showAlert: (text: string) => void;
};

/** Колонки разработчика и тестировщика. */
const DEV_COLS = [
  { key: "todo", title: "Pending" },
  { key: "approve", title: "Approve" },
  { key: "doing", title: "In progress" },
  { key: "review", title: "Review" },
  { key: "test", title: "Test" },
  { key: "deploy", title: "Deploy" },
  { key: "done", title: "Done" },
  { key: "canceled", title: "Canceled" },
];

const QA_COLS = [
  { key: "todo", title: "To test" },
  { key: "doing", title: "Testing" },
  { key: "done", title: "Done" },
];

const WHEN_PRESETS = [
  { value: "сейчас", label: "Now" },
  { value: "завтра 4:20", label: "Tomorrow at 4:20" },
  { value: "custom", label: "Date & time…" },
];

/** «2026-08-20T09:00» из datetime-local → «2026-08-20 09:00», как ждёт трекер. */
function whenFromPicker(value: string): string {
  return value.replace("T", " ");
}

/** «[Спринт 2] текст» → номер спринта отдельным бейджем. */
function parsePrompt(prompt: string): { sprint: number | null; text: string } {
  const raw = String(prompt || "").trim();
  const m = raw.match(/^\[(?:Спринт|Sprint)\s+(\d+)\]\s*/i);
  if (m) return { sprint: Number(m[1]), text: raw.slice(m[0].length).trim() || raw };
  return { sprint: null, text: raw };
}

/** Номер задачи на доске. У старых задач его нет — показываем id, чтобы не пустовало. */
function taskNo(task: TrackerTask): number {
  return Number(task.num) || Number(task.id);
}

/** Агент не стартовал, карточка зависла в «В работе». */
function canRetryAgent(task: TrackerTask): boolean {
  const status = String(task.status || "").toLowerCase();
  const column = String(task.dev_column || "").toLowerCase();
  const err = String(task.error || "");
  const failed = /агент не стартовал|agent failed to start|только заметка|note only|нет правок|no code changes/i.test(err);
  return failed && (status === "running" || status === "error" || column === "doing");
}

/** Текст задачи можно править, пока агент её не взял. */
function canEditPrompt(task: TrackerTask): boolean {
  if (typeof task.can_edit_prompt === "boolean") return task.can_edit_prompt;
  const status = String(task.status || "").toLowerCase();
  const column = String(task.dev_column || "").toLowerCase();
  if (["running", "reviewing", "holding", "done", "completed", "canceled", "cancelled"].includes(status)) {
    return false;
  }
  if (["doing", "review", "test", "deploy", "done", "canceled"].includes(column)) return false;
  return column === "todo" || column === "approve" || column === "";
}

/** Собрать prompt с префиксом спринта, если он был. */
function buildPromptText(task: TrackerTask, text: string): string {
  const parsed = parsePrompt(task.prompt);
  const body = text.trim();
  if (!body) return "";
  if (parsed.sprint != null) return `[Sprint ${parsed.sprint}] ${body}`;
  return body;
}

/** Завершённую, отменённую, упавшую или зависшую задачу можно снова поставить. */
function canReturnToWork(task: TrackerTask): boolean {
  if (typeof task.can_restart === "boolean") return task.can_restart;
  const status = String(task.status || "").toLowerCase();
  const column = String(task.dev_column || "").toLowerCase();
  return canRetryAgent(task)
    || ["canceled", "cancelled", "done", "error", "holding"].includes(status)
    || ["canceled", "cancelled", "done"].includes(column);
}

/** Исполнителя меняем до старта и когда задачу можно запустить снова: был Cursor — выбрали Claude. */
function canChangeAgent(task: TrackerTask): boolean {
  return canEditPrompt(task) || canReturnToWork(task);
}

/** Пуш и сборка: агент уже закончил. Коммит не обязателен — с сервера git push часто закрыт. */
function canShipTask(task: TrackerTask): boolean {
  const status = String(task.status || "").toLowerCase();
  if (["running", "reviewing", "pending", "scheduled"].includes(status)) return false;
  if (task.done) return true;
  if (["done", "completed", "holding"].includes(status)) return true;
  const column = String(task.dev_column || "").toLowerCase();
  if (["done", "deploy", "test"].includes(column)) return true;
  const qa = String(task.qa_column || "").toLowerCase();
  return task.handed_to_qa && (qa === "done" || String(task.qa_status || "").toLowerCase() === "pass");
}

/** RFC3339 с сервера → «19.08 14:30» в часовом поясе читателя. Пусто — прочерк. */
function formatWhen(iso: string): string {
  const t = Date.parse(iso || "");
  if (!t) return "—";
  return new Date(t).toLocaleString("en-US", {
    day: "2-digit",
    month: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** Задачу придумал Лео, а админ одобрил. */
const LEO_AUTHOR_ID = -1;

/** На доске автором показываем Лео: id −1 или kind leo_task. */
function isLeoTask(task: TrackerTask): boolean {
  const id = Number(task.author_id) || 0;
  return id === LEO_AUTHOR_ID || task.kind === "leo_task";
}

/** В «Ожидает» — FIFO: самая старая созданная задача выше. */
function sortTodoColumnFifo(tasks: TrackerTask[]): TrackerTask[] {
  return [...tasks].sort((a, b) => {
    const ca = Date.parse(a.created_at || "") || 0;
    const cb = Date.parse(b.created_at || "") || 0;
    if (ca !== cb) return ca - cb;
    return (Number(a.id) || 0) - (Number(b.id) || 0);
  });
}

/** Задача ждёт аппрува — на доске можно поставить галочку без открытия карточки. */
function canApproveOnBoard(task: TrackerTask, isQa: boolean): boolean {
  if (isQa) return false;
  const needed = task.approvals_needed ?? 2;
  const count = task.approvals_count ?? 0;
  return Boolean(task.needs_approval) && boardDevColumn(task) === "approve" && count < needed;
}

/** Колонка на доске: без аппрува карточка в «Аппрув», не в «Ожидает». */
function boardDevColumn(task: TrackerTask): string {
  const col = String(task.dev_column || "todo").toLowerCase();
  const needed = task.approvals_needed ?? 2;
  const count = task.approvals_count ?? 0;
  const status = String(task.status || "").toLowerCase();
  if (
    task.needs_approval &&
    count < needed &&
    !["done", "canceled", "cancelled"].includes(col) &&
    !["done", "canceled", "cancelled"].includes(status)
  ) {
    if (["doing", "review", "test", "deploy"].includes(col)) return col;
    return "approve";
  }
  return col || "todo";
}

const NEXT_COL: Record<string, { column: string; label: string }> = {
  todo: { column: "doing", label: "Start work" },
  approve: { column: "doing", label: "Start work" },
  doing: { column: "review", label: "To review" },
  review: { column: "test", label: "To test" },
  test: { column: "deploy", label: "To deploy" },
  deploy: { column: "done", label: "Done" },
};

/** Автор задачи: Лео или человек из админки. */
function authorLabel(task: TrackerTask, authors: Record<number, string>): string {
  if (isLeoTask(task)) return "Leo";
  const id = Number(task.author_id) || 0;
  if (!id) return "From chat";
  return authors[id] || `id ${id}`;
}

/** Картинка автора: у Лео своя, у людей — из чата стаи, у задач из чата её нет. */
function authorAvatar(task: TrackerTask, initData: string): string {
  if (isLeoTask(task)) return LEO_AVATAR_URL;
  const id = Number(task.author_id) || 0;
  return id > 0 ? trackerAvatarUrl(initData, id) : "";
}

type Approver = { id: number; name: string; avatar: string };

/** Кто одобрил карточку: имя и картинка для каждого голоса, Лео — со своей. */
function taskApprovers(task: TrackerTask, authors: Record<number, string>, initData: string): Approver[] {
  return (task.approver_ids ?? []).map((id) =>
    id === LEO_AUTHOR_ID
      ? { id, name: "Leo", avatar: LEO_AVATAR_URL }
      : { id, name: authors[id] || `id ${id}`, avatar: trackerAvatarUrl(initData, id) },
  );
}

/** Аватарки одобривших — стопкой рядом со счётчиком 👍. */
function ApproverAvatars({ approvers }: { approvers: Approver[] }) {
  if (approvers.length === 0) return null;
  return (
    <span className="tracker-approvers" aria-label={`Approved: ${approvers.map((a) => a.name).join(", ")}`}>
      {approvers.map((a) =>
        a.avatar ? (
          <img key={a.id} src={a.avatar} alt="" title={a.name} loading="lazy" referrerPolicy="no-referrer" />
        ) : (
          <span key={a.id} className="tracker-approvers__dot" title={a.name}>
            {a.name.replace(/^@/, "").slice(0, 1).toUpperCase()}
          </span>
        ),
      )}
    </span>
  );
}

function plural(n: number, one: string, few: string, many: string): string {
  const mod10 = n % 10;
  const mod100 = n % 100;
  if (mod10 === 1 && mod100 !== 11) return one;
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 10 || mod100 >= 20)) return few;
  return many;
}

/** Короткий хвост результата на карточке: «Готово» видно, не раскрывая модалку. */
function clipCardText(text: string, max = 180): string {
  const raw = String(text || "").replace(/\s+/g, " ").trim();
  if (raw.length <= max) return raw;
  return `${raw.slice(0, max)}…`;
}

function cardClasses(t: TrackerTask, isQa: boolean): string {
  const cls = ["tracker-card"];
  if (isQa) {
    if ((t.qa_column || "todo") === "doing") cls.push("is-running");
  } else if (t.status === "running" || t.status === "reviewing") cls.push("is-running");
  else if (boardDevColumn(t) === "review") cls.push("is-review");
  else if (boardDevColumn(t) === "approve") cls.push("is-approve");
  else if (boardDevColumn(t) === "test") cls.push("is-qa");
  else if (boardDevColumn(t) === "deploy") cls.push("is-deploy");
  else if (t.status === "holding") cls.push("is-holding");
  if (t.error) cls.push("is-err");
  if (t.handed_to_qa && !isQa) cls.push("is-qa");
  return cls.join(" ");
}

type TrackerAgent = "auto" | "claude" | "cursor";

const AGENT_OPTIONS: { value: TrackerAgent; label: string }[] = [
  { value: "claude", label: "Claude" },
  { value: "cursor", label: "Cursor" },
];

function trackerAgentOf(raw: string | undefined): TrackerAgent {
  return raw === "cursor" ? "cursor" : "claude";
}

function trackerAgentLabel(raw: string | undefined): string {
  if (raw === "claude") return "Claude";
  if (raw === "cursor") return "Cursor";
  return "Auto";
}

function metaParts(t: TrackerTask, isQa: boolean): string[] {
  const parts: string[] = [];
  if (!isQa && (t.agent === "claude" || t.agent === "cursor")) parts.push(trackerAgentLabel(t.agent));
  if (t.when) parts.push(t.when);
  if (t.repeat && t.repeat !== "once" && t.repeat !== "разово") parts.push(t.repeat);
  if (!isQa && t.review === false) parts.push("no review");
  if (!isQa && t.test === false) parts.push("no test");
  if (!isQa && t.auto_review) parts.push("auto review");
  if (t.manual_qa) parts.push("manual QA");
  if (t.fast_track) parts.push("no review or test");
  if (t.auto_push === false) parts.push("no push");
  if (!isQa && t.handed_to_qa) parts.push("→ QA");
  if (t.error) parts.push("error");
  return parts;
}

function isTrackerTextField(el: Element | null): el is HTMLElement {
  if (el instanceof HTMLTextAreaElement) return true;
  if (el instanceof HTMLInputElement) {
    return !["button", "submit", "reset", "checkbox", "radio", "file", "hidden", "image"].includes(el.type);
  }
  return false;
}

/** Нижняя граница реально видимой области (над клавиатурой) в координатах layout-вьюпорта. */
function visibleViewportBottom(): number {
  let bottom = window.innerHeight || document.documentElement.clientHeight;
  const vv = window.visualViewport;
  if (vv && vv.height > 0) bottom = Math.min(bottom, vv.offsetTop + vv.height);
  const tgH = window.Telegram?.WebApp?.viewportHeight;
  if (typeof tgH === "number" && tgH > 100) bottom = Math.min(bottom, tgH);
  return bottom;
}

function scrollableAncestor(el: HTMLElement): HTMLElement | null {
  let p = el.parentElement;
  while (p && p !== document.body) {
    const oy = window.getComputedStyle(p).overflowY;
    if ((oy === "auto" || oy === "scroll") && p.scrollHeight > p.clientHeight) return p;
    p = p.parentElement;
  }
  return null;
}

/**
 * Поле ввода в трекере целиком над клавиатурой. Глобальный scrollIntoView
 * срабатывает раньше, чем клавиатура доедет и обновится высота вьюпорта,
 * поэтому, пока поле в фокусе, докручиваем его после каждого изменения вьюпорта.
 */
function useKeepFocusedFieldVisible(rootRef: RefObject<HTMLDivElement | null>) {
  useEffect(() => {
    const root = rootRef.current;
    if (!root) return;
    let field: HTMLElement | null = null;
    const timers: number[] = [];
    const MARGIN = 12;

    const reveal = () => {
      if (!field || document.activeElement !== field || !field.isConnected) return;
      const rect = field.getBoundingClientRect();
      const container = scrollableAncestor(field);
      const containerTop = container ? Math.max(0, container.getBoundingClientRect().top) : 0;
      const vv = window.visualViewport;
      const top = Math.max(containerTop, vv ? vv.offsetTop : 0) + MARGIN;
      const bottom = visibleViewportBottom() - MARGIN;
      let delta = 0;
      if (rect.height > bottom - top || rect.top < top) {
        // Не влезает целиком или ушло вверх — выравниваем по верху видимой области.
        delta = rect.top - top;
      } else if (rect.bottom > bottom) {
        delta = rect.bottom - bottom;
      }
      if (Math.abs(delta) < 1) return;
      if (container) container.scrollTop += delta;
      else window.scrollBy(0, delta);
    };

    const clearTimers = () => {
      while (timers.length) window.clearTimeout(timers.pop());
    };

    const schedule = () => {
      if (!field) return;
      clearTimers();
      for (const ms of [0, 120, 300, 500, 800]) timers.push(window.setTimeout(reveal, ms));
    };

    const onFocusIn = (e: FocusEvent) => {
      const t = e.target as Element | null;
      if (!isTrackerTextField(t)) return;
      field = t;
      schedule();
    };

    const onFocusOut = () => {
      window.setTimeout(() => {
        if (field && document.activeElement !== field) {
          field = null;
          clearTimers();
        }
      }, 0);
    };

    root.addEventListener("focusin", onFocusIn);
    root.addEventListener("focusout", onFocusOut);
    const vv = window.visualViewport;
    vv?.addEventListener("resize", schedule);
    window.addEventListener("resize", schedule);
    const tg = window.Telegram?.WebApp as { onEvent?: (e: string, fn: () => void) => void } | undefined;
    tg?.onEvent?.("viewportChanged", schedule);

    return () => {
      clearTimers();
      root.removeEventListener("focusin", onFocusIn);
      root.removeEventListener("focusout", onFocusOut);
      vv?.removeEventListener("resize", schedule);
      window.removeEventListener("resize", schedule);
      const tgOff = window.Telegram?.WebApp as { offEvent?: (e: string, fn: () => void) => void } | undefined;
      tgOff?.offEvent?.("viewportChanged", schedule);
    };
  }, [rootRef]);
}

export function TrackerScreen({ initData, showAlert }: Props) {
  const rootRef = useRef<HTMLDivElement | null>(null);
  useKeepFocusedFieldVisible(rootRef);
  const [tab, setTab] = useState<"board" | "task" | "sprint">("board");
  /** author_id → как показать человека. Задачи знают только id. */
  const [authors, setAuthors] = useState<Record<number, string>>({});
  const [role, setRole] = useState<"developer" | "tester">("developer");
  const [tasks, setTasks] = useState<TrackerTask[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [prompt, setPrompt] = useState("");
  const [when, setWhen] = useState(WHEN_PRESETS[0].value);
  const [whenAt, setWhenAt] = useState("");
  const [needsApproval, setNeedsApproval] = useState(false);
  const [withReview, setWithReview] = useState(true);
  const [withTest, setWithTest] = useState(true);
  /** Кто пишет код. По умолчанию Claude, Cursor — только явный выбор. */
  const [agent, setAgent] = useState<TrackerAgent>("claude");
  /** Задачу от Лео на доску — с аппрувом всех админов. */
  const [leoNeedsApproval, setLeoNeedsApproval] = useState(true);
  const [images, setImages] = useState<TaskImage[]>([]);
  /** Тема для Лео; пусто — придумывает сам. */
  const [leoTopic, setLeoTopic] = useState("");
  /** Автономный режим: Лео сам придумывает спринты, пока админ не выключит. */
  const [autonomy, setAutonomy] = useState<LeoAutonomy | null>(null);
  const [autonomyDays, setAutonomyDays] = useState(1);
  const [autonomyBusy, setAutonomyBusy] = useState(false);
  /** Автодеплой: трекер сам просит Railway собраться после пуша в main. */
  const [deploy, setDeploy] = useState<TrackerDeploy | null>(null);
  const [deployBusy, setDeployBusy] = useState(false);
  /** Задача от Лео на утверждении: он предлагает — админ решает. */
  const [proposal, setProposal] = useState<LeoProposeResult | null>(null);
  const [selectedVariant, setSelectedVariant] = useState(0);
  const [discussFeedback, setDiscussFeedback] = useState("");
  const [rejected, setRejected] = useState<string[]>([]);
  const [proposeBusy, setProposeBusy] = useState(false);
  const [editorFor, setEditorFor] = useState<"new" | number | null>(null);
  /** Картинка из буфера обмена: с ней редактор открывается сразу, минуя выбор файла. */
  const [pasted, setPasted] = useState<Blob | null>(null);
  /** Что заменяем: после того как новая картинка приложена, старую снимаем. */
  const [replacing, setReplacing] = useState<string | null>(null);
  /** id вложения → data-URL. Байты идут через наш бэкенд, ссылкой их не показать. */
  const [attPreviews, setAttPreviews] = useState<Record<string, string>>({});
  /** Открытая на весь экран картинка. */
  const [zoomed, setZoomed] = useState<string | null>(null);
  const [detail, setDetail] = useState<TrackerTask | null>(null);
  const [editingPrompt, setEditingPrompt] = useState(false);
  const [promptDraft, setPromptDraft] = useState("");
  const [moveAt, setMoveAt] = useState("");
  /** Карточка, которую просят удалить с доски — показываем модалку подтверждения. */
  const [deleteAsk, setDeleteAsk] = useState<TrackerTask | null>(null);

  const [hint, setHint] = useState("");
  const [sprintCount, setSprintCount] = useState(1);
  const [tasksPerSprint, setTasksPerSprint] = useState(5);
  const [ideas, setIdeas] = useState<SprintIdea[]>([]);
  const [pickedIdea, setPickedIdea] = useState<string>("");
  const [features, setFeatures] = useState<(SprintFeature & { _on: boolean })[]>([]);
  const [sprintBusy, setSprintBusy] = useState<"" | "ideas" | "plan" | "apply" | "leo">("");
  const [leoSprintReply, setLeoSprintReply] = useState("");
  /** Спринт сгенерирован Лео — перед стартом нужен аппрув админов. */
  const [fromLeoSprint, setFromLeoSprint] = useState(false);
  /** Подвкладка «Sprints»: загрузка спринтов или функционал Лео. */
  const [sprintTab, setSprintTab] = useState<"load" | "leo">("load");

  const isQa = role === "tester";
  const detailRef = useRef<number | null>(null);
  detailRef.current = detail?.id ?? null;

  const load = useCallback(
    async (silent = false, claim = false) => {
      try {
        const j = claim ? await trackerRefresh(initData) : await trackerList(initData);
        const list = j.tasks ?? [];
        setTasks(list);
        const ids = Array.from(
          new Set(
            list
              .flatMap((t) => [Number(t.author_id) || 0, ...(t.approver_ids ?? [])])
              .filter((id) => id > 0),
          ),
        );
        if (ids.length > 0) {
          const people = await trackerAuthors(initData, ids);
          setAuthors((prev) => {
            const next = { ...prev };
            for (const p of people) {
              const nick = (p.username || "").replace(/^@+/, "");
              next[p.user_id] = p.display_name || (nick ? `@${nick}` : `id ${p.user_id}`);
            }
            for (const id of ids) if (!next[id]) next[id] = `id ${id}`;
            return next;
          });
        }
        const openId = detailRef.current;
        if (openId) {
          const fresh = (j.tasks ?? []).find((t) => t.id === openId);
          if (fresh) setDetail((prev) => (prev ? { ...prev, ...fresh } : prev));
        }
        return j;
      } catch (e) {
        if (!silent) showAlert(e instanceof Error ? e.message : "Couldn't load board");
        return null;
      } finally {
        setLoading(false);
      }
    },
    [initData, showAlert],
  );

  const refreshBoard = async () => {
    if (refreshing) return;
    setRefreshing(true);
    try {
      const j = await load(false, true);
      const n = Number(j?.started) || 0;
      if (n > 0) {
        showAlert(`Picked up ${n} ${plural(n, "task", "tasks", "tasks")}.`);
      }
    } finally {
      setRefreshing(false);
    }
  };

  useEffect(() => {
    void load();
  }, [load]);

  // Живые статусы: агент работает прямо сейчас, и карточка должна это показывать.
  useEffect(() => {
    if (tab !== "board") return undefined;
    const id = window.setInterval(() => {
      if (!document.hidden) void load(true);
    }, 10000);
    return () => window.clearInterval(id);
  }, [tab, load]);

  const pool = useMemo(() => (isQa ? tasks.filter((t) => t.handed_to_qa) : tasks), [tasks, isQa]);
  const columns = isQa ? QA_COLS : DEV_COLS;
  const errCount = pool.filter((t) => t.error).length;
  const runningCount = pool.filter((t) =>
    isQa
      ? (t.qa_column || "todo") === "doing"
      : t.status === "running" || t.status === "reviewing" || t.steps_running,
  ).length;

  /** Выбранный вариант после обсуждения с Лео или единственная формулировка. */
  const activeProposal = useMemo(() => {
    if (!proposal) return null;
    const variants = proposal.variants?.filter((v) => v.task.trim()) ?? [];
    if (variants.length > 0) {
      const v = variants[selectedVariant] ?? variants[0];
      return { ...proposal, title: v.title, task: v.task };
    }
    return proposal;
  }, [proposal, selectedVariant]);

  const createTask = async () => {
    await createTaskWith(prompt.trim());
  };

  const createTaskWith = async (raw: string, opts?: { leo?: boolean; needsApproval?: boolean }) => {
    const text = raw.trim();
    if (!text) {
      showAlert("Describe the task.");
      return;
    }
    if (when === "custom" && !whenAt) {
      showAlert("Pick date and time.");
      return;
    }
    const wantApproval = opts?.leo ? (opts.needsApproval ?? leoNeedsApproval) : needsApproval;
    setBusy(true);
    try {
      const res = await trackerCreate(initData, {
        when: when === "custom" ? whenFromPicker(whenAt) : when,
        prompt: text,
        needs_approval: wantApproval,
        review: withReview,
        test: withTest,
        // Задачу сочинил Лео — пусть на доске и стоит он, а не тот, кто одобрил.
        leo: opts?.leo,
        agent: opts?.leo ? "claude" : agent,
      });
      if (images.length > 0 && res.id) {
        // Картинку трекер принимает только к существующей задаче, поэтому
        // прикладываем сразу после создания.
        for (const img of images) {
          await trackerAttachImage(initData, res.id, {
            data: img.data,
            filename: img.filename,
            mime: img.mime,
          });
        }
      }
      setImages([]);
      setPrompt("");
      if (!opts?.leo) setNeedsApproval(false);
      showAlert(
        wantApproval
          ? `Task on the board in Approve. Admins were notified.`
          : `Task scheduled for ${res.when || "next run"}.`,
      );
      await load(false, true);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't create task");
    } finally {
      setBusy(false);
    }
  };

  // Лео сам придумывает задачу. Чтобы не ходил по кругу, отдаём ему то, что
  // уже на доске, и то, что админ только что отклонил.
  const proposeFromLeo = async (extraReject?: string) => {
    setProposeBusy(true);
    try {
      const busy = [
        ...tasks.slice(0, 12).map((t) => parsePrompt(t.prompt).text.slice(0, 160)),
        ...rejected,
        ...(extraReject ? [extraReject] : []),
      ];
      const j = await leoProposeTask(initData, leoTopic.trim(), busy);
      setProposal(j);
      setSelectedVariant(0);
      setDiscussFeedback("");
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Leo didn't answer");
    } finally {
      setProposeBusy(false);
    }
  };

  const discussWithLeo = async () => {
    const fb = discussFeedback.trim();
    if (!fb || !activeProposal?.task) return;
    setProposeBusy(true);
    try {
      const busy = [
        ...tasks.slice(0, 12).map((t) => parsePrompt(t.prompt).text.slice(0, 160)),
        ...rejected,
      ];
      const j = await leoProposeTask(initData, leoTopic.trim(), busy, {
        feedback: fb,
        previous: { title: activeProposal.title, task: activeProposal.task },
      });
      setProposal(j);
      setSelectedVariant(0);
      setDiscussFeedback("");
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Leo didn't answer");
    } finally {
      setProposeBusy(false);
    }
  };

  // Статус автономного режима читаем при открытии вкладки: он живёт на сервере
  // и меняется сам (Лео отработал очередной спринт), а не только по нажатию.
  const loadAutonomy = useCallback(async () => {
    try {
      setAutonomy(await leoAutonomy(initData, { action: "status" }));
    } catch {
      // Режим доступен только настоящим админам — остальным просто не показываем блок.
      setAutonomy(null);
    }
  }, [initData]);

  // Состояние автодеплоя читаем там же, где автономию: оно живёт на сервере
  // и может измениться из другой сессии админа.
  const loadDeploy = useCallback(async () => {
    try {
      setDeploy((await trackerDeploySettings(initData, "status")).deploy);
    } catch {
      // Настройка только для настоящих админов — остальным блок не показываем.
      setDeploy(null);
    }
  }, [initData]);

  useEffect(() => {
    if (tab === "task") void loadDeploy();
    if (tab === "sprint" && sprintTab === "leo") void loadAutonomy();
  }, [tab, sprintTab, loadAutonomy, loadDeploy]);

  const switchDeploy = async (on: boolean) => {
    setDeployBusy(true);
    try {
      const next = await trackerDeploySettings(initData, on ? "on" : "off");
      setDeploy(next.deploy);
      showAlert(
        on
          ? "Finished tasks will deploy on Railway automatically."
          : "Auto-deploy off: code goes to main; start deploy with the button.",
      );
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Something went wrong");
    } finally {
      setDeployBusy(false);
    }
  };

  const switchAutonomy = async (action: "start" | "stop") => {
    setAutonomyBusy(true);
    try {
      const next = await leoAutonomy(initData, {
        action,
        days: autonomyDays,
        every_hours: 4,
        tasks_per_run: 3,
      });
      setAutonomy(next);
      showAlert(
        action === "start"
          ? `Leo is running solo until ${formatWhen(next.active_until)}.`
          : "Leo no longer creates tasks on his own.",
      );
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Something went wrong");
    } finally {
      setAutonomyBusy(false);
    }
  };

  const approveProposal = async () => {
    if (!activeProposal?.task) return;
    const text = activeProposal.task;
    setProposal(null);
    setSelectedVariant(0);
    setDiscussFeedback("");
    await createTaskWith(text, { leo: true, needsApproval: leoNeedsApproval });
  };

  const openTask = async (task: TrackerTask) => {
    setDetail(task);
    setMoveAt("");
    setEditingPrompt(false);
    setPromptDraft("");
    try {
      const j = await trackerTask(initData, task.id);
      if (j.task) setDetail(j.task);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't open task");
    }
  };

  // Превью вложений: байты идут через свой бэкенд, ссылкой их не показать.
  useEffect(() => {
    const list = detail?.attachments;
    if (!detail || !list || list.length === 0) return;
    let alive = true;
    void (async () => {
      for (const att of list) {
        if (attPreviews[att.id]) continue;
        try {
          const url = await trackerAttachmentGet(initData, detail.id, att.id);
          if (!alive) return;
          setAttPreviews((prev) => ({ ...prev, [att.id]: url }));
        } catch {
          // Не показать картинку — не повод ломать карточку задачи.
        }
      }
    })();
    return () => {
      alive = false;
    };
  }, [detail, initData, attPreviews]);

  // Вставка из буфера (на компьютере — Cmd/Ctrl+V): картинка сразу попадает в
  // редактор — к новой задаче на вкладке «Задача» или к открытой карточке.
  useEffect(() => {
    const onPaste = (e: ClipboardEvent) => {
      const item = Array.from(e.clipboardData?.items ?? []).find((i) =>
        i.type.startsWith("image/"),
      );
      if (!item) return;
      const file = item.getAsFile();
      if (!file) return;
      const target: "new" | number | null = detail ? detail.id : tab === "task" ? "new" : null;
      if (target === null) return;
      e.preventDefault();
      setPasted(file);
      setEditorFor(target);
    };
    window.addEventListener("paste", onPaste);
    return () => window.removeEventListener("paste", onPaste);
  }, [detail, tab]);

  // Действия с фото не должны закрывать карточку: человек смотрит на картинку,
  // а обычный act() возвращает его на доску. Здесь просто перечитываем задачу.
  const actOnDetail = async (fn: () => Promise<unknown>, done: string) => {
    if (!detail) return;
    setBusy(true);
    try {
      await fn();
      showAlert(done);
      const j = await trackerTask(initData, detail.id);
      if (j.task) setDetail(j.task);
      await load(true);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Something went wrong");
    } finally {
      setBusy(false);
    }
  };

  const act = async (fn: () => Promise<unknown>, done: string) => {
    setBusy(true);
    try {
      await fn();
      showAlert(done);
      setDetail(null);
      await load();
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Something went wrong");
    } finally {
      setBusy(false);
    }
  };

  const restartFromBoard = async (task: TrackerTask) => {
    setBusy(true);
    try {
      await trackerRestart(initData, task.id);
      showAlert(canRetryAgent(task) ? "Restarting the agent." : "Task restarted.");
      if (detail?.id === task.id) setDetail(null);
      await load(false, true);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't restart task");
    } finally {
      setBusy(false);
    }
  };

  const deleteFromBoard = async (task: TrackerTask) => {
    setBusy(true);
    try {
      await trackerDelete(initData, task.id);
      showAlert("Task deleted.");
      if (detail?.id === task.id) setDetail(null);
      setDeleteAsk(null);
      await load();
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't delete task");
    } finally {
      setBusy(false);
    }
  };

  const approveFromBoard = async (task: TrackerTask, action: "approve" | "reject") => {
    setBusy(true);
    try {
      const res = await trackerApprove(initData, task.id, action);
      showAlert(action === "approve" ? "Approval recorded." : "Task rejected.");
      if (detail?.id === task.id) {
        if (res.task) setDetail(res.task);
        else setDetail(null);
      }
      await load(false, action === "approve");
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Something went wrong");
    } finally {
      setBusy(false);
    }
  };

  const loadIdeas = async () => {
    const text = hint.trim();
    if (!text) {
      showAlert("Enter a sprint theme.");
      return;
    }
    setSprintBusy("ideas");
    setFeatures([]);
    setFromLeoSprint(false);
    setLeoSprintReply("");
    try {
      const j = await sprintIdeas(initData, text);
      setIdeas(j.ideas ?? []);
      setPickedIdea(String(j.recommended_id || (j.ideas ?? [])[0]?.id || ""));
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't suggest ideas");
    } finally {
      setSprintBusy("");
    }
  };

  // Спринт от Лео: он же придумывает тему и нарезку. Задачи кладём в тот же
  // список, что и обычный генератор, — дальше «На доску (аппрув)».
  const askLeoSprint = async () => {
    setSprintBusy("leo");
    setLeoSprintReply("");
    setFromLeoSprint(false);
    try {
      const j = await leoSprint(initData, hint.trim());
      setLeoSprintReply(j.reply);
      if (j.theme) setHint(j.theme);
      setIdeas([]);
      setFeatures(
        j.tasks.map((t, i) => ({
          title: t.split(/[.!?]/, 1)[0].slice(0, 80) || `Task ${i + 1}`,
          prompt: t,
          sprint: 1,
          _on: true,
        })),
      );
      setFromLeoSprint(true);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Leo didn't answer");
    } finally {
      setSprintBusy("");
    }
  };

  const buildPlan = async () => {
    const idea = ideas.find((i) => String(i.id) === pickedIdea) ?? ideas[0];
    if (!idea) {
      showAlert("Pick an idea first.");
      return;
    }
    setSprintBusy("plan");
    setFromLeoSprint(false);
    setLeoSprintReply("");
    try {
      const j = await sprintGenerate(initData, {
        hint: hint.trim(),
        idea,
        sprint_count: sprintCount,
        tasks_per_sprint: tasksPerSprint,
      });
      setFeatures((j.features ?? []).map((f) => ({ ...f, _on: true })));
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't build plan");
    } finally {
      setSprintBusy("");
    }
  };

  const applyPlan = async () => {
    const picked = features.filter((f) => f._on);
    if (!picked.length) {
      showAlert("Select at least one task.");
      return;
    }
    setSprintBusy("apply");
    try {
      const j = await sprintApply(initData, {
        features: picked.map(({ _on, ...rest }) => rest),
        sprint_count: sprintCount,
        tasks_per_sprint: tasksPerSprint,
        ...(fromLeoSprint ? { leo: true, needs_approval: true } : {}),
      });
      showAlert(
        fromLeoSprint
          ? `Created ${j.created ?? picked.length} task(s). Needs two admin approvals — Approve column.`
          : `Created ${j.created ?? picked.length} task(s).`,
      );
      setFeatures([]);
      setIdeas([]);
      setHint("");
      setFromLeoSprint(false);
      setLeoSprintReply("");
      setTab("board");
      await load();
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't create tasks");
    } finally {
      setSprintBusy("");
    }
  };

  return (
    <div className="tracker" ref={rootRef}>
      <div className="tracker__tabs" role="tablist">
        <button
          type="button"
          role="tab"
          aria-selected={tab === "board"}
          className={tab === "board" ? "on" : ""}
          onClick={() => setTab("board")}
        >
          Board
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === "task"}
          className={tab === "task" ? "on" : ""}
          onClick={() => setTab("task")}
        >
          Task
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === "sprint"}
          className={tab === "sprint" ? "on" : ""}
          onClick={() => setTab("sprint")}
        >
          Sprints
        </button>
      </div>

      {tab === "board" ? (
        <>
          <div className="tracker__toolbar">
            <div className="tracker__roles">
              <button
                type="button"
                className={role === "developer" ? "on" : ""}
                onClick={() => setRole("developer")}
              >
                Developer
              </button>
              <button
                type="button"
                className={role === "tester" ? "on" : ""}
                onClick={() => setRole("tester")}
              >
                Tester
              </button>
            </div>
            <button
              type="button"
              className={`tracker__refresh${refreshing ? " is-refreshing" : ""}`}
              disabled={refreshing}
              aria-busy={refreshing}
              onClick={() => void refreshBoard()}
            >
              {refreshing ? "Refreshing…" : "Refresh"}
            </button>
          </div>

          <div className="tracker__summary">
            <span className="tracker-stat">
              <strong>{pool.length}</strong> {plural(pool.length, "task", "tasks", "tasks")}
            </span>
            {runningCount > 0 ? (
              <span className="tracker-stat tracker-stat--active">
                <strong>{runningCount}</strong> in progress
              </span>
            ) : null}
            {errCount > 0 ? (
              <span className="tracker-stat tracker-stat--err">
                <strong>{errCount}</strong> {plural(errCount, "error", "errors", "errors")}
              </span>
            ) : null}
          </div>

          <p className="tracker__hint">
            {isQa
              ? "After handoff you can accept, send back, or ask Leo for a checklist."
              : "Pending → in progress (agent writes code) → Review (branch check) → test (build & tests) → deploy → done + notification. Code ships when you deploy."}
          </p>

          {loading ? (
            <p className="tracker__muted">Loading board…</p>
          ) : (
            <div className="tracker__cols">
              {columns.map((col) => {
                let items = pool.filter((t) =>
                  isQa ? (t.qa_column || "todo") === col.key : boardDevColumn(t) === col.key,
                );
                if (!isQa && col.key === "todo") {
                  items = sortTodoColumnFifo(items);
                }
                return (
                  <div className="tracker-col" data-col={col.key} key={col.key}>
                    <div className="tracker-col__head">
                      <span className="tracker-col__title">{col.title}</span>
                      <span className="tracker-col__count">{items.length}</span>
                    </div>
                    <div className="tracker-col__list">
                      {items.length === 0 ? (
                        <div className="tracker-empty">
                          <span className="tracker-empty__icon" aria-hidden>
                            ∅
                          </span>
                          empty
                        </div>
                      ) : (
                        items.map((t) => (
                          <TaskCard
                            key={t.id}
                            task={t}
                            isQa={isQa}
                            author={authorLabel(t, authors)}
                            avatar={authorAvatar(t, initData)}
                            approvers={taskApprovers(t, authors, initData)}
                            onOpen={() => void openTask(t)}
                            onApprove={
                              canApproveOnBoard(t, isQa)
                                ? () => void approveFromBoard(t, "approve")
                                : undefined
                            }
                            onReject={
                              canApproveOnBoard(t, isQa)
                                ? () => void approveFromBoard(t, "reject")
                                : undefined
                            }
                            onRestart={canReturnToWork(t) ? () => void restartFromBoard(t) : undefined}
                            onDelete={t.can_delete ? () => setDeleteAsk(t) : undefined}
                          />
                        ))
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </>
      ) : tab === "task" ? (
        <div className="tracker__task">
          <p className="tracker__hint">
            Describe what to do, when to run it, and attach a screenshot if helpful.
          </p>

          <div className="tracker__toggles">
            {deploy ? (
              <label className="tracker-feat">
                <input
                  type="checkbox"
                  checked={deploy.enabled}
                  disabled={deployBusy}
                  onChange={(e) => void switchDeploy(e.target.checked)}
                />
                <span>
                  <b>Auto-deploy</b>
                </span>
              </label>
            ) : null}
            <label className="tracker-feat">
              <input
                type="checkbox"
                checked={needsApproval}
                onChange={(e) => setNeedsApproval(e.target.checked)}
              />
              <span>
                <b>Team approval</b>
              </span>
            </label>
            <label className="tracker-feat">
              <input
                type="checkbox"
                checked={withReview}
                onChange={(e) => setWithReview(e.target.checked)}
              />
              <span>
                <b>Review</b>
              </span>
            </label>
            <label className="tracker-feat">
              <input
                type="checkbox"
                checked={withTest}
                onChange={(e) => setWithTest(e.target.checked)}
              />
              <span>
                <b>Test</b>
              </span>
            </label>
          </div>

          <div className="tracker__new">
              <textarea
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                placeholder="What to do? e.g. admin export of weekly workouts"
              />
              {images.length > 0 ? (
                <div className="tracker__previews">
                  {images.map((img, i) => (
                    <div key={`${img.filename}-${i}`} className="tracker__preview">
                      <img src={img.preview} alt="" />
                      <button
                        type="button"
                        onClick={() => setImages((prev) => prev.filter((_, j) => j !== i))}
                        aria-label="Remove image"
                      >
                        ✕
                      </button>
                    </div>
                  ))}
                </div>
              ) : null}
              <div className="tracker__new-row">
                <select
                  value={agent}
                  aria-label="Assignee"
                  onChange={(e) => setAgent(trackerAgentOf(e.target.value))}
                >
                  {AGENT_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {opt.label}
                    </option>
                  ))}
                </select>
                <select value={when} onChange={(e) => setWhen(e.target.value)}>
                  {WHEN_PRESETS.map((p) => (
                    <option key={p.value} value={p.value}>
                      {p.label}
                    </option>
                  ))}
                </select>
                {when === "custom" ? (
                  <input
                    type="datetime-local"
                    className="tracker__at"
                    value={whenAt}
                    onChange={(e) => setWhenAt(e.target.value)}
                  />
                ) : null}
                <button type="button" className="tracker__attach" onClick={() => setEditorFor("new")}>
                  {images.length > 0 ? "🖼 More photos" : "🖼 Image"}
                </button>
                <button type="button" className="tracker__primary" disabled={busy} onClick={() => void createTask()}>
                  Create task
                </button>
              </div>
            </div>
        </div>
      ) : (
        <div className="tracker__sprint">
          <div className="tracker__roles tracker__subtabs" role="tablist">
            <button
              type="button"
              role="tab"
              aria-selected={sprintTab === "load"}
              className={sprintTab === "load" ? "on" : ""}
              onClick={() => setSprintTab("load")}
            >
              Load sprints
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={sprintTab === "leo"}
              className={sprintTab === "leo" ? "on" : ""}
              onClick={() => setSprintTab("leo")}
            >
              Leo features
            </button>
          </div>
          {sprintTab === "load" ? (
            <>
              <p className="tracker__hint">
                Describe a sprint theme — Leo will suggest ideas and slice tasks. You can add them to the board next.
              </p>
              <textarea
                value={hint}
                onChange={(e) => setHint(e.target.value)}
                placeholder="e.g. reduce pack churn"
              />
              <div className="tracker__new-row">
                <label className="tracker__num">
                  Sprints
                  <input
                    type="number"
                    min={1}
                    max={8}
                    value={sprintCount}
                    onChange={(e) => setSprintCount(Math.min(8, Math.max(1, Number(e.target.value) || 1)))}
                  />
                </label>
                <label className="tracker__num">
                  Tasks per sprint
                  <input
                    type="number"
                    min={1}
                    max={12}
                    value={tasksPerSprint}
                    onChange={(e) => setTasksPerSprint(Math.min(12, Math.max(1, Number(e.target.value) || 1)))}
                  />
                </label>
                <button
                  type="button"
                  className="tracker__primary"
                  disabled={sprintBusy !== ""}
                  onClick={() => void loadIdeas()}
                >
                  {sprintBusy === "ideas" ? "Thinking…" : "Suggest ideas"}
                </button>
                <button
                  type="button"
                  className="tracker__attach"
                  disabled={sprintBusy !== ""}
                  onClick={() => void askLeoSprint()}
                >
                  {sprintBusy === "leo" ? "Leo is thinking…" : "🐆 Sprint from Leo"}
                </button>
              </div>
              {leoSprintReply ? <p className="tracker__leo-reply">🐆 {leoSprintReply}</p> : null}

              {ideas.length > 0 ? (
                <div className="tracker__ideas">
                  <h3 className="tracker__subtitle">Ideas</h3>
                  {ideas.map((idea, i) => {
                    const id = String(idea.id ?? i);
                    const on = pickedIdea === id;
                    return (
                      <button
                        type="button"
                        key={id}
                        className={`tracker-idea${on ? " on" : ""}`}
                        onClick={() => setPickedIdea(id)}
                      >
                        <b>{String(idea.title || idea.name || `Idea ${i + 1}`)}</b>
                        <small>{String(idea.summary || idea.description || "")}</small>
                      </button>
                    );
                  })}
                  <button
                    type="button"
                    className="tracker__primary tracker__primary--block"
                    disabled={sprintBusy !== ""}
                    onClick={() => void buildPlan()}
                  >
                    {sprintBusy === "plan" ? "Building plan…" : "Build plan & tasks"}
                  </button>
                </div>
              ) : null}

              {features.length > 0 ? (
                <div className="tracker__feats">
                  <h3 className="tracker__subtitle">Sprint tasks</h3>
                  {fromLeoSprint ? (
                    <p className="tracker__hint">
                      Sprint from Leo — tasks land in Approve; they start after two admin approvals.
                    </p>
                  ) : null}
                  {features.map((f, i) => (
                    <label className="tracker-feat" key={`${f.title}-${i}`}>
                      <input
                        type="checkbox"
                        checked={f._on}
                        onChange={(e) =>
                          setFeatures((prev) =>
                            prev.map((item, idx) => (idx === i ? { ...item, _on: e.target.checked } : item)),
                          )
                        }
                      />
                      <span>
                        <b>
                          {f.sprint ? <span className="tracker-badge tracker-badge--sprint">Sprint {f.sprint}</span> : null}
                          {String(f.title || "")}
                        </b>
                        <small>{String(f.prompt || "").slice(0, 220)}</small>
                      </span>
                    </label>
                  ))}
                  <button
                    type="button"
                    className="tracker__primary tracker__primary--block"
                    disabled={sprintBusy !== ""}
                    onClick={() => void applyPlan()}
                  >
                    {sprintBusy === "apply" ? "Creating…" : fromLeoSprint ? "To board (approve)" : "Add tasks to plan"}
                  </button>
                </div>
              ) : null}
            </>
          ) : (
            <>
              {autonomy ? (
                <div className="tracker__leo tracker__auto">
                  <div className="tracker__leo-head">
                    <span aria-hidden>🤖</span>
                    <b>Leo runs the product solo</b>
                    <span className={`tracker__auto-state${autonomy.active ? " is-on" : ""}`}>
                      {autonomy.active ? "on" : "off"}
                    </span>
                  </div>
                  <p className="tracker__hint">
                    Every {autonomy.every_hours}h Leo drafts a sprint in his voice and adds tasks to the board — {autonomy.tasks_per_run} per run. Approve column first, then work. His avatar on the cards.
                  </p>
                  {autonomy.active ? (
                    <p className="tracker__auto-facts">
                      Until {formatWhen(autonomy.active_until)} · next sprint{" "}
                      {formatWhen(autonomy.next_run_at)}
                    </p>
                  ) : null}
                  {autonomy.last_note ? (
                    <p className="tracker__auto-facts">
                      Last run ({formatWhen(autonomy.last_run_at)}): {autonomy.last_note}
                    </p>
                  ) : null}
                  <div className="tracker__new-row">
                    <select
                      value={autonomyDays}
                      onChange={(e) => setAutonomyDays(Number(e.target.value))}
                      aria-label="How many days Leo runs solo"
                    >
                      {[1, 2, 3, 5, 7, 14]
                        .filter((d) => d <= (autonomy.max_days || 14))
                        .map((d) => (
                          <option key={d} value={d}>
                            {d} {plural(d, "day", "days", "days")}
                          </option>
                        ))}
                    </select>
                    <button
                      type="button"
                      className="tracker__primary"
                      disabled={autonomyBusy}
                      onClick={() => void switchAutonomy("start")}
                    >
                      {autonomy.active ? "Extend" : "Let Leo run solo"}
                    </button>
                    {autonomy.active ? (
                      <button
                        type="button"
                        className="tracker__attach"
                        disabled={autonomyBusy}
                        onClick={() => void switchAutonomy("stop")}
                      >
                        Stop
                      </button>
                    ) : null}
                  </div>
                </div>
              ) : null}

              <div className="tracker__leo">
                <div className="tracker__leo-head">
                  <span aria-hidden>🐆</span>
                  <b>Task from Leo</b>
                </div>
                <p className="tracker__hint">
                  Enter a theme — or leave it empty for Leo to decide. You can discuss details, pick a wording, then confirm the task on the board.
                </p>
                <div className="tracker__new-row">
                  <input
                    value={leoTopic}
                    onChange={(e) => setLeoTopic(e.target.value)}
                    placeholder="Theme (optional): e.g. retain newcomers"
                  />
                </div>
                {proposal ? (
                  <>
                    <p className="tracker__leo-reply">{proposal.reply}</p>
                    {proposal.variants && proposal.variants.length > 1 ? (
                      <div className="tracker__ideas">
                        <h3 className="tracker__subtitle">Options</h3>
                        {proposal.variants.map((v, i) => {
                          const on = selectedVariant === i;
                          return (
                            <button
                              type="button"
                              key={`${v.title}-${i}`}
                              className={`tracker-idea${on ? " on" : ""}`}
                              onClick={() => setSelectedVariant(i)}
                            >
                              <b>{v.title || `Option ${i + 1}`}</b>
                              <small>{v.task.slice(0, 220)}</small>
                            </button>
                          );
                        })}
                      </div>
                    ) : (
                      <>
                        {activeProposal?.title ? <p className="tracker__leo-title">{activeProposal.title}</p> : null}
                        <p className="tracker__leo-task">{activeProposal?.task}</p>
                      </>
                    )}
                    <div className="tracker__leo-discuss">
                      <textarea
                        value={discussFeedback}
                        onChange={(e) => setDiscussFeedback(e.target.value)}
                        placeholder="Ask Leo to clarify or rephrase…"
                        rows={2}
                      />
                      <button
                        type="button"
                        className="tracker__attach"
                        disabled={proposeBusy || !discussFeedback.trim()}
                        onClick={() => void discussWithLeo()}
                      >
                        {proposeBusy ? "Leo is replying…" : "Discuss with Leo"}
                      </button>
                    </div>
                    <div className="tracker__new-row">
                      <select value={when} onChange={(e) => setWhen(e.target.value)}>
                        {WHEN_PRESETS.map((p) => (
                          <option key={p.value} value={p.value}>
                            {p.label}
                          </option>
                        ))}
                      </select>
                      {when === "custom" ? (
                        <input
                          type="datetime-local"
                          className="tracker__at"
                          value={whenAt}
                          onChange={(e) => setWhenAt(e.target.value)}
                        />
                      ) : null}
                      <label className="tracker-feat">
                        <input
                          type="checkbox"
                          checked={leoNeedsApproval}
                          onChange={(e) => setLeoNeedsApproval(e.target.checked)}
                        />
                        <span>
                          <b>Needs approval from all admins</b>
                          <small>Two approvals (including you) in Telegram or here — then the task ships</small>
                        </span>
                      </label>
                      <button
                        type="button"
                        className="tracker__primary"
                        disabled={busy || proposeBusy || !activeProposal?.task}
                        onClick={() => void approveProposal()}
                      >
                        {leoNeedsApproval ? "Confirm (approve)" : "Confirm on board"}
                      </button>
                      <button
                        type="button"
                        className="tracker__attach"
                        disabled={proposeBusy}
                        onClick={() => {
                          const reject = activeProposal?.title || activeProposal?.task || "";
                          setRejected((prev) => [...prev, reject].slice(-10));
                          setProposal(null);
                          setSelectedVariant(0);
                          setDiscussFeedback("");
                          void proposeFromLeo(reject);
                        }}
                      >
                        Not that — try another
                      </button>
                    </div>
                  </>
                ) : (
                  <button
                    type="button"
                    className="tracker__attach"
                    disabled={proposeBusy}
                    onClick={() => void proposeFromLeo()}
                  >
                    {proposeBusy ? "Leo is brainstorming…" : "Let Leo brainstorm"}
                  </button>
                )}
              </div>
            </>
          )}
        </div>
      )}

      {deleteAsk ? (
        <div className="tracker-confirm" role="dialog" aria-modal="true" aria-labelledby="tracker-delete-title">
          <div className="tracker-confirm__box">
            <h3 id="tracker-delete-title">Delete task?</h3>
            <p>
              <strong>#{taskNo(deleteAsk)}</strong>{" "}
              {parsePrompt(deleteAsk.prompt).text.slice(0, 160) || deleteAsk.prompt}
              {parsePrompt(deleteAsk.prompt).text.length > 160 ? "…" : ""}
            </p>
            <p className="tracker-confirm__hint">The card and photos will be removed from the board with no way to restore.</p>
            <div className="tracker-confirm__actions">
              <button type="button" disabled={busy} onClick={() => setDeleteAsk(null)}>
                Cancel
              </button>
              <button
                type="button"
                className="tracker-confirm__danger"
                disabled={busy}
                onClick={() => void deleteFromBoard(deleteAsk)}
              >
                Delete
              </button>
            </div>
          </div>
        </div>
      ) : null}

      {detail ? (
        <div className="tracker-modal" role="dialog" aria-modal="true">
          <div className="tracker-modal__box">
            <div className="tracker-modal__head">
              <span className="tracker-card__id">#{taskNo(detail)}</span>
              <span className="tracker-modal__status">
                {detail.status_icon} {detail.status_label}
              </span>
              <button
                type="button"
                className="tracker-modal__close"
                onClick={() => {
                  setDetail(null);
                  setEditingPrompt(false);
                  setPromptDraft("");
                }}
                aria-label="Close"
              >
                ✕
              </button>
            </div>
            <div className="tracker-card__author tracker-modal__author">
              {authorAvatar(detail, initData) ? (
                <img src={authorAvatar(detail, initData)} alt="" loading="lazy" />
              ) : (
                <span className="tracker-card__author-dot">🤖</span>
              )}
              <span>Created by: {authorLabel(detail, authors)}</span>
            </div>
            {detail.needs_approval ? (
              <div className="tracker-modal__approvers">
                <span>
                  👍 {detail.approvals_count ?? 0}/{detail.approvals_needed ?? 2}
                </span>
                <ApproverAvatars approvers={taskApprovers(detail, authors, initData)} />
                <span className="tracker-modal__approvers-names">
                  {(detail.approver_ids ?? []).length > 0
                    ? taskApprovers(detail, authors, initData)
                        .map((a) => a.name)
                        .join(", ")
                    : "no approvals yet"}
                </span>
              </div>
            ) : null}
            {canChangeAgent(detail) ? (
              <label className="tracker__agent">
                Assignee
                <select
                  value={trackerAgentOf(detail.agent)}
                  disabled={busy}
                  onChange={(e) => {
                    const next = trackerAgentOf(e.target.value);
                    void actOnDetail(async () => {
                      const res = await trackerPrompt(
                        initData,
                        detail.id,
                        detail.prompt,
                        next,
                      );
                      if (res.task) setDetail(res.task);
                    }, `Assignee: ${trackerAgentLabel(next)}.`);
                  }}
                >
                  {AGENT_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {opt.label}
                    </option>
                  ))}
                </select>
              </label>
            ) : (
              <p className="tracker__hint">Assignee: {trackerAgentLabel(detail.agent)}</p>
            )}
            {detail.status !== "done" && detail.status !== "canceled" ? (
              <div className="tracker__toggles">
                <label className="tracker-feat">
                  <input
                    type="checkbox"
                    checked={detail.review !== false}
                    disabled={busy}
                    onChange={(e) => {
                      const review = e.target.checked;
                      void actOnDetail(async () => {
                        const res = await trackerPrompt(initData, detail.id, detail.prompt, undefined, { review });
                        if (res.task) setDetail(res.task);
                      }, review ? "Review on." : "Review off.");
                    }}
                  />
                  <span>
                    <b>Review</b>
                  </span>
                </label>
                <label className="tracker-feat">
                  <input
                    type="checkbox"
                    checked={detail.test !== false}
                    disabled={busy}
                    onChange={(e) => {
                      const test = e.target.checked;
                      void actOnDetail(async () => {
                        const res = await trackerPrompt(initData, detail.id, detail.prompt, undefined, { test });
                        if (res.task) setDetail(res.task);
                      }, test ? "Test on." : "Test off.");
                    }}
                  />
                  <span>
                    <b>Test</b>
                  </span>
                </label>
              </div>
            ) : null}
            <div className="tracker-modal__prompt-block">
              {editingPrompt ? (
                <>
                  <textarea
                    className="tracker-modal__prompt-edit"
                    value={promptDraft}
                    onChange={(e) => setPromptDraft(e.target.value)}
                    disabled={busy}
                    rows={4}
                  />
                  <div className="tracker-modal__prompt-actions">
                    <button
                      type="button"
                      className="tracker__primary"
                      disabled={busy || !promptDraft.trim()}
                      onClick={() =>
                        void actOnDetail(async () => {
                          const next = buildPromptText(detail, promptDraft);
                          if (!next) throw new Error("Describe the task");
                          const res = await trackerPrompt(initData, detail.id, next);
                          if (res.task) setDetail(res.task);
                          setEditingPrompt(false);
                        }, "Wording updated.")
                      }
                    >
                      Save
                    </button>
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() => {
                        setEditingPrompt(false);
                        setPromptDraft("");
                      }}
                    >
                      Cancel
                    </button>
                  </div>
                </>
              ) : (
                <>
                  <p className="tracker-modal__prompt">{parsePrompt(detail.prompt).text}</p>
                  {canEditPrompt(detail) ? (
                    <button
                      type="button"
                      className="tracker-modal__prompt-edit-btn"
                      disabled={busy}
                      onClick={() => {
                        setPromptDraft(parsePrompt(detail.prompt).text);
                        setEditingPrompt(true);
                      }}
                    >
                      Edit text
                    </button>
                  ) : null}
                </>
              )}
            </div>
            {/* Приложенные фото: их можно рассмотреть и заменить. */}
            <div className="tracker-modal__atts">
              {(detail.attachments ?? []).map((att) => (
                <div key={att.id} className="tracker-att">
                  {attPreviews[att.id] ? (
                    <button
                      type="button"
                      className="tracker-att__shot"
                      onClick={() => setZoomed(attPreviews[att.id])}
                      title="Open fullscreen"
                    >
                      <img src={attPreviews[att.id]} alt={att.name} />
                    </button>
                  ) : (
                    <div className="tracker-att__shot tracker-att__shot--wait">loading…</div>
                  )}
                  <div className="tracker-att__row">
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() => {
                        setReplacing(att.id);
                        setPasted(null);
                        setEditorFor(detail.id);
                      }}
                    >
                      Replace
                    </button>
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() =>
                        void actOnDetail(
                          () => trackerAttachmentDelete(initData, detail.id, att.id),
                          "Photo removed.",
                        )
                      }
                    >
                      Remove
                    </button>
                  </div>
                </div>
              ))}
              <button
                type="button"
                className="tracker-att__add"
                disabled={busy}
                onClick={() => {
                  setReplacing(null);
                  setPasted(null);
                  setEditorFor(detail.id);
                }}
              >
                {(detail.attachments ?? []).length > 0 ? "More photos" : "Attach photo"}
                <span className="tracker-att__hint"> · or paste from clipboard</span>
              </button>
            </div>
            <div className="tracker-card__meta">
              {metaParts(detail, isQa).map((p) => (
                <span key={p}>{p}</span>
              ))}
            </div>
            {detail.steps && detail.steps.length > 0 ? (
              <pre className="tracker-modal__log">{detail.steps.slice(-40).join("\n")}</pre>
            ) : null}
            {detail.done_summary ? (
              <pre className="tracker-modal__log tracker-modal__log--summary">{detail.done_summary}</pre>
            ) : null}
            {detail.result ? <pre className="tracker-modal__log">{detail.result}</pre> : null}
            {detail.error ? <pre className="tracker-modal__log tracker-modal__log--err">{detail.error}</pre> : null}
            {canShipTask(detail) ? (
              <div className="tracker-modal__result">
                <span className="tracker-modal__commit">
                  Code lives in this repo. To ship to the server, use the deploy action.
                </span>
                <div className="tracker-modal__result-row">
                  <button
                    type="button"
                    className="tracker__primary"
                    disabled={busy}
                    onClick={() =>
                      void actOnDetail(async () => {
                        const res = await trackerShip(initData, detail.id);
                        if (res.skipped) throw new Error("Too early to mark done — move the card further on the board.");
                        if (res.deployed || res.pushed) return;
                        if (res.error) throw new Error(res.error);
                      }, "Marked done. Deploy to server with the deploy action.")
                    }
                  >
                    Mark ready to ship
                  </button>
                </div>
              </div>
            ) : null}

            {/* Перенос запуска. Он же возвращает завершённую или отменённую
                задачу в «Ожидает»: время в будущем — и она снова в очереди.
                Выполняющуюся двигать нельзя, сначала останови. */}
            {detail.status !== "running" && detail.status !== "reviewing" ? (
              <div className="tracker-modal__move">
                <label>
                  {canReturnToWork(detail) ? "Back to pending on" : "Schedule"}
                  <input type="datetime-local" value={moveAt} onChange={(e) => setMoveAt(e.target.value)} />
                </label>
                <button
                  type="button"
                  disabled={busy || !moveAt}
                  onClick={() =>
                    void act(
                      () => trackerReschedule(initData, detail.id, moveAt.replace("T", " ")),
                      "Run time updated.",
                    )
                  }
                >
                  Reschedule
                </button>
              </div>
            ) : null}

            <div className="tracker-modal__actions">
              <button type="button" disabled={busy} onClick={() => setEditorFor(detail.id)}>
                🖼 Image
              </button>
              {!isQa && detail.dev_column === "approve" && detail.needs_approval ? (
                <>
                  <button
                    type="button"
                    className="tracker-modal__accent"
                    disabled={busy}
                    onClick={() =>
                      void actOnDetail(
                        () => trackerApprove(initData, detail.id, "approve"),
                        "Approval recorded.",
                      )
                    }
                  >
                    Approve ({detail.approvals_count ?? 0}/{detail.approvals_needed ?? 2})
                  </button>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() =>
                      void actOnDetail(
                        () => trackerApprove(initData, detail.id, "reject"),
                        "Task rejected.",
                      )
                    }
                  >
                    Reject
                  </button>
                </>
              ) : null}
              {!isQa && detail.dev_column !== "approve" && NEXT_COL[detail.dev_column || "todo"] ? (
                <button
                  type="button"
                  className="tracker-modal__accent"
                  disabled={busy}
                  onClick={() =>
                    void actOnDetail(
                      () => trackerMove(initData, detail.id, NEXT_COL[detail.dev_column || "todo"].column),
                      `Moved: ${NEXT_COL[detail.dev_column || "todo"].label}.`,
                    )
                  }
                >
                  {NEXT_COL[detail.dev_column || "todo"].label}
                </button>
              ) : null}
              {!isQa && detail.dev_column === "review" ? (
                <button
                  type="button"
                  className="tracker-modal__accent"
                  disabled={busy}
                  onClick={() =>
                    void actOnDetail(() => trackerReview(initData, detail.id), "Review started.")
                  }
                >
                  Start review
                </button>
              ) : null}
              {!isQa && detail.dev_column === "test" ? (
                <button
                  type="button"
                  className="tracker-modal__accent"
                  disabled={busy}
                  onClick={() =>
                    void actOnDetail(() => trackerAutoTest(initData, detail.id), "Test started.")
                  }
                >
                  Run test
                </button>
              ) : null}
              {!isQa && (detail.dev_column === "deploy" || detail.dev_column === "done") ? (
                <button
                  type="button"
                  className="tracker-modal__accent"
                  disabled={busy}
                  onClick={() =>
                    void actOnDetail(async () => {
                      const res = await trackerDeployNow(initData, detail.id);
                      if (res.busy) throw new Error("Deploy for this task is already running.");
                    }, "Railway is building prod. Watch progress in the task steps.")
                  }
                >
                  🚀 Deploy
                </button>
              ) : null}
              {canReturnToWork(detail) ? (
                <button
                  type="button"
                  className="tracker-modal__accent"
                  disabled={busy}
                  onClick={() =>
                    void actOnDetail(
                      () => trackerRestart(initData, detail.id),
                      canRetryAgent(detail) ? "Restarting the agent." : "Task restarted.",
                    )
                  }
                >
                  {canRetryAgent(detail) ? "Run again" : "Restart"}
                </button>
              ) : null}
              {isQa && detail.handed_to_qa ? (
                <>
                  {(detail.qa_column || "todo") === "todo" ? (
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() => void act(() => trackerQa(initData, detail.id, "start"), "Picked up for testing.")}
                    >
                      Pick up for QA
                    </button>
                  ) : null}
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void act(() => trackerQa(initData, detail.id, "pass"), "Task accepted. Deploy with the deploy action.")}
                  >
                    Accept
                  </button>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void act(() => trackerQa(initData, detail.id, "fail"), "Sent back to developer.")}
                  >
                    Send back
                  </button>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void act(() => trackerAutoQa(initData, detail.id), "Leo wrote a checklist.")}
                  >
                    AI test
                  </button>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void act(() => trackerAutoTest(initData, detail.id), "Test started.")}
                  >
                    Run test
                  </button>
                </>
              ) : null}
              {detail.active ? (
                <button
                  type="button"
                  disabled={busy}
                  onClick={() =>
                    void act(
                      () => trackerCancel(initData, detail.id),
                      detail.status === "running" || detail.status === "reviewing"
                        ? "Task stopped."
                        : "Task canceled.",
                    )
                  }
                >
                  {detail.status === "running" || detail.status === "reviewing" ? "Stop" : "Cancel"}
                </button>
              ) : null}
              {detail.can_delete ? (
                <button
                  type="button"
                  className="tracker-modal__danger"
                  disabled={busy}
                  onClick={() => void act(() => trackerDelete(initData, detail.id), "Task deleted.")}
                >
                  Delete
                </button>
              ) : null}
            </div>
          </div>
        </div>
      ) : null}

      {editorFor !== null ? (
        <TaskImageEditor
          initialFile={pasted}
          allowMultiple={editorFor === "new"}
          onDoneMany={(batch) => {
            if (editorFor !== "new") return;
            setEditorFor(null);
            setPasted(null);
            setReplacing(null);
            setImages((prev) => [...prev, ...batch]);
          }}
          onCancel={() => {
            setEditorFor(null);
            setPasted(null);
            setReplacing(null);
          }}
          onDone={(img) => {
            const target = editorFor;
            const oldAtt = replacing;
            setEditorFor(null);
            setPasted(null);
            setReplacing(null);
            if (target === "new") {
              setImages((prev) => [...prev, img]);
              return;
            }
            if (typeof target === "number") {
              void actOnDetail(async () => {
                await trackerAttachImage(initData, target, {
                  data: img.data,
                  filename: img.filename,
                  mime: img.mime,
                });
                // Замена — это «приложить новое, снять старое» именно в таком
                // порядке: если приложить не вышло, задача не останется без фото.
                if (oldAtt) await trackerAttachmentDelete(initData, target, oldAtt);
              }, oldAtt ? "Photo replaced." : "Image attached.");
            }
          }}
        />
      ) : null}

      {zoomed ? (
        <button type="button" className="tracker-zoom" onClick={() => setZoomed(null)} aria-label="Close">
          <img src={zoomed} alt="" />
        </button>
      ) : null}
    </div>
  );
}

function TaskCard({
  task,
  isQa,
  author,
  avatar,
  approvers,
  onOpen,
  onApprove,
  onReject,
  onRestart,
  onDelete,
}: {
  task: TrackerTask;
  isQa: boolean;
  author: string;
  avatar: string;
  approvers: Approver[];
  onOpen: () => void;
  onApprove?: () => void;
  onReject?: () => void;
  onRestart?: () => void;
  onDelete?: () => void;
}) {
  const parsed = parsePrompt(task.prompt);
  const statusText = isQa
    ? `${task.qa_icon || "🧪"} ${task.qa_label || "QA"}`
    : `${task.status_icon || ""} ${task.status_label || ""}`;
  const steps = Array.isArray(task.steps) ? task.steps : [];
  const live = (task.live_step || "").trim() || (steps.length ? String(steps[steps.length - 1] || "").trim() : "");
  const showLive =
    task.status === "running" ||
    task.status === "reviewing" ||
    task.steps_running ||
    task.dev_column === "test" ||
    task.dev_column === "deploy";
  const resultPreview = clipCardText(task.done_summary || task.result || "");
  const meta = metaParts(task, isQa);
  return (
    <div
      className={cardClasses(task, isQa)}
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
    >
      <div className="tracker-card__head">
        <span className="tracker-card__id">#{taskNo(task)}</span>
        <span className="tracker-card__status">{statusText}</span>
        {onRestart || onDelete ? (
          <span className="tracker-card__actions">
            {onRestart ? (
              <button
                type="button"
                className="tracker-card__restart"
                aria-label="Restart task"
                title="Restart"
                onClick={(e) => {
                  e.stopPropagation();
                  onRestart();
                }}
              >
                ↻
              </button>
            ) : null}
            {onDelete ? (
              <button
                type="button"
                className="tracker-card__delete"
                aria-label="Delete task"
                title="Delete"
                onClick={(e) => {
                  e.stopPropagation();
                  onDelete();
                }}
              >
                🗑
              </button>
            ) : null}
          </span>
        ) : null}
      </div>
      <div className="tracker-card__author">
        {avatar ? <img src={avatar} alt="" loading="lazy" /> : <span className="tracker-card__author-dot">🤖</span>}
        <span>{author}</span>
      </div>
      <div className="tracker-card__text">{parsed.text || task.prompt}</div>
      {parsed.sprint || isLeoTask(task) || task.kind === "deploy_fix" || task.manual_qa || task.fast_track || task.has_attachments || task.needs_approval ? (
        <div className="tracker-card__badges">
          {parsed.sprint ? <span className="tracker-badge tracker-badge--sprint">Sprint {parsed.sprint}</span> : null}
          {isLeoTask(task) ? <span className="tracker-badge tracker-badge--leo">🐆 Leo</span> : null}
          {task.needs_approval ? (
            <span className="tracker-badge tracker-badge--approve">
              👍 {task.approvals_count ?? 0}/{task.approvals_needed ?? 2}
              <ApproverAvatars approvers={approvers} />
            </span>
          ) : null}
          {task.kind === "deploy_fix" ? (
            <span className="tracker-badge tracker-badge--deploy">auto deploy fix</span>
          ) : null}
          {task.manual_qa ? <span className="tracker-badge tracker-badge--qa">manual QA</span> : null}
          {task.fast_track ? <span className="tracker-badge tracker-badge--deploy">⚡ fast track</span> : null}
          {task.has_attachments ? (
            <span className="tracker-badge tracker-badge--photo">📷 {task.attachments_count || 1}</span>
          ) : null}
        </div>
      ) : null}
      {resultPreview ? (
        <div className="tracker-card__live tracker-card__live--result">{resultPreview}</div>
      ) : showLive ? (
        <div className="tracker-card__live">{live || "⏳ running…"}</div>
      ) : null}
      {onApprove || onReject ? (
        <div className="tracker-card__approve">
          {onApprove ? (
            <button
              type="button"
              className="tracker-card__approve-btn"
              aria-label="Approve"
              title="Approve"
              onClick={(e) => {
                e.stopPropagation();
                onApprove();
              }}
            >
              👍 Approve ({task.approvals_count ?? 0}/{task.approvals_needed ?? 2})
            </button>
          ) : null}
          {onReject ? (
            <button
              type="button"
              className="tracker-card__reject-btn"
              aria-label="Reject"
              title="Reject"
              onClick={(e) => {
                e.stopPropagation();
                onReject();
              }}
            >
              Reject
            </button>
          ) : null}
        </div>
      ) : null}
      {meta.length > 0 ? (
        <div className="tracker-card__meta">
          {meta.map((p) => (
            <span key={p}>{p}</span>
          ))}
        </div>
      ) : null}
    </div>
  );
}

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  leoAutonomy,
  leoProposeTask,
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
  trackerClearFinished,
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
  { key: "todo", title: "Ожидает" },
  { key: "approve", title: "Аппрув" },
  { key: "doing", title: "В работе" },
  { key: "review", title: "Review" },
  { key: "test", title: "Тест" },
  { key: "deploy", title: "Сборка" },
  { key: "done", title: "Выполнено" },
  { key: "canceled", title: "Отменено" },
];

const QA_COLS = [
  { key: "todo", title: "К тестированию" },
  { key: "doing", title: "В тестировании" },
  { key: "done", title: "Готово" },
];

const WHEN_PRESETS = [
  { value: "сейчас", label: "Сейчас" },
  { value: "завтра 4:20", label: "Завтра в 4:20" },
  { value: "custom", label: "Дата и время…" },
];

/** «2026-08-20T09:00» из datetime-local → «2026-08-20 09:00», как ждёт трекер. */
function whenFromPicker(value: string): string {
  return value.replace("T", " ");
}

/** «[Спринт 2] текст» → номер спринта отдельным бейджем. */
function parsePrompt(prompt: string): { sprint: number | null; text: string } {
  const raw = String(prompt || "").trim();
  const m = raw.match(/^\[Спринт\s+(\d+)\]\s*/i);
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
  const failed = /агент не стартовал|только заметка|нет правок/i.test(err);
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
  if (parsed.sprint != null) return `[Спринт ${parsed.sprint}] ${body}`;
  return body;
}

/** Выполненные, отменённые и упавшие карточки можно снять с доски пакетом. */
function isClearableTask(task: TrackerTask): boolean {
  const status = String(task.status || "").toLowerCase();
  const column = String(task.dev_column || "").toLowerCase();
  if (["done", "completed"].includes(status) || column === "done") return true;
  if (["canceled", "cancelled"].includes(status) || column === "canceled") return true;
  if (status === "error") return true;
  if (!task.error) return false;
  if (status === "running" || status === "reviewing") return canRetryAgent(task);
  return true;
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
  return new Date(t).toLocaleString("ru-RU", {
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
  todo: { column: "doing", label: "В работу" },
  approve: { column: "doing", label: "В работу" },
  doing: { column: "review", label: "На review" },
  review: { column: "test", label: "На тест" },
  test: { column: "deploy", label: "К сборке" },
  deploy: { column: "done", label: "Выполнено" },
};

/** Автор задачи: Лео или человек из админки. */
function authorLabel(task: TrackerTask, authors: Record<number, string>): string {
  if (isLeoTask(task)) return "Лео";
  const id = Number(task.author_id) || 0;
  if (!id) return "Из чата";
  return authors[id] || `id ${id}`;
}

/** Картинка автора: у Лео своя, у людей — из чата стаи, у задач из чата её нет. */
function authorAvatar(task: TrackerTask, initData: string): string {
  if (isLeoTask(task)) return LEO_AVATAR_URL;
  const id = Number(task.author_id) || 0;
  return id > 0 ? trackerAvatarUrl(initData, id) : "";
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

function metaParts(t: TrackerTask, isQa: boolean): string[] {
  const parts: string[] = [];
  if (t.when) parts.push(t.when);
  if (t.repeat && t.repeat !== "разово") parts.push(t.repeat);
  if (!isQa && t.auto_review) parts.push("авто-ревью");
  if (t.manual_qa) parts.push("ручное QA");
  if (t.fast_track) parts.push("без ревью и теста");
  if (t.auto_push === false) parts.push("без пуша");
  if (!isQa && t.handed_to_qa) parts.push("→ QA");
  if (t.error) parts.push("ошибка");
  return parts;
}

export function TrackerScreen({ initData, showAlert }: Props) {
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
  const [proposal, setProposal] = useState<{ reply: string; title: string; task: string } | null>(null);
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
  /** Пакетная очистка завершённых, отменённых и упавших карточек. */
  const [clearAsk, setClearAsk] = useState(false);

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
          new Set(list.map((t) => Number(t.author_id) || 0).filter((id) => id > 0)),
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
        if (!silent) showAlert(e instanceof Error ? e.message : "Не удалось загрузить доску");
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
        showAlert(`Взяли ${n} ${plural(n, "задачу", "задачи", "задач")} в работу.`);
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
  const clearableCount = tasks.filter(isClearableTask).length;
  const runningCount = pool.filter((t) =>
    isQa
      ? (t.qa_column || "todo") === "doing"
      : t.status === "running" || t.status === "reviewing" || t.steps_running,
  ).length;

  const createTask = async () => {
    await createTaskWith(prompt.trim());
  };

  const createTaskWith = async (raw: string, opts?: { leo?: boolean; needsApproval?: boolean }) => {
    const text = raw.trim();
    if (!text) {
      showAlert("Опиши задачу.");
      return;
    }
    if (when === "custom" && !whenAt) {
      showAlert("Выбери дату и время.");
      return;
    }
    const wantApproval = opts?.leo ? (opts.needsApproval ?? leoNeedsApproval) : needsApproval;
    setBusy(true);
    try {
      const res = await trackerCreate(initData, {
        when: when === "custom" ? whenFromPicker(whenAt) : when,
        prompt: text,
        needs_approval: wantApproval,
        // Задачу сочинил Лео — пусть на доске и стоит он, а не тот, кто одобрил.
        leo: opts?.leo,
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
          ? `Задача на доске в колонке «Аппрув». Админам ушло уведомление.`
          : `Задача поставлена на ${res.when || "ближайший запуск"}.`,
      );
      await load(false, true);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Не удалось поставить задачу");
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
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Лео промолчал");
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
    if (tab === "task") {
      void loadAutonomy();
      void loadDeploy();
    }
  }, [tab, loadAutonomy, loadDeploy]);

  const switchDeploy = async (on: boolean) => {
    setDeployBusy(true);
    try {
      const next = await trackerDeploySettings(initData, on ? "on" : "off");
      setDeploy(next.deploy);
      showAlert(
        on
          ? "Готовые задачи будут собираться на Railway сами."
          : "Автодеплой выключен: код уедет в main, а сборку запускай кнопкой.",
      );
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Не получилось");
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
          ? `Лео работает сам до ${formatWhen(next.active_until)}.`
          : "Лео больше не ставит задачи сам.",
      );
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Не получилось");
    } finally {
      setAutonomyBusy(false);
    }
  };

  const approveProposal = async () => {
    if (!proposal?.task) return;
    const text = proposal.task;
    setProposal(null);
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
      showAlert(e instanceof Error ? e.message : "Не удалось открыть задачу");
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
      showAlert(e instanceof Error ? e.message : "Не получилось");
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
      showAlert(e instanceof Error ? e.message : "Не получилось");
    } finally {
      setBusy(false);
    }
  };

  const restartFromBoard = async (task: TrackerTask) => {
    setBusy(true);
    try {
      await trackerRestart(initData, task.id);
      showAlert(canRetryAgent(task) ? "Снова запускаем агента." : "Задача перезапущена.");
      if (detail?.id === task.id) setDetail(null);
      await load(false, true);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Не удалось перезапустить задачу");
    } finally {
      setBusy(false);
    }
  };

  const deleteFromBoard = async (task: TrackerTask) => {
    setBusy(true);
    try {
      await trackerDelete(initData, task.id);
      showAlert("Задача удалена.");
      if (detail?.id === task.id) setDetail(null);
      setDeleteAsk(null);
      await load();
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Не удалось удалить задачу");
    } finally {
      setBusy(false);
    }
  };

  const clearFinishedFromBoard = async () => {
    setBusy(true);
    try {
      const j = await trackerClearFinished(initData);
      const n = Number(j.deleted) || 0;
      showAlert(
        n > 0
          ? `Убрали ${n} ${plural(n, "задачу", "задачи", "задач")} с доски.`
          : "Нечего убирать — на доске нет завершённых карточек.",
      );
      if (detail && isClearableTask(detail)) setDetail(null);
      setClearAsk(false);
      setTasks(j.tasks ?? []);
      await load(true);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Не удалось очистить доску");
    } finally {
      setBusy(false);
    }
  };

  const loadIdeas = async () => {
    const text = hint.trim();
    if (!text) {
      showAlert("Напиши тему спринта.");
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
      showAlert(e instanceof Error ? e.message : "Не удалось предложить идеи");
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
          title: t.split(/[.!?]/, 1)[0].slice(0, 80) || `Задача ${i + 1}`,
          prompt: t,
          sprint: 1,
          _on: true,
        })),
      );
      setFromLeoSprint(true);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Лео промолчал");
    } finally {
      setSprintBusy("");
    }
  };

  const buildPlan = async () => {
    const idea = ideas.find((i) => String(i.id) === pickedIdea) ?? ideas[0];
    if (!idea) {
      showAlert("Сначала выбери идею.");
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
      showAlert(e instanceof Error ? e.message : "Не удалось собрать план");
    } finally {
      setSprintBusy("");
    }
  };

  const applyPlan = async () => {
    const picked = features.filter((f) => f._on);
    if (!picked.length) {
      showAlert("Отметь хотя бы одну задачу.");
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
          ? `Поставлено задач: ${j.created ?? picked.length}. Сначала аппрув двух админов — в колонке «Аппрув».`
          : `Поставлено задач: ${j.created ?? picked.length}.`,
      );
      setFeatures([]);
      setIdeas([]);
      setHint("");
      setFromLeoSprint(false);
      setLeoSprintReply("");
      setTab("board");
      await load();
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Не удалось поставить задачи");
    } finally {
      setSprintBusy("");
    }
  };

  return (
    <div className="tracker">
      <div className="tracker__tabs" role="tablist">
        <button
          type="button"
          role="tab"
          aria-selected={tab === "board"}
          className={tab === "board" ? "on" : ""}
          onClick={() => setTab("board")}
        >
          Доска
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === "task"}
          className={tab === "task" ? "on" : ""}
          onClick={() => setTab("task")}
        >
          Задача
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === "sprint"}
          className={tab === "sprint" ? "on" : ""}
          onClick={() => setTab("sprint")}
        >
          Спринты
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
                Разработчик
              </button>
              <button
                type="button"
                className={role === "tester" ? "on" : ""}
                onClick={() => setRole("tester")}
              >
                Тестировщик
              </button>
            </div>
            <button
              type="button"
              className={`tracker__refresh${refreshing ? " is-refreshing" : ""}`}
              disabled={refreshing}
              aria-busy={refreshing}
              onClick={() => void refreshBoard()}
            >
              {refreshing ? "Обновляю…" : "Обновить"}
            </button>
            {clearableCount > 0 ? (
              <button
                type="button"
                className="tracker__clear"
                disabled={busy}
                onClick={() => setClearAsk(true)}
              >
                Очистить ({clearableCount})
              </button>
            ) : null}
          </div>

          <div className="tracker__summary">
            <span className="tracker-stat">
              <strong>{pool.length}</strong> {plural(pool.length, "задача", "задачи", "задач")}
            </span>
            {runningCount > 0 ? (
              <span className="tracker-stat tracker-stat--active">
                <strong>{runningCount}</strong> в работе
              </span>
            ) : null}
            {errCount > 0 ? (
              <span className="tracker-stat tracker-stat--err">
                <strong>{errCount}</strong> {plural(errCount, "ошибка", "ошибки", "ошибок")}
              </span>
            ) : null}
          </div>

          <p className="tracker__hint">
            {isQa
              ? "После сдачи можно принять, вернуть в работу или попросить Лео написать чек-лист."
              : "Ожидает → в работе (агент пишет код) → Review (Composer) → тест (Composer) → сборка → выполнено и уведомление. На сервер код уедет, когда напишешь «запушь»."}
          </p>

          {loading ? (
            <p className="tracker__muted">Загружаю доску…</p>
          ) : (
            <div className="tracker__cols">
              {columns.map((col) => {
                const items = pool.filter((t) =>
                  isQa ? (t.qa_column || "todo") === col.key : boardDevColumn(t) === col.key,
                );
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
                          пусто
                        </div>
                      ) : (
                        items.map((t) => (
                          <TaskCard
                            key={t.id}
                            task={t}
                            isQa={isQa}
                            author={authorLabel(t, authors)}
                            avatar={authorAvatar(t, initData)}
                            onOpen={() => void openTask(t)}
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
            Опиши, что сделать, когда запускать и приложи картинку, если так понятнее.
          </p>

          {autonomy ? (
            <div className="tracker__leo tracker__auto">
              <div className="tracker__leo-head">
                <span aria-hidden>🤖</span>
                <b>Лео ведёт продукт сам</b>
                <span className={`tracker__auto-state${autonomy.active ? " is-on" : ""}`}>
                  {autonomy.active ? "включено" : "выключено"}
                </span>
              </div>
              <p className="tracker__hint">
                Раз в {autonomy.every_hours} ч Лео придумывает спринт своим голосом и выносит задачи
                на доску — по {autonomy.tasks_per_run} за прогон. Сначала колонка «Аппрув», потом
                работа. На карточках его аватарка.
              </p>
              {autonomy.active ? (
                <p className="tracker__auto-facts">
                  До {formatWhen(autonomy.active_until)} · следующий спринт{" "}
                  {formatWhen(autonomy.next_run_at)}
                </p>
              ) : null}
              {autonomy.last_note ? (
                <p className="tracker__auto-facts">
                  Прошлый раз ({formatWhen(autonomy.last_run_at)}): {autonomy.last_note}
                </p>
              ) : null}
              <div className="tracker__new-row">
                <select
                  value={autonomyDays}
                  onChange={(e) => setAutonomyDays(Number(e.target.value))}
                  aria-label="Сколько дней Лео работает сам"
                >
                  {[1, 2, 3, 5, 7, 14]
                    .filter((d) => d <= (autonomy.max_days || 14))
                    .map((d) => (
                      <option key={d} value={d}>
                        {d} {plural(d, "день", "дня", "дней")}
                      </option>
                    ))}
                </select>
                <button
                  type="button"
                  className="tracker__primary"
                  disabled={autonomyBusy}
                  onClick={() => void switchAutonomy("start")}
                >
                  {autonomy.active ? "Продлить" : "Пусть работает сам"}
                </button>
                {autonomy.active ? (
                  <button
                    type="button"
                    className="tracker__attach"
                    disabled={autonomyBusy}
                    onClick={() => void switchAutonomy("stop")}
                  >
                    Остановить
                  </button>
                ) : null}
              </div>
            </div>
          ) : null}

          {deploy ? (
            <div className="tracker__leo tracker__auto">
              <div className="tracker__leo-head">
                <span aria-hidden>🚀</span>
                <b>Автодеплой на Railway</b>
                <span className={`tracker__auto-state${deploy.enabled ? " is-on" : ""}`}>
                  {deploy.enabled ? "включено" : "выключено"}
                </span>
              </div>
              <p className="tracker__hint">
                Задача прошла тест — трекер вливает её ветку в main и сам просит Railway собрать
                прод, не надеясь на вебхук. «Выполнено» карточка получит, только когда сборка
                действительно прошла.
              </p>
              {deploy.services?.length ? (
                <p className="tracker__auto-facts">Пересобираем: {deploy.services.join(", ")}</p>
              ) : null}
              {deploy.hint ? <p className="tracker__auto-facts">{deploy.hint}</p> : null}
              <div className="tracker__new-row">
                <button
                  type="button"
                  className={deploy.enabled ? "tracker__attach" : "tracker__primary"}
                  disabled={deployBusy}
                  onClick={() => void switchDeploy(!deploy.enabled)}
                >
                  {deploy.enabled ? "Выключить" : "Включить автодеплой"}
                </button>
              </div>
            </div>
          ) : null}

          <div className="tracker__leo">
            <div className="tracker__leo-head">
              <span aria-hidden>🐆</span>
              <b>Задача от Лео</b>
            </div>
            <p className="tracker__hint">
              Напиши тему — или оставь пусто, тогда Лео решит сам. Понравилось — выноси на доску: сначала аппрув
              всех админов, потом в работу.
            </p>
            <div className="tracker__new-row">
              <input
                value={leoTopic}
                onChange={(e) => setLeoTopic(e.target.value)}
                placeholder="Тема (необязательно): например, удержание новичков"
              />
            </div>
            {proposal ? (
              <>
                <p className="tracker__leo-reply">{proposal.reply}</p>
                {proposal.title ? <p className="tracker__leo-title">{proposal.title}</p> : null}
                <p className="tracker__leo-task">{proposal.task}</p>
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
                      <b>Нужен аппрув всех админов</b>
                      <small>Два аппрува (включая тебя) в Telegram или здесь — и задача уйдёт в работу</small>
                    </span>
                  </label>
                  <button
                    type="button"
                    className="tracker__primary"
                    disabled={busy || proposeBusy || !proposal.task}
                    onClick={() => void approveProposal()}
                  >
                    {leoNeedsApproval ? "На доску (аппрув)" : "На доску"}
                  </button>
                  <button
                    type="button"
                    className="tracker__attach"
                    disabled={proposeBusy}
                    onClick={() => {
                      const reject = proposal.title || proposal.task;
                      setRejected((prev) => [...prev, reject].slice(-10));
                      setProposal(null);
                      void proposeFromLeo(reject);
                    }}
                  >
                    Не то, давай другую
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
                {proposeBusy ? "Лео придумывает…" : "Пусть Лео придумает"}
              </button>
            )}
          </div>

          <div className="tracker__new">
              <textarea
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                placeholder="Что сделать? Например: в админке выгрузка отчёта по тренировкам за неделю"
              />
              {images.length > 0 ? (
                <div className="tracker__previews">
                  {images.map((img, i) => (
                    <div key={`${img.filename}-${i}`} className="tracker__preview">
                      <img src={img.preview} alt="" />
                      <button
                        type="button"
                        onClick={() => setImages((prev) => prev.filter((_, j) => j !== i))}
                        aria-label="Убрать картинку"
                      >
                        ✕
                      </button>
                    </div>
                  ))}
                </div>
              ) : null}
              <label className="tracker-feat">
                <input
                  type="checkbox"
                  checked={needsApproval}
                  onChange={(e) => setNeedsApproval(e.target.checked)}
                />
                <span>
                  <b>Нужен аппрув других админов</b>
                  <small>Два аппрува в Telegram — и задача уйдёт в работу</small>
                </span>
              </label>
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
                <button type="button" className="tracker__attach" onClick={() => setEditorFor("new")}>
                  {images.length > 0 ? "🖼 Ещё фото" : "🖼 Картинка"}
                </button>
                <button type="button" className="tracker__primary" disabled={busy} onClick={() => void createTask()}>
                  Поставить задачу
                </button>
              </div>
            </div>
        </div>
      ) : (
        <div className="tracker__sprint">
          <p className="tracker__hint">
            Опиши тему спринта — Лео предложит идеи и нарежет задачи. Потом их можно поставить на доску.
          </p>
          <textarea
            value={hint}
            onChange={(e) => setHint(e.target.value)}
            placeholder="Например: снизить отток участников стаи"
          />
          <div className="tracker__new-row">
            <label className="tracker__num">
              Спринтов
              <input
                type="number"
                min={1}
                max={8}
                value={sprintCount}
                onChange={(e) => setSprintCount(Math.min(8, Math.max(1, Number(e.target.value) || 1)))}
              />
            </label>
            <label className="tracker__num">
              Задач в спринте
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
              {sprintBusy === "ideas" ? "Думаю…" : "Предложить идеи"}
            </button>
            <button
              type="button"
              className="tracker__attach"
              disabled={sprintBusy !== ""}
              onClick={() => void askLeoSprint()}
            >
              {sprintBusy === "leo" ? "Лео думает…" : "🐆 Спринт от Лео"}
            </button>
          </div>
          {leoSprintReply ? <p className="tracker__leo-reply">🐆 {leoSprintReply}</p> : null}

          {ideas.length > 0 ? (
            <div className="tracker__ideas">
              <h3 className="tracker__subtitle">Идеи</h3>
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
                    <b>{String(idea.title || idea.name || `Идея ${i + 1}`)}</b>
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
                {sprintBusy === "plan" ? "Собираю план…" : "Собрать план и задачи"}
              </button>
            </div>
          ) : null}

          {features.length > 0 ? (
            <div className="tracker__feats">
              <h3 className="tracker__subtitle">Задачи спринта</h3>
              {fromLeoSprint ? (
                <p className="tracker__hint">
                  Спринт от Лео — задачи попадут в «Аппрув», старт после двух аппрувов админов.
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
                      {f.sprint ? <span className="tracker-badge tracker-badge--sprint">Спринт {f.sprint}</span> : null}
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
                {sprintBusy === "apply" ? "Ставлю…" : fromLeoSprint ? "На доску (аппрув)" : "Поставить задачи в план"}
              </button>
            </div>
          ) : null}
        </div>
      )}

      {clearAsk ? (
        <div className="tracker-confirm" role="dialog" aria-modal="true" aria-labelledby="tracker-clear-title">
          <div className="tracker-confirm__box">
            <h3 id="tracker-clear-title">Очистить доску?</h3>
            <p>
              Будут удалены выполненные, отменённые и завершившиеся с ошибкой:{" "}
              <strong>{clearableCount}</strong>{" "}
              {plural(clearableCount, "задача", "задачи", "задач")}.
            </p>
            <p className="tracker-confirm__hint">
              Карточки в работе, на review и в очереди останутся. Восстановить удалённое нельзя.
            </p>
            <div className="tracker-confirm__actions">
              <button type="button" disabled={busy} onClick={() => setClearAsk(false)}>
                Отмена
              </button>
              <button
                type="button"
                className="tracker-confirm__danger"
                disabled={busy}
                onClick={() => void clearFinishedFromBoard()}
              >
                Очистить
              </button>
            </div>
          </div>
        </div>
      ) : null}

      {deleteAsk ? (
        <div className="tracker-confirm" role="dialog" aria-modal="true" aria-labelledby="tracker-delete-title">
          <div className="tracker-confirm__box">
            <h3 id="tracker-delete-title">Удалить задачу?</h3>
            <p>
              <strong>#{taskNo(deleteAsk)}</strong>{" "}
              {parsePrompt(deleteAsk.prompt).text.slice(0, 160) || deleteAsk.prompt}
              {parsePrompt(deleteAsk.prompt).text.length > 160 ? "…" : ""}
            </p>
            <p className="tracker-confirm__hint">Карточка и приложенные фото исчезнут с доски без возможности восстановить.</p>
            <div className="tracker-confirm__actions">
              <button type="button" disabled={busy} onClick={() => setDeleteAsk(null)}>
                Отмена
              </button>
              <button
                type="button"
                className="tracker-confirm__danger"
                disabled={busy}
                onClick={() => void deleteFromBoard(deleteAsk)}
              >
                Удалить
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
                aria-label="Закрыть"
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
              <span>Поставил: {authorLabel(detail, authors)}</span>
            </div>
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
                          if (!next) throw new Error("Опиши задачу");
                          const res = await trackerPrompt(initData, detail.id, next);
                          if (res.task) setDetail(res.task);
                          setEditingPrompt(false);
                        }, "Формулировку обновили.")
                      }
                    >
                      Сохранить
                    </button>
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() => {
                        setEditingPrompt(false);
                        setPromptDraft("");
                      }}
                    >
                      Отмена
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
                      Изменить текст
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
                      title="Открыть во весь экран"
                    >
                      <img src={attPreviews[att.id]} alt={att.name} />
                    </button>
                  ) : (
                    <div className="tracker-att__shot tracker-att__shot--wait">загрузка…</div>
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
                      Заменить
                    </button>
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() =>
                        void actOnDetail(
                          () => trackerAttachmentDelete(initData, detail.id, att.id),
                          "Фото убрано.",
                        )
                      }
                    >
                      Убрать
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
                {(detail.attachments ?? []).length > 0 ? "Ещё фото" : "Приложить фото"}
                <span className="tracker-att__hint"> · или вставь из буфера</span>
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
                  Код в этом проекте. Чтобы выкатить на сервер — напиши «запушь».
                </span>
                <div className="tracker-modal__result-row">
                  <button
                    type="button"
                    className="tracker__primary"
                    disabled={busy}
                    onClick={() =>
                      void actOnDetail(async () => {
                        const res = await trackerShip(initData, detail.id);
                        if (res.skipped) throw new Error("Задачу ещё рано отмечать — сдвинь её дальше по доске.");
                        if (res.deployed || res.pushed) return;
                        if (res.error) throw new Error(res.error);
                      }, "Отмечено. Чтобы выкатить на сервер — напиши «запушь».")
                    }
                  >
                    Отметить к публикации
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
                  {canReturnToWork(detail) ? "Вернуть в ожидание на" : "Запуск"}
                  <input type="datetime-local" value={moveAt} onChange={(e) => setMoveAt(e.target.value)} />
                </label>
                <button
                  type="button"
                  disabled={busy || !moveAt}
                  onClick={() =>
                    void act(
                      () => trackerReschedule(initData, detail.id, moveAt.replace("T", " ")),
                      "Время запуска изменено.",
                    )
                  }
                >
                  Перенести
                </button>
              </div>
            ) : null}

            <div className="tracker-modal__actions">
              <button type="button" disabled={busy} onClick={() => setEditorFor(detail.id)}>
                🖼 Картинка
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
                        "Аппрув учтён.",
                      )
                    }
                  >
                    Аппрув ({detail.approvals_count ?? 0}/{detail.approvals_needed ?? 2})
                  </button>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() =>
                      void actOnDetail(
                        () => trackerApprove(initData, detail.id, "reject"),
                        "Задача отклонена.",
                      )
                    }
                  >
                    Отклонить
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
                      `Сдвинули: ${NEXT_COL[detail.dev_column || "todo"].label}.`,
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
                    void actOnDetail(() => trackerReview(initData, detail.id), "Composer взял ревью.")
                  }
                >
                  Ревью Composer
                </button>
              ) : null}
              {!isQa && detail.dev_column === "test" ? (
                <button
                  type="button"
                  className="tracker-modal__accent"
                  disabled={busy}
                  onClick={() =>
                    void actOnDetail(() => trackerAutoTest(initData, detail.id), "Composer взял тест.")
                  }
                >
                  Тест Composer
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
                      if (res.busy) throw new Error("Сборка этой задачи уже идёт.");
                    }, "Railway собирает прод. Ход сборки — в шагах карточки.")
                  }
                >
                  🚀 Задеплоить
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
                      canRetryAgent(detail) ? "Снова запускаем агента." : "Задача перезапущена.",
                    )
                  }
                >
                  {canRetryAgent(detail) ? "Запустить снова" : "Перезапустить"}
                </button>
              ) : null}
              {isQa && detail.handed_to_qa ? (
                <>
                  {(detail.qa_column || "todo") === "todo" ? (
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() => void act(() => trackerQa(initData, detail.id, "start"), "Взяли в тест.")}
                    >
                      Взять в тест
                    </button>
                  ) : null}
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void act(() => trackerQa(initData, detail.id, "pass"), "Задача принята. Чтобы выкатить — напиши «запушь».")}
                  >
                    Принять
                  </button>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void act(() => trackerQa(initData, detail.id, "fail"), "Вернул разработчику.")}
                  >
                    Вернуть
                  </button>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void act(() => trackerAutoQa(initData, detail.id), "Лео написал чек-лист.")}
                  >
                    AI-тест
                  </button>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void act(() => trackerAutoTest(initData, detail.id), "Composer взял тест.")}
                  >
                    Тест Composer
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
                        ? "Задача остановлена."
                        : "Задача отменена.",
                    )
                  }
                >
                  {detail.status === "running" || detail.status === "reviewing" ? "Остановить" : "Отменить"}
                </button>
              ) : null}
              {detail.can_delete ? (
                <button
                  type="button"
                  className="tracker-modal__danger"
                  disabled={busy}
                  onClick={() => void act(() => trackerDelete(initData, detail.id), "Задача удалена.")}
                >
                  Удалить
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
              }, oldAtt ? "Фото заменено." : "Картинка приложена.");
            }
          }}
        />
      ) : null}

      {zoomed ? (
        <button type="button" className="tracker-zoom" onClick={() => setZoomed(null)} aria-label="Закрыть">
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
  onOpen,
  onRestart,
  onDelete,
}: {
  task: TrackerTask;
  isQa: boolean;
  author: string;
  avatar: string;
  onOpen: () => void;
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
                aria-label="Перезапустить задачу"
                title="Перезапустить"
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
                aria-label="Удалить задачу"
                title="Удалить"
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
          {parsed.sprint ? <span className="tracker-badge tracker-badge--sprint">Спринт {parsed.sprint}</span> : null}
          {isLeoTask(task) ? <span className="tracker-badge tracker-badge--leo">🐆 Лео</span> : null}
          {task.needs_approval ? (
            <span className="tracker-badge tracker-badge--approve">
              👍 {task.approvals_count ?? 0}/{task.approvals_needed ?? 2}
            </span>
          ) : null}
          {task.kind === "deploy_fix" ? (
            <span className="tracker-badge tracker-badge--deploy">авто-фикс деплоя</span>
          ) : null}
          {task.manual_qa ? <span className="tracker-badge tracker-badge--qa">ручное QA</span> : null}
          {task.fast_track ? <span className="tracker-badge tracker-badge--deploy">⚡ быстрый выпуск</span> : null}
          {task.has_attachments ? (
            <span className="tracker-badge tracker-badge--photo">📷 {task.attachments_count || 1}</span>
          ) : null}
        </div>
      ) : null}
      {resultPreview ? (
        <div className="tracker-card__live tracker-card__live--result">{resultPreview}</div>
      ) : showLive ? (
        <div className="tracker-card__live">{live || "⏳ выполняется…"}</div>
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

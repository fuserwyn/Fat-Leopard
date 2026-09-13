// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { TrackerTask } from "../lib/trackerApi";

const trackerList = vi.fn();
const trackerRefresh = vi.fn();
const trackerAuthors = vi.fn();
const trackerRestart = vi.fn();
const trackerTask = vi.fn();
const trackerPrompt = vi.fn();
const trackerClearFinished = vi.fn();

vi.mock("../lib/trackerApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../lib/trackerApi")>();
  return {
    ...actual,
    trackerList: (...args: unknown[]) => trackerList(...args),
    trackerRefresh: (...args: unknown[]) => trackerRefresh(...args),
    trackerAuthors: (...args: unknown[]) => trackerAuthors(...args),
    trackerRestart: (...args: unknown[]) => trackerRestart(...args),
    trackerTask: (...args: unknown[]) => trackerTask(...args),
    trackerPrompt: (...args: unknown[]) => trackerPrompt(...args),
    trackerClearFinished: (...args: unknown[]) => trackerClearFinished(...args),
    trackerAvatarUrl: () => "",
  };
});

import { TrackerScreen } from "./TrackerScreen";

afterEach(() => {
  cleanup();
  trackerList.mockReset();
  trackerRefresh.mockReset();
  trackerAuthors.mockReset();
  trackerRestart.mockReset();
  trackerTask.mockReset();
  trackerPrompt.mockReset();
  trackerClearFinished.mockReset();
});

const pending: TrackerTask = {
  id: 11,
  num: 1,
  prompt: "починить кнопку обновить",
  repo: "",
  when: "20.08 09:19",
  repeat: "разово",
  kind: "task",
  status: "pending",
  status_label: "Ожидает",
  status_icon: "⏳",
  done: false,
  active: true,
  can_delete: true,
  can_edit_prompt: true,
  auto_review: false,
  manual_qa: false,
  fast_track: false,
  error: "",
  has_result: false,
  phase: "todo",
  qa_status: null,
  qa_label: "",
  qa_icon: "",
  auto_qa_running: false,
  dev_column: "todo",
  qa_column: null,
  handed_to_qa: false,
  attachments_count: 0,
  has_attachments: false,
  author_id: 42,
};

const running: TrackerTask = {
  ...pending,
  status: "running",
  status_label: "В работе",
  status_icon: "🔧",
  phase: "doing",
  dev_column: "doing",
};

const reviewed: TrackerTask = {
  ...running,
  status: "reviewing",
  status_label: "Review",
  status_icon: "👀",
  phase: "review",
  dev_column: "review",
  has_result: true,
  result: "⏰ Задача #1 выполнена.\n\nГотово.\n- Подпись теперь только «сгорит через …».",
  live_step: "Агент сдал результат",
};

const doneTask: TrackerTask = {
  ...pending,
  status: "done",
  status_label: "Выполнено",
  status_icon: "✅",
  phase: "done",
  dev_column: "done",
  done: true,
  active: false,
  can_restart: true,
};

describe("TrackerScreen prompt edit", () => {
  it("shows edit control for queued task and saves new text", async () => {
    trackerList.mockResolvedValue({ tasks: [pending], started: 0 });
    trackerAuthors.mockResolvedValue([]);
    trackerTask.mockResolvedValue({ task: pending });
    trackerPrompt.mockResolvedValue({
      ok: true,
      task: { ...pending, prompt: "новый текст задачи" },
    });
    const alerts: string[] = [];

    render(<TrackerScreen initData="admin" showAlert={(t) => alerts.push(t)} />);
    await waitFor(() => expect(screen.getByText("#1")).toBeTruthy());

    fireEvent.click(screen.getByText("#1"));
    await waitFor(() => expect(screen.getByRole("button", { name: "Изменить текст" })).toBeTruthy());

    fireEvent.click(screen.getByRole("button", { name: "Изменить текст" }));
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "новый текст задачи" } });
    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));

    await waitFor(() =>
      expect(trackerPrompt).toHaveBeenCalledWith("admin", 11, "новый текст задачи"),
    );
    expect(alerts.some((a) => a.includes("Формулировку"))).toBe(true);
  });

  it("hides edit control for running task", async () => {
    trackerList.mockResolvedValue({ tasks: [running], started: 0 });
    trackerAuthors.mockResolvedValue([]);
    trackerTask.mockResolvedValue({ task: { ...running, can_edit_prompt: false } });

    render(<TrackerScreen initData="admin" showAlert={() => undefined} />);
    await waitFor(() => expect(screen.getByText("#1")).toBeTruthy());

    fireEvent.click(screen.getByText("#1"));
    await waitFor(() => expect(screen.getByRole("dialog")).toBeTruthy());
    expect(screen.queryByRole("button", { name: "Изменить текст" })).toBeNull();
  });
});

describe("TrackerScreen restart", () => {
  it("shows restart on done card and calls restart op", async () => {
    trackerList.mockResolvedValue({ tasks: [doneTask], started: 0 });
    trackerRestart.mockResolvedValue({ ok: true });
    trackerAuthors.mockResolvedValue([]);
    const alerts: string[] = [];

    render(<TrackerScreen initData="admin" showAlert={(t) => alerts.push(t)} />);
    await waitFor(() => expect(screen.getByText("#1")).toBeTruthy());

    fireEvent.click(screen.getByRole("button", { name: "Перезапустить задачу" }));
    await waitFor(() => expect(trackerRestart).toHaveBeenCalledWith("admin", 11));
    expect(alerts.some((a) => a.includes("перезапущена"))).toBe(true);
  });
});

const awaitingApproval: TrackerTask = {
  ...pending,
  id: 12,
  num: 2,
  prompt: "новая фича от Лео",
  kind: "leo_task",
  needs_approval: true,
  approvals_count: 0,
  approvals_needed: 2,
  status_label: "Аппрув",
  status_icon: "👍",
  phase: "approve",
  dev_column: "approve",
};

describe("TrackerScreen approval column", () => {
  it("shows unapproved task in approve column even when dev_column is todo", async () => {
    trackerList.mockResolvedValue({
      tasks: [{ ...awaitingApproval, dev_column: "todo", status_label: "Ожидает", status_icon: "⏳", phase: "todo" }],
      started: 0,
    });
    trackerAuthors.mockResolvedValue([]);

    render(<TrackerScreen initData="admin" showAlert={() => undefined} />);
    await waitFor(() => expect(screen.getByText("#2")).toBeTruthy());
    expect(document.querySelector('[data-col="todo"]')?.textContent).not.toContain("#2");
    expect(document.querySelector('[data-col="approve"]')?.textContent).toContain("#2");
  });
});

const canceledTask: TrackerTask = {
  ...pending,
  id: 13,
  num: 3,
  status: "canceled",
  status_label: "Отменено",
  status_icon: "⛔",
  phase: "canceled",
  dev_column: "canceled",
  done: false,
  active: false,
  can_delete: true,
};

const failedTask: TrackerTask = {
  ...running,
  id: 14,
  num: 4,
  error: "Агент не стартовал",
  can_restart: true,
};

describe("TrackerScreen clear finished", () => {
  it("shows clear button for finished cards and removes them", async () => {
    trackerList.mockResolvedValue({ tasks: [pending, doneTask, canceledTask, failedTask], started: 0 });
    trackerAuthors.mockResolvedValue([]);
    trackerClearFinished.mockResolvedValue({ ok: true, deleted: 3, tasks: [pending] });
    const alerts: string[] = [];

    render(<TrackerScreen initData="admin" showAlert={(t) => alerts.push(t)} />);
    await waitFor(() => expect(screen.getByRole("button", { name: "Очистить (3)" })).toBeTruthy());

    fireEvent.click(screen.getByRole("button", { name: "Очистить (3)" }));
    await waitFor(() => expect(screen.getByRole("heading", { name: "Очистить доску?" })).toBeTruthy());

    const dialog = screen.getByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Очистить" }));
    await waitFor(() => expect(trackerClearFinished).toHaveBeenCalledWith("admin"));
    expect(alerts.some((a) => a.includes("Убрали 3"))).toBe(true);
  });

  it("hides clear button when nothing to remove", async () => {
    const inWork: TrackerTask = { ...running, id: 15, num: 2 };
    trackerList.mockResolvedValue({ tasks: [pending, inWork], started: 0 });
    trackerAuthors.mockResolvedValue([]);

    render(<TrackerScreen initData="admin" showAlert={() => undefined} />);
    await waitFor(() => expect(screen.getByText("#1")).toBeTruthy());
    expect(screen.queryByRole("button", { name: /Очистить/ })).toBeNull();
  });
});

describe("TrackerScreen refresh button", () => {
  it("calls refresh and moves a due card into work", async () => {
    trackerList.mockResolvedValue({ tasks: [pending], started: 0 });
    trackerRefresh.mockResolvedValue({ tasks: [running], started: 1 });
    trackerAuthors.mockResolvedValue([]);
    const alerts: string[] = [];

    render(<TrackerScreen initData="admin" showAlert={(t) => alerts.push(t)} />);
    await waitFor(() => expect(screen.getByText("#1")).toBeTruthy());
    expect(document.querySelector('[data-col="todo"]')?.textContent).toContain("#1");
    expect(trackerList).toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Обновить" }));
    await waitFor(() => expect(trackerRefresh).toHaveBeenCalledWith("admin"));
    await waitFor(() => {
      expect(document.querySelector('[data-col="doing"]')?.textContent).toContain("#1");
    });
    expect(alerts.some((a) => a.includes("Взяли 1"))).toBe(true);
  });

  it("starts a waiting card after create by calling refresh", async () => {
    trackerList.mockResolvedValue({ tasks: [pending], started: 0 });
    trackerRefresh.mockResolvedValue({ tasks: [running], started: 1 });
    trackerAuthors.mockResolvedValue([]);

    render(<TrackerScreen initData="admin" showAlert={() => undefined} />);
    await waitFor(() => expect(screen.getByText("#1")).toBeTruthy());
    expect(document.querySelector('[data-col="todo"]')?.textContent).toContain("#1");

    fireEvent.click(screen.getByRole("button", { name: "Обновить" }));
    await waitFor(() => {
      expect(document.querySelector('[data-col="doing"]')?.textContent).toContain("#1");
    });
  });

  it("shows a completed result on the Review column, not in work", async () => {
    trackerList.mockResolvedValue({ tasks: [reviewed], started: 0 });
    trackerAuthors.mockResolvedValue([]);

    render(<TrackerScreen initData="admin" showAlert={() => undefined} />);
    await waitFor(() => expect(screen.getByText("#1")).toBeTruthy());
    expect(document.querySelector('[data-col="doing"]')?.textContent).not.toContain("#1");
    expect(document.querySelector('[data-col="review"]')?.textContent).toContain("#1");
    expect(document.querySelector('[data-col="review"]')?.textContent).toContain("👀");
    expect(document.querySelector(".tracker-card__live--result")?.textContent).toContain("выполнена");
  });

  it("shows the build column and the Composer pipeline hint", async () => {
    trackerList.mockResolvedValue({ tasks: [reviewed], started: 0 });
    trackerAuthors.mockResolvedValue([]);

    render(<TrackerScreen initData="admin" showAlert={() => undefined} />);
    await waitFor(() => expect(screen.getByText("#1")).toBeTruthy());
    expect(document.querySelector('[data-col="deploy"]')?.textContent).toContain("Сборка");
    expect(screen.getByText(/Composer/)).toBeTruthy();
  });
});

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
const trackerApprove = vi.fn();

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
    trackerApprove: (...args: unknown[]) => trackerApprove(...args),
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
  trackerApprove.mockReset();
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
  status_label: "Pending",
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
  status_label: "In progress",
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
  result: "⏰ Task #1 done.\n\nDone.\n- Label now only shows burn timer.",
  live_step: "Agent handed off result",
};

const doneTask: TrackerTask = {
  ...pending,
  status: "done",
  status_label: "Done",
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
      task: { ...pending, prompt: "new task text" },
    });
    const alerts: string[] = [];

    render(<TrackerScreen initData="admin" showAlert={(t) => alerts.push(t)} />);
    await waitFor(() => expect(screen.getByText("#1")).toBeTruthy());

    fireEvent.click(screen.getByText("#1"));
    await waitFor(() => expect(screen.getByRole("button", { name: "Edit text" })).toBeTruthy());

    fireEvent.click(screen.getByRole("button", { name: "Edit text" }));
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "new task text" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(trackerPrompt).toHaveBeenCalledWith("admin", 11, "new task text"),
    );
    expect(alerts.some((a) => a.includes("Wording"))).toBe(true);
  });

  it("hides edit control for running task", async () => {
    trackerList.mockResolvedValue({ tasks: [running], started: 0 });
    trackerAuthors.mockResolvedValue([]);
    trackerTask.mockResolvedValue({ task: { ...running, can_edit_prompt: false } });

    render(<TrackerScreen initData="admin" showAlert={() => undefined} />);
    await waitFor(() => expect(screen.getByText("#1")).toBeTruthy());

    fireEvent.click(screen.getByText("#1"));
    await waitFor(() => expect(screen.getByRole("dialog")).toBeTruthy());
    expect(screen.queryByRole("button", { name: "Edit text" })).toBeNull();
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

    fireEvent.click(screen.getByRole("button", { name: "Restart task" }));
    await waitFor(() => expect(trackerRestart).toHaveBeenCalledWith("admin", 11));
    expect(alerts.some((a) => a.includes("restarted"))).toBe(true);
  });
});

describe("TrackerScreen agent change", () => {
  const failed: TrackerTask = {
    ...running,
    status: "error",
    error: "Agent failed to start: cursor run error",
    agent: "cursor",
    can_edit_prompt: false,
    can_restart: true,
  };

  it("offers another agent on a failed started task", async () => {
    trackerList.mockResolvedValue({ tasks: [failed], started: 0 });
    trackerAuthors.mockResolvedValue([]);
    trackerTask.mockResolvedValue({ task: failed });
    trackerPrompt.mockResolvedValue({ ok: true, task: { ...failed, agent: "claude" } });

    render(<TrackerScreen initData="admin" showAlert={() => undefined} />);
    await waitFor(() => expect(screen.getByText("#1")).toBeTruthy());
    fireEvent.click(screen.getByText("#1"));
    const dialog = await screen.findByRole("dialog");

    const select = within(dialog).getByLabelText("Assignee") as HTMLSelectElement;
    expect(select.value).toBe("cursor");
    fireEvent.change(select, { target: { value: "claude" } });
    await waitFor(() =>
      expect(trackerPrompt).toHaveBeenCalledWith("admin", failed.id, failed.prompt, "claude"),
    );
  });

  it("keeps the agent fixed while the task is running", async () => {
    const live: TrackerTask = { ...running, can_edit_prompt: false, can_restart: false };
    trackerList.mockResolvedValue({ tasks: [live], started: 0 });
    trackerAuthors.mockResolvedValue([]);
    trackerTask.mockResolvedValue({ task: live });

    render(<TrackerScreen initData="admin" showAlert={() => undefined} />);
    await waitFor(() => expect(screen.getByText("#1")).toBeTruthy());
    fireEvent.click(screen.getByText("#1"));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).queryByLabelText("Assignee")).toBeNull();
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
  status_label: "Approve",
  status_icon: "👍",
  phase: "approve",
  dev_column: "approve",
};

describe("TrackerScreen todo column order", () => {
  it("shows oldest created task first in Pending (FIFO)", async () => {
    const older: TrackerTask = {
      ...pending,
      id: 10,
      num: 1,
      prompt: "old task",
      created_at: "2026-01-01T10:00:00Z",
    };
    const newer: TrackerTask = {
      ...pending,
      id: 20,
      num: 2,
      prompt: "new task",
      created_at: "2026-02-01T10:00:00Z",
    };
    trackerList.mockResolvedValue({ tasks: [newer, older], started: 0 });
    trackerAuthors.mockResolvedValue([]);

    render(<TrackerScreen initData="admin" showAlert={() => undefined} />);
    await waitFor(() => expect(screen.getByText("#1")).toBeTruthy());

    const todoCol = document.querySelector('[data-col="todo"]');
    const text = todoCol?.textContent || "";
    expect(text.indexOf("#1")).toBeLessThan(text.indexOf("#2"));
  });
});

describe("TrackerScreen approval column", () => {
  it("shows unapproved task in approve column even when dev_column is todo", async () => {
    trackerList.mockResolvedValue({
      tasks: [{ ...awaitingApproval, dev_column: "todo", status_label: "Pending", status_icon: "⏳", phase: "todo" }],
      started: 0,
    });
    trackerAuthors.mockResolvedValue([]);

    render(<TrackerScreen initData="admin" showAlert={() => undefined} />);
    await waitFor(() => expect(screen.getByText("#2")).toBeTruthy());
    expect(document.querySelector('[data-col="todo"]')?.textContent).not.toContain("#2");
    expect(document.querySelector('[data-col="approve"]')?.textContent).toContain("#2");
  });

  it("shows approve button on board card and calls approve op", async () => {
    trackerList.mockResolvedValue({ tasks: [awaitingApproval], started: 0 });
    trackerAuthors.mockResolvedValue([]);
    trackerApprove.mockResolvedValue({
      ok: true,
      task: { ...awaitingApproval, approvals_count: 1 },
    });
    const alerts: string[] = [];

    render(<TrackerScreen initData="admin" showAlert={(t) => alerts.push(t)} />);
    await waitFor(() => expect(screen.getByText("#2")).toBeTruthy());

    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    await waitFor(() => expect(trackerApprove).toHaveBeenCalledWith("admin", 12, "approve"));
    expect(alerts.some((a) => a.includes("Approval recorded"))).toBe(true);
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

    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => expect(trackerRefresh).toHaveBeenCalledWith("admin"));
    await waitFor(() => {
      expect(document.querySelector('[data-col="doing"]')?.textContent).toContain("#1");
    });
    expect(alerts.some((a) => a.includes("Picked up 1"))).toBe(true);
  });

  it("starts a waiting card after create by calling refresh", async () => {
    trackerList.mockResolvedValue({ tasks: [pending], started: 0 });
    trackerRefresh.mockResolvedValue({ tasks: [running], started: 1 });
    trackerAuthors.mockResolvedValue([]);

    render(<TrackerScreen initData="admin" showAlert={() => undefined} />);
    await waitFor(() => expect(screen.getByText("#1")).toBeTruthy());
    expect(document.querySelector('[data-col="todo"]')?.textContent).toContain("#1");

    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
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
    expect(document.querySelector(".tracker-card__live--result")?.textContent).toContain("done");
  });

  it("shows the build column and the pipeline hint", async () => {
    trackerList.mockResolvedValue({ tasks: [reviewed], started: 0 });
    trackerAuthors.mockResolvedValue([]);

    render(<TrackerScreen initData="admin" showAlert={() => undefined} />);
    await waitFor(() => expect(screen.getByText("#1")).toBeTruthy());
    expect(document.querySelector('[data-col="deploy"]')?.textContent).toContain("Deploy");
    expect(screen.getByText(/Review \(branch check\)/)).toBeTruthy();
  });
});

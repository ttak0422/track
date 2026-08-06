import { describe, expect, it } from "vitest";
import type { TaskItem } from "../../types";
import { sameTask } from "./TaskControls";

const task: TaskItem = {
  line: 3,
  state: "TODO",
  done: false,
  text: "a task",
  scheduled: "2026-08-01",
  due: "2026-08-07",
  completed: undefined,
  priority: "A",
};

describe("sameTask", () => {
  it("treats a refreshed but unchanged task as the same, so the controls skip the re-render", () => {
    expect(sameTask(task, { ...task })).toBe(true);
  });

  // The memo comparison is what keeps an open date picker alive across a sync landing, so a field
  // dropped from it would show as a row that quietly stops updating. Walk them all.
  it("notices a change in any field a control renders", () => {
    const changes: Partial<TaskItem>[] = [
      { line: 4 },
      { state: "DOING" },
      { done: true },
      { text: "renamed" },
      { scheduled: "2026-08-02" },
      { due: undefined },
      { completed: "2026-08-07" },
      { priority: "B" },
    ];
    for (const change of changes) {
      expect(sameTask(task, { ...task, ...change }), `change: ${JSON.stringify(change)}`).toBe(false);
    }
  });

  it("handles a task appearing or disappearing", () => {
    expect(sameTask(undefined, undefined)).toBe(true);
    expect(sameTask(task, undefined)).toBe(false);
    expect(sameTask(undefined, task)).toBe(false);
  });
});

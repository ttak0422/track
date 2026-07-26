import type { TaskState } from "./types";

// taskStates mirrors the engine's task.States (internal/track/task). The set is fixed, not
// configurable, so it is not carried on the wire with a note's tasks — both sides hold the same five
// states and neither has to be told them. Keep this list in step with the Go one; the markers are
// what a checkbox line is read back by.
export const taskStates: TaskState[] = [
  { name: "TODO", char: " ", done: false },
  { name: "DOING", char: "/", done: false },
  { name: "WAITING", char: "?", done: false },
  { name: "DONE", char: "x", done: true },
  { name: "CANCELLED", char: "-", done: true },
];

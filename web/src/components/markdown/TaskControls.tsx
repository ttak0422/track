import type { Element } from "hast";
import { type ReactNode, useContext, useState } from "react";
import { TaskBoardContext } from "./context";
import { useSetTaskDateMutation, useSetTaskStateMutation } from "../../queries";
import { STATIC_MODE } from "../../runtime";
import { taskStates } from "../../taskStates";
import type { DateField, NoteID, TaskItem } from "../../types";

// The write UI a rendered body carries: the tickable plain checkbox of a GFM checklist, and the
// notation table's state and date cells. Each one resolves the line it was rendered from back to the
// task the engine parsed (through TaskBoardContext), and each falls back to the read-only look where
// that resolution comes up empty — the published static site, a preview carrying no note, an editor
// buffer with unsaved edits — so a write is never offered where it would be refused or would land on
// a line that has since moved. Sorting the table is view-only; the note keeps its own order.

// ElementProps is a copy of MarkdownView's, not an import of it: these components are values in its
// markdownComponents map, so importing back from there would close a module-level import cycle.
interface ElementProps {
  node?: Element;
  children?: ReactNode;
}

// useTaskAtLine resolves a rendered line back to the task the engine parsed. It yields nothing on
// the published static site, inside a preview that carries no note, or while the editor buffer is
// dirty (noteID is blanked then) — every surface where a write would either be refused or land on a
// line that no longer means what it did. Reading the context costs no query client, so a read-only
// render (a preview, a test) never needs one; the control below mounts the mutation only once there
// is something to write.
function useTaskAtLine(line: number) {
  const { noteID, tasks, etag, lineOffset = 0 } = useContext(TaskBoardContext);
  const item =
    !STATIC_MODE && noteID !== "" && tasks && etag && line > 0
      ? tasks.items.find((t) => t.line === line + lineOffset)
      : undefined;
  return { noteID, item, etag };
}

// TaskCheck makes a plain GFM checklist ("- [ ] foo", no task notation) tickable: the engine has
// always parsed those lines as tasks, only the frontend left the native checkbox disabled. The line
// comes from rehypeTaskCheck; a box it cannot resolve stays exactly as it renders today.
export function TaskCheck({ line, checked }: { line: number; checked: boolean }) {
  const { noteID, item, etag } = useTaskAtLine(line);
  if (!item) {
    return <input type="checkbox" checked={checked} disabled readOnly />;
  }
  return <TaskCheckControl noteID={noteID} item={item} etag={etag!} />;
}

function TaskCheckControl({ noteID, item, etag }: { noteID: NoteID; item: TaskItem; etag: string }) {
  const mutation = useSetTaskStateMutation(noteID);
  const target = taskStates.find((state) => state.done !== item.done);
  // While the write is in flight, show where it is going rather than snapping back.
  const shown = mutation.isPending ? !item.done : item.done;
  return (
    <input
      type="checkbox"
      checked={shown}
      disabled={mutation.isPending || !target}
      aria-label={`Toggle task: ${item.text}`}
      onChange={() => {
        if (target) mutation.mutate({ line: item.line, state: target.name, expect: item.state, etag });
      }}
    />
  );
}

// TaskRowDate is the scheduled/due cell. Read-only it is the marked date as written; where the note
// can be written it is a native date input styled down to look like that same text, so picking a
// date is a click on what it shows rather than a separate editing mode. An empty cell shows nothing
// until the row is hovered or focused (the CSS reveals it), so an untouched table stays quiet.
function TaskRowDate({ field, value, line }: { field: DateField; value: string; line: number }) {
  const { noteID, item, etag } = useTaskAtLine(line);
  const marker = field === "sched" ? "▷" : "!";
  if (!item) {
    return <>{value ? `${marker} ${value}` : ""}</>;
  }
  return <TaskRowDateControl noteID={noteID} item={item} field={field} value={value} etag={etag!} />;
}

function TaskRowDateControl({
  noteID,
  item,
  field,
  value,
  etag,
}: {
  noteID: NoteID;
  item: TaskItem;
  field: DateField;
  value: string;
  etag: string;
}) {
  const mutation = useSetTaskDateMutation(noteID);
  return (
    <input
      type="date"
      className="task-row-date-input"
      aria-label={field === "sched" ? "Scheduled date" : "Due date"}
      value={value}
      disabled={mutation.isPending}
      data-empty={value === "" || undefined}
      // The cell wears the note's own type and hides the browser's picker indicator, so a click would
      // otherwise land in the date segments. showPicker opens the calendar the indicator would have
      // opened, which keeps picking a date one click on what the cell already shows.
      //
      // The indicator we hide is a -webkit- pseudo, so Gecko still draws its own calendar button and
      // toggles the picker from a system-group click listener — a second dispatch pass, after this
      // handler. It would find the picker already open and close it. Cancelling the click makes that
      // listener stand down (it returns early on defaultPrevented) and costs nothing elsewhere: no
      // engine focuses a date segment on click, only on mousedown.
      onClick={(event) => {
        event.preventDefault();
        event.currentTarget.showPicker?.();
      }}
      onChange={(event) =>
        mutation.mutate({ line: item.line, field, date: event.currentTarget.value, expect: item.state, etag })
      }
    />
  );
}

// TaskRowState is the state cell of a task-table row, and doubles as the state control: in the
// live workspace it renders as a select stripped down to the badge's text look, writing through
// the same engine path as the board's cards. Its source line resolves the row to the engine-parsed
// task (rendered bodies are line-aligned with the note file — the invariant includes rely on); on
// static sites and hover previews (no note id) it stays a plain badge.
function TaskRowState({ name, done, line }: { name: string; done: boolean; line: number }) {
  const { noteID, item, etag } = useTaskAtLine(line);
  const className = `task-row-state${done ? " task-row-state-done" : ""}`;
  if (!item) {
    return <span className={className}>{name}</span>;
  }
  return <TaskRowStateControl noteID={noteID} item={item} className={className} etag={etag!} />;
}

function TaskRowStateControl({
  noteID,
  item,
  className,
  etag,
}: {
  noteID: NoteID;
  item: TaskItem;
  className: string;
  etag: string;
}) {
  const mutation = useSetTaskStateMutation(noteID);
  return (
    <select
      className={className}
      aria-label="Task state"
      value={item.state}
      disabled={mutation.isPending}
      onChange={(event) =>
        mutation.mutate({ line: item.line, state: event.currentTarget.value, expect: item.state, etag })
      }
    >
      {taskStates.map((state) => (
        <option key={state.name} value={state.name}>
          {state.name}
        </option>
      ))}
    </select>
  );
}

type TaskRowProps = { line?: unknown; state?: unknown; done?: unknown; sched?: unknown; due?: unknown; depth?: unknown };

// TaskTable renders a notation-bearing checklist as one sortable table. Sorting is view-only (the
// note keeps its order); STATE sorts by the state-set order, the date columns sort empties last, and
// a third click on a header returns to the source order.
export function TaskTable({ node, children }: ElementProps) {
  const [sort, setSort] = useState<{ key: "state" | DateField; asc: boolean } | null>(null);
  const rowNodes = (node?.children ?? []).filter((c): c is Element => c.type === "element");
  const rowEls = (Array.isArray(children) ? children : [children]).filter((c) => typeof c !== "string");
  let order = rowNodes.map((_, i) => i);
  if (sort) {
    const { key, asc } = sort;
    const valueOf = (i: number): number | string => {
      const p = (rowNodes[i].properties ?? {}) as TaskRowProps;
      if (key === "state") {
        return taskStates.findIndex((s) => s.name === String(p.state ?? ""));
      }
      return String(p[key] ?? "");
    };
    order = [...order].sort((a, b) => {
      const va = valueOf(a);
      const vb = valueOf(b);
      const emptyA = va === "";
      const emptyB = vb === "";
      if (emptyA !== emptyB) {
        return emptyA ? 1 : -1; // rows without the date always sink to the bottom
      }
      const cmp = va < vb ? -1 : va > vb ? 1 : a - b;
      return asc ? cmp : -cmp;
    });
  }
  const header = (key: "state" | DateField, label: string) => (
    <th>
      <button
        type="button"
        className="task-table-sort"
        onClick={() =>
          setSort(sort?.key !== key ? { key, asc: true } : sort.asc ? { key, asc: false } : null)
        }
      >
        {label}
        {sort?.key === key ? (sort.asc ? " ▲" : " ▼") : ""}
      </button>
    </th>
  );
  return (
    <table className="task-table">
      <thead>
        <tr>
          {header("state", "STATE")}
          <th className="task-table-label">TASK</th>
          {header("sched", "SCHED")}
          {header("due", "DUE")}
        </tr>
      </thead>
      <tbody>{order.map((i) => rowEls[i])}</tbody>
    </table>
  );
}

// TaskRow is one table row: the state cell (select where editable), the task text with its chips,
// and the date columns.
export function TaskRow({ node, children }: ElementProps) {
  const props = (node?.properties ?? {}) as TaskRowProps;
  const done = Boolean(props.done);
  const depth = Number(props.depth ?? 0);
  return (
    <tr className={`task-row${done ? " task-row-done" : ""}`}>
      <td className="task-row-state-cell">
        <TaskRowState name={String(props.state ?? "")} done={done} line={Number(props.line ?? 0)} />
      </td>
      {/* Nesting from the source is an indent, not a nested table: the rows are flat so the whole
          checklist stays one sortable table. */}
      <td className="task-row-text" style={depth > 0 ? { paddingLeft: `${depth * 16}px` } : undefined}>
        {children}
      </td>
      <td className="task-row-date">
        <TaskRowDate field="sched" value={String(props.sched ?? "")} line={Number(props.line ?? 0)} />
      </td>
      <td className="task-row-date task-row-due">
        <TaskRowDate field="due" value={String(props.due ?? "")} line={Number(props.line ?? 0)} />
      </td>
    </tr>
  );
}

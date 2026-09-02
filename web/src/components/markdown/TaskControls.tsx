import type { Element } from "hast";
import { type CSSProperties, type ReactNode, memo, useContext, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { TaskBoardContext } from "./context";
import { useSetTaskDateMutation, useSetTaskStateMutation } from "../../queries";
import { STATIC_MODE } from "../../runtime";
import { taskStates } from "../../taskStates";
import type { DateField, NoteID, TaskItem } from "../../types";
import { IconChevronLeft, IconChevronRight, RailIcon } from "../icons";

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
  const { noteID, tasksRef, lineOffset = 0 } = useContext(TaskBoardContext);
  const tasks = tasksRef?.current.tasks;
  const etag = tasksRef?.current.etag;
  const item =
    !STATIC_MODE && noteID !== "" && tasks && etag && line > 0
      ? tasks.items.find((t) => t.line === line + lineOffset)
      : undefined;
  return { noteID, item, etag };
}

// sameTask compares the parts of a task a control renders, so a resolved row can skip re-rendering
// when the underlying data refreshed without changing it. The etag is deliberately not part of that
// comparison: it changes on every disk refresh, and re-rendering on it would re-commit the input
// (re-applying its type) and close an open native date picker. Controls read the etag from the
// context at write time instead.
//
// That trades away part of the lock, and the trade is the point: a write now carries the newest
// etag rather than the one that was on screen, so an edit that landed elsewhere in the file no
// longer collides. What still catches a stale write is the per-line `expect` — the line's own state
// must be what the control rendered — which is the half that protects the row being written.
//
// Exported for its test: every field a control renders has to be in here, and a comparison that
// quietly drops one shows up as a row that stops updating.
export function sameTask(a: TaskItem | undefined, b: TaskItem | undefined) {
  if (a === b) return true;
  if (!a || !b) return false;
  return (
    a.line === b.line &&
    a.state === b.state &&
    a.done === b.done &&
    a.text === b.text &&
    a.scheduled === b.scheduled &&
    a.due === b.due &&
    a.completed === b.completed &&
    a.priority === b.priority
  );
}

// TaskCheck makes a plain GFM checklist ("- [ ] foo", no task notation) tickable: the engine has
// always parsed those lines as tasks, only the frontend left the native checkbox disabled. The line
// comes from rehypeTaskCheck; a box it cannot resolve stays exactly as it renders today.
export function TaskCheck({ line, checked }: { line: number; checked: boolean }) {
  const { noteID, item } = useTaskAtLine(line);
  if (!item) {
    return <input type="checkbox" checked={checked} disabled readOnly />;
  }
  return <TaskCheckControl noteID={noteID} item={item} />;
}

// TaskCheckControl is memoized on the rendered task: a disk refresh that does not change the line
// must not re-render it (see sameTask). Its own mutation state still re-renders it while a write is
// in flight. The etag is read from the context at write time, not captured in the render, so a write
// always carries the current optimistic lock even while this component is skipping re-renders.
const TaskCheckControl = memo(
  function TaskCheckControl({ noteID, item }: { noteID: NoteID; item: TaskItem }) {
    const mutation = useSetTaskStateMutation(noteID);
    const { tasksRef } = useContext(TaskBoardContext);
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
          if (target) {
            mutation.mutate({
              line: item.line,
              state: target.name,
              expect: item.state,
              etag: tasksRef?.current.etag ?? "",
            });
          }
        }}
      />
    );
  },
  (prev, next) => prev.noteID === next.noteID && sameTask(prev.item, next.item),
);

// TaskRowDate is the scheduled/due cell. Read-only it is the marked date as written; where the note
// can be written it is a button styled down to look like that same text, opening the custom picker
// (TaskDatePicker) — the browser's own calendar shows era years and a garish native scheme, so the
// cell opens one drawn in the workspace's own language instead. An empty cell shows nothing until
// the row is hovered or focused (the CSS reveals it), so an untouched table stays quiet.
function TaskRowDate({ field, value, line }: { field: DateField; value: string; line: number }) {
  const { noteID, item } = useTaskAtLine(line);
  const marker = field === "sched" ? "▷" : "!";
  if (!item) {
    return <>{value ? `${marker} ${value}` : ""}</>;
  }
  return <TaskRowDateControl noteID={noteID} item={item} field={field} value={value} marker={marker} />;
}

// TaskRowDateControl is memoized on what it renders (the cell's date, the resolved task): a disk
// refresh that leaves the line's task unchanged must not re-commit the input — re-applying its type
// closes an open picker. The etag is read from the context at write time (see TaskCheckControl), so
// a write always carries the current lock without re-rendering.
const TaskRowDateControl = memo(
  function TaskRowDateControl({
    noteID,
    item,
    field,
    value,
    marker,
  }: {
    noteID: NoteID;
    item: TaskItem;
    field: DateField;
    value: string;
    marker: string;
  }) {
    const mutation = useSetTaskDateMutation(noteID);
    const { tasksRef } = useContext(TaskBoardContext);
    const [pickerOpen, setPickerOpen] = useState(false);
    const cellRef = useRef<HTMLButtonElement>(null);

    // Committing from the picker: the picked day, or "" to clear the token. The line's own state is
    // asserted at write time, like the state controls — a date picked against a task that has since
    // moved is refused rather than written onto whatever the line became.
    function commit(date: string) {
      mutation.mutate({
        line: item.line,
        field,
        date,
        expect: item.state,
        etag: tasksRef?.current.etag ?? "",
      });
      setPickerOpen(false);
    }

    return (
      <>
        <button
          ref={cellRef}
          type="button"
          className="task-row-date-input"
          aria-label={field === "sched" ? "Scheduled date" : "Due date"}
          aria-haspopup="dialog"
          aria-expanded={pickerOpen}
          data-empty={value === "" || undefined}
          disabled={mutation.isPending}
          onClick={() => setPickerOpen(true)}
        >
          {value ? `${marker} ${value}` : ""}
        </button>
        {pickerOpen && cellRef.current
          ? createPortal(
              <TaskDatePicker
                anchor={cellRef.current}
                value={value}
                onSave={(date) => commit(date)}
                onClear={() => commit("")}
                onClose={() => setPickerOpen(false)}
              />,
              document.body,
            )
          : null}
      </>
    );
  },
  (prev, next) =>
    prev.noteID === next.noteID &&
    prev.field === next.field &&
    prev.value === next.value &&
    sameTask(prev.item, next.item),
);

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

// TaskDatePicker is the cell's own calendar: a floating layer (design.md variant 3) anchored under
// the cell, with the workspace's tokens and its own text SAVE/DELETE instead of the browser's
// native chrome. The month header navigates; a day click sets the working choice; SAVE writes it and
// DELETE clears the token. Outside clicks and Escape close without writing.
function TaskDatePicker({
  anchor,
  value,
  onSave,
  onClear,
  onClose,
}: {
  anchor: HTMLElement;
  value: string;
  onSave: (date: string) => void;
  onClear: () => void;
  onClose: () => void;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const [month, setMonth] = useState(() => monthOf(value));
  const [picked, setPicked] = useState(value);

  useEffect(() => {
    function onPointerDown(event: MouseEvent) {
      if (!panelRef.current?.contains(event.target as Node)) onClose();
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [onClose]);

  // Anchor the panel under the cell, clamped into the window.
  const rect = anchor.getBoundingClientRect();
  const style: CSSProperties = {
    top: Math.min(rect.bottom + 6, Math.max(8, window.innerHeight - 316)),
    left: Math.min(Math.max(rect.left, 8), Math.max(8, window.innerWidth - 248)),
  };

  const year = month.getFullYear();
  const monthNo = month.getMonth() + 1;
  const daysInMonth = new Date(year, monthNo, 0).getDate();
  const leadingBlanks = month.getDay();
  const todayKey = dateKey(new Date());

  function shiftMonth(delta: number) {
    setMonth((current) => new Date(current.getFullYear(), current.getMonth() + delta, 1));
  }

  return (
    <div className="task-date-picker" role="dialog" aria-label="Pick a date" ref={panelRef} style={style}>
      <header className="task-date-head">
        <button type="button" aria-label="Previous month" onClick={() => shiftMonth(-1)}>
          <RailIcon Icon={IconChevronLeft} size={14} />
        </button>
        <span className="task-date-month">
          {year} / {pad2(monthNo)}
        </span>
        <button type="button" aria-label="Next month" onClick={() => shiftMonth(1)}>
          <RailIcon Icon={IconChevronRight} size={14} />
        </button>
      </header>
      <div className="task-date-weekdays">
        {WEEKDAYS.map((day) => (
          <span key={day}>{day}</span>
        ))}
      </div>
      <div className="task-date-grid">
        {Array.from({ length: leadingBlanks }, (_, i) => (
          <span className="task-date-blank" key={`blank-${i}`} />
        ))}
        {Array.from({ length: daysInMonth }, (_, i) => {
          const key = `${year}-${pad2(monthNo)}-${pad2(i + 1)}`;
          return (
            <button
              type="button"
              className={`task-date-day${key === todayKey ? " task-date-today" : ""}`}
              aria-pressed={key === picked}
              key={key}
              onClick={() => setPicked(key)}
            >
              {i + 1}
            </button>
          );
        })}
      </div>
      <footer className="task-date-actions">
        <button type="button" className="task-date-clear" onClick={onClear}>
          DELETE
        </button>
        <button type="button" className="task-date-save" onClick={() => onSave(picked)}>
          SAVE
        </button>
      </footer>
    </div>
  );
}

function monthOf(date: string): Date {
  const match = /^(\d{4})-(\d{2})/.exec(date);
  if (match) return new Date(Number(match[1]), Number(match[2]) - 1, 1);
  const now = new Date();
  return new Date(now.getFullYear(), now.getMonth(), 1);
}

function dateKey(date: Date): string {
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())}`;
}

function pad2(n: number): string {
  return String(n).padStart(2, "0");
}

// TaskRowState is the state cell of a task-table row, and doubles as the state control: in the
// live workspace it renders as a select stripped down to the badge's text look, writing through
// the same engine path as the board's cards. Its source line resolves the row to the engine-parsed
// task (rendered bodies are line-aligned with the note file — the invariant includes rely on); on
// static sites and hover previews (no note id) it stays a plain badge.
function TaskRowState({ name, done, line }: { name: string; done: boolean; line: number }) {
  const { noteID, item } = useTaskAtLine(line);
  const className = `task-row-state${done ? " task-row-state-done" : ""}`;
  if (!item) {
    return <span className={className}>{name}</span>;
  }
  return <TaskRowStateControl noteID={noteID} item={item} className={className} />;
}

const TaskRowStateControl = memo(
  function TaskRowStateControl({
    noteID,
    item,
    className,
  }: {
    noteID: NoteID;
    item: TaskItem;
    className: string;
  }) {
    const mutation = useSetTaskStateMutation(noteID);
    const { tasksRef } = useContext(TaskBoardContext);
    return (
      <select
        className={className}
        aria-label="Task state"
        value={item.state}
        disabled={mutation.isPending}
        onChange={(event) =>
          mutation.mutate({
            line: item.line,
            state: event.currentTarget.value,
            expect: item.state,
            etag: tasksRef?.current.etag ?? "",
          })
        }
      >
        {taskStates.map((state) => (
          <option key={state.name} value={state.name}>
            {state.name}
          </option>
        ))}
      </select>
    );
  },
  (prev, next) =>
    prev.noteID === next.noteID && prev.className === next.className && sameTask(prev.item, next.item),
);

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

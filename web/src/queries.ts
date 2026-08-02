import { keepPreviousData, type QueryClient, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  APIError,
  deleteNote,
  fetchAssetText,
  getActivity,
  getAgenda,
  getGraph,
  getLocalGraph,
  getNote,
  getNoteMeta,
  getOgp,
  getSite,
  listDatedTasks,
  listNewNotes,
  listNotes,
  listOpenTasks,
  renderMarkdown,
  renderViewSpec,
  resolveTerm,
  saveNote,
  saveNoteMeta,
  searchNotes,
  setTaskDate,
  setTaskState,
  uploadAsset,
} from "./api";
import { useNotifications } from "./notifications";
import { STATIC_MODE } from "./runtime";
import { useDebouncedValue } from "./hooks/useDebouncedValue";
import type { DateField, NoteID, NoteMetaResponse, NoteResponse, SaveNoteMetaRequest, SaveNoteRequest } from "./types";

export const queryKeys = {
  site: () => ["site"] as const,
  activity: (since: string, until: string) => ["activity", since, until] as const,
  agenda: (date: string, vault = "") => ["agenda", vault, date] as const,
  graph: () => ["graph"] as const,
  localGraph: (noteID: NoteID) => ["graph", "local", noteID] as const,
  note: (noteID: NoteID) => ["note", noteID] as const,
  noteMeta: (noteID: NoteID) => ["note-meta", noteID] as const,
  notes: () => ["notes"] as const,
  newNotes: (limit: number) => ["notes", "new", limit] as const,
  // Both listings sit under one prefix so a task write invalidates them together.
  tasks: () => ["tasks"] as const,
  datedTasks: () => ["tasks", "dated"] as const,
  openTasks: () => ["tasks", "open"] as const,
  resolve: (term: string, vault = "") => ["resolve", vault, term] as const,
  search: (query: string, limit: number) => ["search", query, limit] as const,
  ogp: (url: string) => ["ogp", url] as const,
  render: (body: string, vault = "") => ["render", vault, body] as const,
  assetText: (href: string) => ["assetText", href] as const,
  viewspec: (spec: string, vault = "") => ["viewspec", vault, spec] as const,
};

export function useActivityQuery(since: string, until: string) {
  return useQuery({
    queryKey: queryKeys.activity(since, until),
    queryFn: () => getActivity(since, until),
    enabled: since !== "" && until !== "",
  });
}

// The day's notes come from the same vault as the journal being read: a journal id is the date, so
// every vault has one for that day and an unscoped lookup would list another vault's notes.
export function useAgendaQuery(date: string, vault = "", options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: queryKeys.agenda(date, vault),
    queryFn: () => getAgenda(date, vault),
    enabled: (options?.enabled ?? true) && date !== "",
  });
}

export function useOgpQuery(url: string, enabled = true) {
  return useQuery({
    queryKey: queryKeys.ogp(url),
    queryFn: () => getOgp(url),
    enabled: enabled && url !== "",
    // Link metadata is effectively static for a session and the server caches it too, so never refetch.
    staleTime: Infinity,
    gcTime: Infinity,
    retry: false,
  });
}

export function useAssetTextQuery(href: string, enabled = true) {
  return useQuery({
    queryKey: queryKeys.assetText(href),
    queryFn: () => fetchAssetText(href),
    enabled: enabled && href !== "",
    // The reader re-fetches a note when it changes, so caching an asset's text for the session is enough.
    staleTime: Infinity,
    gcTime: Infinity,
    retry: false,
  });
}

export function useSearchQuery(query: string, limit = 100, options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: queryKeys.search(query, limit),
    queryFn: () => searchNotes(query, limit),
    enabled: options?.enabled ?? true,
  });
}

export function useNotesQuery() {
  return useQuery({
    queryKey: queryKeys.notes(),
    queryFn: listNotes,
  });
}

export function useNewNotesQuery(limit = 10) {
  return useQuery({
    queryKey: queryKeys.newNotes(limit),
    queryFn: () => listNewNotes(limit),
  });
}

// useDatedTasksQuery lists every task in the vault carrying a date, for the calendar and the day
// page. One cache entry serves both, so opening a day from the calendar paints from what is already
// held.
export function useDatedTasksQuery() {
  return useQuery({
    queryKey: queryKeys.datedTasks(),
    queryFn: listDatedTasks,
  });
}

// useOpenTasksQuery lists every dated task still to do across the vault, worst-first. Live server
// only: the published bundle carries the full dated listing alone.
export function useOpenTasksQuery(enabled = true) {
  return useQuery({
    queryKey: queryKeys.openTasks(),
    queryFn: listOpenTasks,
    enabled,
  });
}

// useSiteQuery reads the published site's descriptor (entry note, calendar toggle). The file only
// exists in the static export, so the query stays off on the live server.
export function useSiteQuery() {
  return useQuery({
    queryKey: queryKeys.site(),
    queryFn: getSite,
    enabled: STATIC_MODE,
    staleTime: Infinity,
  });
}

export function useResolveQuery(term: string, vault = "") {
  return useQuery({
    queryKey: queryKeys.resolve(term, vault),
    queryFn: () => resolveTerm(term, vault),
    enabled: term.trim() !== "",
  });
}

// Fallback poll interval for the open note. Live updates normally arrive via the
// SSE change stream (see useLiveEvents); this only covers a dropped stream.
const liveRefetchInterval = 30000;

export function useNoteQuery(noteID: NoteID, options: { live?: boolean; enabled?: boolean } = {}) {
  return useQuery({
    queryKey: queryKeys.note(noteID),
    queryFn: () => getNote(noteID),
    refetchInterval: options.live ? liveRefetchInterval : false,
    enabled: options.enabled ?? true,
  });
}

// useRenderQuery turns a raw note body into the sanitized Markdown the preview renders, via the server's
// /api/render endpoint. The body is debounced so typing in the editor does not post on every keystroke,
// and the previous render is kept while the next one loads so the preview never flashes empty mid-edit.
export function useRenderQuery(body: string, vault = "") {
  const debounced = useDebouncedValue(body, 200);
  return useQuery({
    queryKey: queryKeys.render(debounced, vault),
    queryFn: () => renderMarkdown(debounced, vault),
    enabled: debounced.trim() !== "",
    // Sanitization is a pure function of the body and the server caches nothing per-note, so an identical
    // body never needs re-posting within a session.
    staleTime: Infinity,
    placeholderData: keepPreviousData,
  });
}

// useViewSpecQuery resolves a fenced ```viewspec block to an ECharts option via the server. The key includes
// the spec text, so an edited block refetches on its own; useLiveEvents additionally invalidates the
// ["viewspec"] prefix when the vault's data/ directory changes, re-rendering charts whose data.source /
// overlays[].source files changed without the note body changing. The previous chart is kept while the
// refetch is in flight so a live update never flashes the loading state.
export function useViewSpecQuery(spec: string, vault = "") {
  return useQuery({
    queryKey: queryKeys.viewspec(spec, vault),
    queryFn: () => renderViewSpec(spec, vault),
    // The static export replaces viewspec fences at build time; a leftover block shows its source.
    enabled: !STATIC_MODE,
    // A bad spec is a deterministic client error the user should see immediately, not retry through.
    retry: false,
    placeholderData: keepPreviousData,
  });
}

export function useGraphQuery(enabled = true) {
  return useQuery({
    queryKey: queryKeys.graph(),
    queryFn: getGraph,
    enabled,
  });
}

export function useLocalGraphQuery(noteID: NoteID | undefined, enabled = noteID !== undefined) {
  return useQuery({
    queryKey: queryKeys.localGraph(noteID ?? ""),
    queryFn: () => {
      if (noteID === undefined) {
        throw new Error("note id is required for local graph");
      }
      return getLocalGraph(noteID);
    },
    enabled,
  });
}

export function useDeleteNoteMutation(noteID: NoteID) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => deleteNote(noteID),
    onSuccess: () => {
      // The note is gone: drop its cache and refresh the lists/graph that referenced it.
      queryClient.removeQueries({ queryKey: queryKeys.note(noteID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.notes() });
      void queryClient.invalidateQueries({ queryKey: ["search"] });
      void queryClient.invalidateQueries({ queryKey: ["graph"] });
      void queryClient.invalidateQueries({ queryKey: ["activity"] });
    },
  });
}

export function useSaveNoteMutation(noteID: NoteID) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (request: SaveNoteRequest) => saveNote(noteID, request),
    onSuccess: (response, request) => {
      queryClient.setQueryData<NoteResponse>(queryKeys.note(noteID), (current) => {
        if (!current) return current;
        return {
          ...current,
          note: {
            ...current.note,
            body: request.body,
            etag: response.etag,
          },
        };
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.note(noteID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.notes() });
      void queryClient.invalidateQueries({ queryKey: ["search"] });
      void queryClient.invalidateQueries({ queryKey: ["graph"] });
      void queryClient.invalidateQueries({ queryKey: ["activity"] });
    },
  });
}

// useNoteMetaQuery loads a note's editable metadata document for the meta dialog; fetched only
// while the dialog is open (live server only — the static site has no editor).
export function useNoteMetaQuery(noteID: NoteID, opts: { enabled: boolean }) {
  return useQuery({
    queryKey: queryKeys.noteMeta(noteID),
    queryFn: () => getNoteMeta(noteID),
    enabled: opts.enabled,
  });
}

// invalidateTaskWrite refreshes every view a task write can have made stale. Writing a task line
// rewrites the note's body on disk, so all of them go at once: the note query (rendered body and
// embedded tasks payload) and the notes listing; both task listings under the ["tasks"] prefix — a
// state change can stamp or clear a completion date and moves a task in or out of the open listing,
// and a date write is exactly what moves it from one calendar day to another; and every cached
// render, because a host note embedding this one keys its excerpt by its own body text and would
// otherwise keep the old marker, stamp, and dates.
function invalidateTaskWrite(queryClient: QueryClient, noteID: NoteID) {
  void queryClient.invalidateQueries({ queryKey: queryKeys.note(noteID) });
  void queryClient.invalidateQueries({ queryKey: queryKeys.notes() });
  void queryClient.invalidateQueries({ queryKey: queryKeys.tasks() });
  void queryClient.invalidateQueries({ queryKey: ["render"] });
}

function handleTaskWriteError(
  queryClient: QueryClient,
  noteID: NoteID,
  notify: (message: string) => void,
  error: unknown,
) {
  void queryClient.invalidateQueries({ queryKey: queryKeys.note(noteID) });
  if (error instanceof APIError && error.status === 409) {
    notify("This note changed since it was loaded. Reloading the latest version; retry your task change.");
  }
}

// useSetTaskStateMutation moves one task line into a named state (board drag / card select).
export function useSetTaskStateMutation(noteID: NoteID) {
  const queryClient = useQueryClient();
  const { notify } = useNotifications();

  return useMutation({
    mutationFn: ({ line, state, expect, etag }: { line: number; state: string; expect?: string; etag: string }) =>
      setTaskState(noteID, line, state, expect ?? "", etag),
    onSuccess: () => invalidateTaskWrite(queryClient, noteID),
    // A refused write means the view is stale — refetch so the error is read against what the
    // note actually says now.
    onError: (error) => handleTaskWriteError(queryClient, noteID, notify, error),
  });
}

// useSetTaskDateMutation writes a task's scheduled/due date.
export function useSetTaskDateMutation(noteID: NoteID) {
  const queryClient = useQueryClient();
  const { notify } = useNotifications();

  return useMutation({
    mutationFn: ({
      line,
      field,
      date,
      expect,
      etag,
    }: {
      line: number;
      field: DateField;
      date: string;
      expect?: string;
      etag: string;
    }) => setTaskDate(noteID, line, field, date, expect ?? "", etag),
    onSuccess: () => invalidateTaskWrite(queryClient, noteID),
    // A refused write means the view is stale — refetch so the error is read against what the
    // note actually says now.
    onError: (error) => handleTaskWriteError(queryClient, noteID, notify, error),
  });
}

export function useSaveNoteMetaMutation(noteID: NoteID) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (request: SaveNoteMetaRequest) => saveNoteMeta(noteID, request),
    onSuccess: (response) => {
      queryClient.setQueryData<NoteMetaResponse>(queryKeys.noteMeta(noteID), response);
      // The edit carries the title, tags, and props, which the note view, lists, and graph render;
      // a title change also rewrites backlinks in other notes.
      void queryClient.invalidateQueries({ queryKey: queryKeys.note(noteID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.notes() });
      void queryClient.invalidateQueries({ queryKey: ["search"] });
      void queryClient.invalidateQueries({ queryKey: ["graph"] });
    },
  });
}

// useUploadAssetMutation imports a picked cover image into the vault assets and yields its
// assets/<name> reference; the dialog sets its image field to the result. Live server only.
// The upload lands in the vault of the note the asset is for: an "assets/<file>" ref is relative to
// its own vault, so a cover stored anywhere else would resolve to nothing.
export function useUploadAssetMutation(vault = "") {
  return useMutation({
    mutationFn: (file: File) => uploadAsset(file, vault),
  });
}

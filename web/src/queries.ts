import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getActivity,
  getGraph,
  getLocalGraph,
  getNote,
  listNotes,
  resolveTerm,
  saveNote,
  searchNotes,
} from "./api";
import type { NoteID, NoteResponse, SaveNoteRequest } from "./types";

export const queryKeys = {
  activity: (days: number) => ["activity", days] as const,
  graph: () => ["graph"] as const,
  localGraph: (noteID: NoteID) => ["graph", "local", noteID] as const,
  note: (noteID: NoteID) => ["note", noteID] as const,
  notes: () => ["notes"] as const,
  resolve: (term: string) => ["resolve", term] as const,
  search: (query: string, limit: number) => ["search", query, limit] as const,
};

export function useActivityQuery(days = 14) {
  return useQuery({
    queryKey: queryKeys.activity(days),
    queryFn: () => getActivity(days),
  });
}

export function useSearchQuery(query: string, limit = 100) {
  return useQuery({
    queryKey: queryKeys.search(query, limit),
    queryFn: () => searchNotes(query, limit),
  });
}

export function useNotesQuery() {
  return useQuery({
    queryKey: queryKeys.notes(),
    queryFn: listNotes,
  });
}

export function useResolveQuery(term: string) {
  return useQuery({
    queryKey: queryKeys.resolve(term),
    queryFn: () => resolveTerm(term),
    enabled: term.trim() !== "",
  });
}

// Poll interval for views that should reflect notes edited elsewhere (Neovim,
// CLI, or a cloud sync) without a manual refresh.
const liveRefetchInterval = 4000;

export function useNoteQuery(noteID: NoteID, options: { live?: boolean } = {}) {
  return useQuery({
    queryKey: queryKeys.note(noteID),
    queryFn: () => getNote(noteID),
    refetchInterval: options.live ? liveRefetchInterval : false,
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
    queryKey: queryKeys.localGraph(noteID ?? 0),
    queryFn: () => {
      if (noteID === undefined) {
        throw new Error("note id is required for local graph");
      }
      return getLocalGraph(noteID);
    },
    enabled,
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

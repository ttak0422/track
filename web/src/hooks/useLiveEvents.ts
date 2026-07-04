import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { queryKeys } from "../queries";
import { STATIC_MODE } from "../runtime";

// Subscribes to the server's change stream and refreshes data when the vault
// changes on disk (edits from Neovim/CLI or a cloud sync). The backend reindexes
// before emitting, so invalidated queries refetch up-to-date results. A separate
// `data` event covers the vault's data/ directory: those files feed embedded
// viewspec charts (data.source / overlays[].source) without being indexed, so
// only the rendered charts are refetched.
export function useLiveEvents() {
  const queryClient = useQueryClient();

  useEffect(() => {
    // The published static site has no server/change stream to subscribe to.
    if (STATIC_MODE || typeof EventSource === "undefined") {
      return;
    }
    let invalidateTimer: number | undefined;
    function scheduleInvalidation() {
      if (invalidateTimer !== undefined) {
        window.clearTimeout(invalidateTimer);
      }
      invalidateTimer = window.setTimeout(() => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.notes() });
        void queryClient.invalidateQueries({ queryKey: ["note"] });
        void queryClient.invalidateQueries({ queryKey: ["search"] });
        void queryClient.invalidateQueries({ queryKey: ["activity"] });
        void queryClient.invalidateQueries({ queryKey: ["agenda"] });
        void queryClient.invalidateQueries({ queryKey: ["graph"] });
      }, 150);
    }
    let dataTimer: number | undefined;
    function scheduleDataInvalidation() {
      if (dataTimer !== undefined) {
        window.clearTimeout(dataTimer);
      }
      dataTimer = window.setTimeout(() => {
        void queryClient.invalidateQueries({ queryKey: ["viewspec"] });
      }, 150);
    }
    const source = new EventSource("/api/events");
    source.addEventListener("change", scheduleInvalidation);
    source.addEventListener("data", scheduleDataInvalidation);
    return () => {
      if (invalidateTimer !== undefined) {
        window.clearTimeout(invalidateTimer);
      }
      if (dataTimer !== undefined) {
        window.clearTimeout(dataTimer);
      }
      source.close();
    };
  }, [queryClient]);
}

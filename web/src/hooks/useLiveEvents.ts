import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { queryKeys } from "../queries";

// Subscribes to the server's change stream and refreshes data when the vault
// changes on disk (edits from Neovim/CLI or a cloud sync). The backend reindexes
// before emitting, so invalidated queries refetch up-to-date results.
export function useLiveEvents() {
  const queryClient = useQueryClient();

  useEffect(() => {
    if (typeof EventSource === "undefined") {
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
    const source = new EventSource("/api/events");
    source.addEventListener("change", scheduleInvalidation);
    return () => {
      if (invalidateTimer !== undefined) {
        window.clearTimeout(invalidateTimer);
      }
      source.close();
    };
  }, [queryClient]);
}

import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

// Subscribes to the server's change stream and refreshes data when the vault
// changes on disk (edits from Neovim/CLI or a cloud sync). The backend reindexes
// before emitting, so invalidated queries refetch up-to-date results.
export function useLiveEvents() {
  const queryClient = useQueryClient();

  useEffect(() => {
    if (typeof EventSource === "undefined") {
      return;
    }
    const source = new EventSource("/api/events");
    source.addEventListener("change", () => {
      void queryClient.invalidateQueries();
    });
    return () => source.close();
  }, [queryClient]);
}

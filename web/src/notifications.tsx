import { useNavigate } from "@tanstack/react-router";
import { createContext, type ReactNode, useCallback, useContext, useMemo, useState } from "react";
import type { NoteID } from "./types";

// One toast: a message, plus the note it navigates to when it announces a note update. A notification
// without a noteID is a plain message with no navigation.
interface Notification {
  message: string;
  noteID?: NoteID;
}

interface Notifications {
  notification: Notification | null;
  notify: (message: string, noteID?: NoteID) => void;
  dismiss: () => void;
}

const NotificationContext = createContext<Notifications>({
  notification: null,
  notify: () => {},
  dismiss: () => {},
});

// NotificationProvider only owns the toast state. The toast itself renders in NotificationToast, a
// component mounted below the router (Shell), because clicking it navigates — this provider sits
// above the router where useNavigate is unavailable.
export function NotificationProvider({ children }: { children: ReactNode }) {
  const [notification, setNotification] = useState<Notification | null>(null);
  const notify = useCallback((message: string, noteID?: NoteID) => setNotification({ message, noteID }), []);
  const dismiss = useCallback(() => setNotification(null), []);
  const value = useMemo(() => ({ notification, notify, dismiss }), [notification, notify, dismiss]);

  return <NotificationContext.Provider value={value}>{children}</NotificationContext.Provider>;
}

// NotificationToast renders the current notification, if any. A toast that names a note opens that
// note on click (the whole toast is the button, except the dismiss ×).
export function NotificationToast() {
  const { notification, dismiss } = useNotifications();
  const navigate = useNavigate();
  if (!notification) return null;
  const { message, noteID } = notification;

  return (
    <div className="notification-toast" role="alert">
      {noteID ? (
        <button
          type="button"
          className="notification-toast-open"
          onClick={() => {
            dismiss();
            void navigate({ to: "/notes/$noteId", params: { noteId: noteID } });
          }}
        >
          <span>{message}</span>
        </button>
      ) : (
        <span>{message}</span>
      )}
      <button type="button" aria-label="Dismiss notification" onClick={dismiss}>
        ×
      </button>
    </div>
  );
}

export function useNotifications(): Notifications {
  return useContext(NotificationContext);
}

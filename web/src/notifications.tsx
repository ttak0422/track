import { useNavigate } from "@tanstack/react-router";
import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import type { NoteID } from "./types";

// How long a toast stays up. Long enough to read the longest message this app raises (the task
// conflict notice) and still reach for it, short enough that it is gone before it becomes furniture.
const toastLifetime = 8000;

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
  // A toast reports something that already happened, so it expires on its own rather than waiting to
  // be dismissed — otherwise a vault update from an hour ago is still sitting over the corner of the
  // reader. Each new notification is a fresh object, so an identical message repeated later restarts
  // the clock instead of inheriting the old one.
  useEffect(() => {
    if (!notification) return;
    const timer = window.setTimeout(dismiss, toastLifetime);
    return () => window.clearTimeout(timer);
  }, [notification, dismiss]);

  if (!notification) return null;
  const { message, noteID } = notification;

  return (
    <div className="notification-toast" role="alert">
      {/* The bar shows how long the toast stays: the accent (--mark) drains over the lifetime,
          matching the timeout above. It is the timer, drawn — the toast needs no other one. */}
      <span
        className="notification-timer"
        style={{ animationDuration: `${toastLifetime}ms` }}
        aria-hidden="true"
      />
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

import {
  Link,
  Outlet,
  RouterProvider,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { api, type ActivityResponse } from "./api";
import "./styles.css";

const queryClient = new QueryClient();

const rootRoute = createRootRoute({
  component: RootLayout,
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: HomeRoute,
});

const noteRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/notes/$noteId",
  component: NoteRoute,
});

const routeTree = rootRoute.addChildren([indexRoute, noteRoute]);

const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}

function RootLayout() {
  return (
    <main className="workspace">
      <aside className="sidebar">
        <header className="brand">
          <h1>
            <Link to="/">track</Link>
          </h1>
          <p>React workspace migration</p>
        </header>
        <nav className="nav">
          <Link to="/">Home</Link>
          <Link to="/notes/$noteId" params={{ noteId: "1781359469000" }}>
            Example note route
          </Link>
        </nav>
      </aside>
      <section className="reader">
        <Outlet />
      </section>
    </main>
  );
}

function HomeRoute() {
  const activity = useQuery({
    queryKey: ["activity", 14],
    queryFn: () => api<ActivityResponse>("/api/activity?days=14"),
  });

  return (
    <article className="panel">
      <h2>Web migration shell</h2>
      <p>
        This React/Vite entry is intentionally small. The current Go-served UI stays in place while
        routes, server-state queries, and components are migrated incrementally.
      </p>
      <section>
        <h3>Activity query</h3>
        {activity.isPending ? <p>Loading activity...</p> : null}
        {activity.isError ? <p className="error">{activity.error.message}</p> : null}
        {activity.data ? (
          <p>
            {activity.data.activity.total} updates over {activity.data.activity.days} days.
          </p>
        ) : null}
      </section>
    </article>
  );
}

function NoteRoute() {
  const { noteId } = noteRoute.useParams();

  return (
    <article className="panel">
      <h2>Note route</h2>
      <p>Selected note id: {noteId}</p>
    </article>
  );
}

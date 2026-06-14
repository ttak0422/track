import { RouterProvider, createRootRoute, createRoute, createRouter } from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Shell } from "./components/Shell";
import "./styles.css";

const queryClient = new QueryClient();

const rootRoute = createRootRoute({
  component: Shell,
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

function HomeRoute() {
  return (
    <article className="panel">
      <h2>Web migration shell</h2>
      <p>
        This React/Vite entry is intentionally small. The current Go-served UI stays in place while
        routes, server-state queries, and components are migrated incrementally.
      </p>
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

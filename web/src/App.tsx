/**
 * Top-level app component: wires up React Query and the TanStack Router.
 * @module App
 */
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { router } from "./router";

/**
 * Shared React Query client.
 * @remarks `refetchOnWindowFocus` is disabled because the queries in this app
 * already refetch on a timer where it matters (vault stats / transactions)
 * and background focus refetches only add RPC load without visible benefit.
 */
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 2,
      refetchOnWindowFocus: false,
    },
  },
});

/** Root React component mounted by `main.tsx`. */
export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}

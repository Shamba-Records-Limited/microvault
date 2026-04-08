/**
 * TanStack Router configuration: root layout plus three lazy-loaded pages.
 * @module router
 * @remarks Child routes use `lazyRouteComponent` so each page's JS is
 * code-split into its own chunk, keeping the initial landing-page payload
 * small. The `declare module` block registers the router type globally so
 * `<Link to="/…" />` gets type-safe path completion everywhere.
 */
import { createRouter, createRoute, createRootRoute, lazyRouteComponent } from "@tanstack/react-router";
import RootLayout from "./routes/__root";
import NotFoundPage from "./routes/not-found";

const rootRoute = createRootRoute({
  component: RootLayout,
  notFoundComponent: NotFoundPage,
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: lazyRouteComponent(() => import("./routes/index")),
});

const ourApproachRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/our-approach",
  component: lazyRouteComponent(() => import("./routes/our-approach")),
});

const transactionsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/transactions",
  component: lazyRouteComponent(() => import("./routes/transactions")),
});

const routeTree = rootRoute.addChildren([indexRoute, ourApproachRoute, transactionsRoute]);

export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

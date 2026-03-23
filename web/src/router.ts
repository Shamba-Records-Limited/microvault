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

const routeTree = rootRoute.addChildren([indexRoute, ourApproachRoute]);

export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

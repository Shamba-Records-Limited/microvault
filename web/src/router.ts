import { createRouter, createRoute, createRootRoute } from "@tanstack/react-router";
import RootLayout from "./routes/__root";
import IndexPage from "./routes/index";
import OurApproachPage from "./routes/our-approach";
import NotFoundPage from "./routes/not-found";

const rootRoute = createRootRoute({
  component: RootLayout,
  notFoundComponent: NotFoundPage,
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: IndexPage,
});

const ourApproachRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/our-approach",
  component: OurApproachPage,
});

const routeTree = rootRoute.addChildren([indexRoute, ourApproachRoute]);

export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
